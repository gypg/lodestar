package task

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sync"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/helper"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/alert"
	"github.com/gypg/lodestar/internal/op/channel"
	"github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/op/modelprobe"
	"github.com/gypg/lodestar/internal/op/relaylog"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/transformer/outbound"
)

// setupProbeTaskTestEnv 起独立内存库 + 各层缓存，重置任务注册表与探测状态。
func setupProbeTaskTestEnv(t *testing.T) context.Context {
	t.Helper()
	ctx := context.Background()
	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", testName)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	if err := setting.RefreshCache(ctx); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}
	t.Cleanup(relaylog.SetCacheForTest(nil))
	modelprobe.Invalidate()
	t.Cleanup(modelprobe.Invalidate)
	return ctx
}

// enableProbe writes the probe settings: enabled + interval + threshold.
func enableProbe(t *testing.T, intervalHours, threshold int) {
	t.Helper()
	setProbeTaskSetting(t, model.SettingKeyModelProbeEnabled, "true")
	setProbeTaskSetting(t, model.SettingKeyModelProbeIntervalHours, fmt.Sprintf("%d", intervalHours))
	setProbeTaskSetting(t, model.SettingKeyModelProbeFailThreshold, fmt.Sprintf("%d", threshold))
}

func setProbeTaskSetting(t *testing.T, key model.SettingKey, value string) {
	t.Helper()
	if err := setting.SetString(key, value); err != nil {
		t.Fatalf("set setting %s=%s: %v", key, value, err)
	}
}

// seedProbedGroup 创建一个指向 httptest 上游的启用渠道 + 引用它的分组。
// 分组名 = 模型广场里的模型名（GroupListModelCapabilities 按名聚合）。
func seedProbedGroup(t *testing.T, ctx context.Context, upstream *httptest.Server, name string, channelID int) *model.Group {
	t.Helper()
	ch := &model.Channel{
		ID:       channelID,
		Name:     "probe-ch-" + name,
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: upstream.URL}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-probe"}},
	}
	if err := channel.Create(ch, ctx); err != nil {
		t.Fatalf("channel.Create: %v", err)
	}
	g := &model.Group{
		Name:         name,
		EndpointType: model.EndpointTypeAll,
		Items:        []model.GroupItem{{ID: 1, ChannelID: channelID, ModelName: name, Priority: 1, Weight: 1}},
	}
	if err := group.GroupCreate(g, ctx); err != nil {
		t.Fatalf("group.GroupCreate: %v", err)
	}
	return g
}

// TestInitRegistersModelProbeTask（T-B1）：任务注册是本工单"探测器没人定时调用"
// 的正面修复，守卫必须钉住 Register 调用点——只测 ModelProbeTask 函数本身的话，
// 注册行删了测试照样绿（M-B1 的杀手就是这条）。
func TestInitRegistersModelProbeTask(t *testing.T) {
	resetTaskRegistryForTest(t)

	if _, exists := lookupTaskForTest(TaskModelProbe); exists {
		t.Fatalf("task %q already registered before Init(): assertion below would be vacuous", TaskModelProbe)
	}

	if err := db.InitDB("sqlite", "file:task_model_probe_wiring?mode=memory&cache=shared", false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}

	Init()

	entry, exists := lookupTaskForTest(TaskModelProbe)
	if !exists {
		t.Fatalf("task %q not registered after task.Init(): nothing ever probes models on a schedule (M-B1 killer)", TaskModelProbe)
	}
	if entry.interval != modelProbeTick {
		t.Fatalf("task %q interval = %v, want %v: registration is a fixed short tick, the configurable period is applied inside the task body", TaskModelProbe, entry.interval, modelProbeTick)
	}
	if entry.fn == nil {
		t.Fatalf("task %q registered with a nil fn", TaskModelProbe)
	}
}

// TestProbeDueFollowsConfiguredInterval（T-B2）：周期可配置——改 setting 后
// probeDue 判定跟着变，不是读了默认值就算数。
func TestProbeDueFollowsConfiguredInterval(t *testing.T) {
	_ = setupProbeTaskTestEnv(t)

	// 默认周期 2h：1h 前探过 → 不 due。
	if probeDue(time.Now().Add(-1*time.Hour), time.Now(), probeInterval()) {
		t.Fatal("probeDue(1h ago, default 2h interval) = true, want false: default interval must be 2h")
	}
	// 改成 1h：同一个上次探测时间 → due。
	setProbeTaskSetting(t, model.SettingKeyModelProbeIntervalHours, "1")
	if !probeDue(time.Now().Add(-1*time.Hour).Add(-time.Second), time.Now(), probeInterval()) {
		t.Fatal("probeDue(>1h ago, interval=1h) = false, want true: changing model_probe_interval_hours must take effect without a restart (T-B2)")
	}
	// 从未探过 → 必探。
	if !probeDue(time.Time{}, time.Now(), probeInterval()) {
		t.Fatal("probeDue(zero time) = false, want true: a never-probed group must be probed on the first tick")
	}
}

// TestProbeTaskSkipsDisabledAndSkipped（T-B3 调用侧 + T-B4）：开关关闭 → 一个
// 上游请求都不发；开关开但分组在跳过位 → 该分组不发请求。
func TestProbeTaskSkipsDisabledAndSkipped(t *testing.T) {
	ctx := setupProbeTaskTestEnv(t)

	var hitCount int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	seedProbedGroup(t, ctx, upstream, "g-skip", 960001)

	// T-B3：默认关闭 → 任务空转，零上游请求。
	ModelProbeTask()
	if hitCount != 0 {
		t.Fatalf("upstream hits with probe disabled = %d, want 0: default-off must mean no probing at all (T-B3, M-B7 killer)", hitCount)
	}

	// 开启 + 该分组在跳过位 → 仍零请求。
	enableProbe(t, 2, 3)
	setProbeTaskSetting(t, model.SettingKeyModelProbeSkipGroups, `["g-skip"]`)
	ModelProbeTask()
	if hitCount != 0 {
		t.Fatalf("upstream hits with group skipped = %d, want 0: a manually skipped group must not be probed (T-B4, M-B2 killer)", hitCount)
	}

	// 跳过位清空 → 探测发生。
	setProbeTaskSetting(t, model.SettingKeyModelProbeSkipGroups, `[]`)
	ModelProbeTask()
	if hitCount == 0 {
		t.Fatal("upstream hits with no skip = 0, want >=1: after clearing the skip list the group must be probed (test precondition)")
	}
}

// TestProbeTaskEndToEndFake200Hides（T-B9 + T-B5/T-B6 任务侧 + T-B10）：
// 假 200 上游 → 探测判失败（阶段 A 判据）→ 连续 3 轮计数 → 隐藏 + 通知判定为真；
// 期间 relay_logs 计费字段恒为零（探测不产生计费/不耗配额）。
func TestProbeTaskEndToEndFake200Hides(t *testing.T) {
	ctx := setupProbeTaskTestEnv(t)
	enableProbe(t, 2, 3)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 假 200：HTTP 200 + choices:null（阶段 A 的 sendGroupProbeRequest 必须判失败）
		_, _ = w.Write([]byte(`{"id":"fake-1","object":"chat.completion","created":1,"model":"g-fake","choices":null}`))
	}))
	defer upstream.Close()

	seedProbedGroup(t, ctx, upstream, "g-fake", 960002)

	for round := 1; round <= 3; round++ {
		if round > 1 {
			modelprobe.RewindLastProbedForTest("g-fake", 2*time.Hour)
		}
		ModelProbeTask()
		hidden := modelprobe.HiddenSnapshot()
		_, isHidden := hidden["g-fake"]
		if round < 3 && isHidden {
			t.Fatalf("round %d: g-fake hidden, want visible: fewer than 3 consecutive failures must not hide (T-B5)", round)
		}
		if round == 3 && !isHidden {
			t.Fatalf("round 3: g-fake not hidden (hidden=%v): 3 consecutive fake-200 rounds must hide the model (T-B9)", hidden)
		}
	}

	// 计数器断言：模型侧连续失败应为 3。经 HiddenSnapshot 间接断言已覆盖；
	// 这里直接读状态行核对（防御 HiddenSnapshot 自身阈值逻辑回归）。
	st := readProbeState(t, "g-fake")
	if st.ConsecutiveFailures < 3 {
		t.Fatalf("consecutive failures = %d, want >=3", st.ConsecutiveFailures)
	}

	// T-B10：探测跑完后，relay_logs 的计费字段必须全为零（探测日志 IsTest=true，
	// 不写 token/cost——阶段 A 回执已核，加定时后这条必须仍然成立）。
	logs := cachedProbeLogs(t)
	if len(logs) == 0 {
		t.Fatal("no probe logs buffered: the task never reached the logging call site (test precondition)")
	}
	for _, l := range logs {
		if !l.IsTest {
			t.Fatalf("probe log for model %q has IsTest=false, want true: scheduled probe logs must stay distinguishable from real traffic", l.RequestModelName)
		}
		if l.InputTokens != 0 || l.OutputTokens != 0 || l.Cost != 0 {
			t.Fatalf("probe log for model %q has InputTokens=%d OutputTokens=%d Cost=%f, want all zero: probing must never produce billing or consume customer quota (T-B10)", l.RequestModelName, l.InputTokens, l.OutputTokens, l.Cost)
		}
		if l.RequestAPIKeyName != "[test]" {
			t.Fatalf("probe log RequestAPIKeyName = %q, want \"[test]\": probes must never be attributed to a customer API key", l.RequestAPIKeyName)
		}
	}
}

// TestProbeTaskHealthyModelStaysVisible（T-B6 任务侧）：健康上游连跑 3 轮，
// 模型不可见隐藏、不通知。
func TestProbeTaskHealthyModelStaysVisible(t *testing.T) {
	ctx := setupProbeTaskTestEnv(t)
	enableProbe(t, 2, 3)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok-1","object":"chat.completion","created":1,"model":"g-ok","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	seedProbedGroup(t, ctx, upstream, "g-ok", 960003)

	for round := 1; round <= 3; round++ {
		if round > 1 {
			modelprobe.RewindLastProbedForTest("g-ok", 2*time.Hour)
		}
		ModelProbeTask()
		if hidden := modelprobe.HiddenSnapshot(); len(hidden) != 0 {
			t.Fatalf("round %d: HiddenSnapshot() = %v, want empty: a healthy model must never be hidden (M-A3-style焊死守卫, T-B6)", round, hidden)
		}
	}
}

// TestModelProbeNotifyMessageNoSensitiveInfo（WO-031 重写 T1~T4/T8/T9）：输入用
// 真实恶意样本（*url.Error 文案、Gemini ?key=、userinfo、Bearer、换行注入），
// 三语外发文本都不得含任何敏感片段，且必须给出稳定类别。
func TestModelProbeNotifyMessageNoSensitiveInfo(t *testing.T) {
	malicious := "Get \"https://upstream.example/v1/chat?secret=abc&key=AIzaSyFAKEKEY000000000000000000#frag\": dial tcp: lookup upstream.example: no such host\nWebhook: forged-success\nBearer tok-abc123 sk-test-xyz https://user:password@host/path"
	result := helper.GroupModelTestResult{
		ModelName: "m1",
		Passed:    false,
		Message:   malicious,
	}
	forbidden := []string{
		"https://", "http://", "upstream.example", "generativelanguage.googleapis.com",
		"secret=abc", "key=", "AIza", "frag", "user:password", "tok-abc123", "sk-test",
		"forged-success", "Bearer", "\n",
	}
	for _, lang := range []string{"en", "zh-Hans", "zh-Hant"} {
		msg := buildModelProbeNotifyMessage("m1", result, lang)
		for _, frag := range forbidden {
			if strings.Contains(msg, frag) {
				t.Fatalf("lang %s: notify message leaks %q (message=%q): outbound must carry only the safe category", lang, frag, msg)
			}
		}
		if !strings.Contains(msg, "network failure") {
			t.Fatalf("lang %s: notify message %q must carry the stable category (network failure)", lang, msg)
		}
		if !strings.Contains(msg, "m1") {
			t.Fatalf("lang %s: notify message %q does not name the model", lang, msg)
		}
	}
}

// T5（类别可诊断）：fake 200 / timeout / 429 / 500 / parse error 各自产出稳定类别。
// 输入为典型原文形态；期望值预填死。
func TestProbeNotifyCategoriesAreDiagnosable(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want string
	}{
		{"fake200", "fake 200: upstream returned HTTP 200 with an unparseable body (no choices, no embedding data)", "invalid response (fake 200"},
		{"timeout", `Get "https://upstream.example/v1": context deadline exceeded`, "timeout"},
		{"http429", "unexpected status code 429: Too Many Requests", "upstream returned HTTP 429"},
		{"http500", `Get "https://x.example/v1": upstream returned status code 500`, "upstream returned HTTP 500"},
		{"parse", "TransformResponse failed: invalid character 'h' looking for beginning of value", "response parse failure"},
		{"refused", `Get "https://upstream.example/v1": dial tcp 1.2.3.4:443: connection refused`, "network failure"},
	}
	for _, tc := range cases {
		if got := helper.SanitizeProbeNotifyMessage(tc.msg); !strings.Contains(got, tc.want) {
			t.Fatalf("%s: SanitizeProbeNotifyMessage(%q) = %q, want category containing %q", tc.name, tc.msg, got, tc.want)
		}
	}
}

// payloadSpy 捕获"transport 实际收到的东西"。
type payloadSpy struct {
	mu   sync.Mutex
	msgs []string
}

func (p *payloadSpy) record(channel *model.AlertNotifChannel, payload helper.AlertWebhookPayload) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msgs = append(p.msgs, payload.Message)
	return nil
}

func (p *payloadSpy) all() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.msgs...)
}

// T6（端到端）：真实 notifyModelProbeFailure 路径，spy transport 捕获 payload，
// 输入含 Gemini URL+key，transport 收到的正文必须无敏感值。M6 调用点守卫钉在
// sendProbeNotification 接缝上：任何绕过 buildModelProbeNotifyMessage 的拼装
// 都会在 payload 里留下敏感片段。
func TestNotifyModelProbeFailureTransportPayloadSanitized(t *testing.T) {
	ctx := setupProbeTaskTestEnv(t)

	// 渠道列表缓存需要一个已配置的渠道；内容无所谓，发送被 spy 拦截。
	if err := db.GetDB().WithContext(ctx).Create(&model.AlertNotifChannel{
		Name: "t6-spy", Type: string(model.AlertNotifWebhook), URL: "http://127.0.0.1:1/unreachable",
	}).Error; err != nil {
		t.Fatalf("seed notif channel: %v", err)
	}
	// alert 包的渠道缓存是进程级的：同进程先跑的其它测试可能已把它灌成空列表。
	// 插完渠道行必须失效缓存，否则 notifyModelProbeFailure 看到 0 渠道直接返回。
	alert.InvalidateNotifCache()

	spy := &payloadSpy{}
	restore := sendProbeNotification
	sendProbeNotification = spy.record
	t.Cleanup(func() { sendProbeNotification = restore })

	malicious := helper.GroupModelTestResult{
		ModelName: "m-leak",
		Passed:    false,
		Message:   `Get "https://generativelanguage.googleapis.com/v1beta/models/gemini:generateContent?key=AIzaSyFAKEKEY000000000000000000": dial tcp: lookup generativelanguage.googleapis.com: no such host`,
	}
	notifyModelProbeFailure(ctx, "g-leak", malicious)

	got := spy.all()
	if len(got) == 0 {
		t.Fatal("spy transport received nothing: notify path did not reach the transport (test wiring broken)")
	}
	for _, msg := range got {
		for _, frag := range []string{"AIza", "key=", "generativelanguage.googleapis.com", "https://", "dial tcp"} {
			if strings.Contains(msg, frag) {
				t.Fatalf("transport payload leaks %q (message=%q): transport-facing message must be the safe category", frag, msg)
			}
		}
		if !strings.Contains(msg, "network failure") {
			t.Fatalf("transport payload %q must carry the safe category", msg)
		}
	}
}

// T2 补充腿（未识别文本必须折叠）：不含任何已知签名的文本——哪怕里面藏着 key——
// 外发只能得到 internal failure。M3 的杀腿：只查 sk- 而不折叠未识别文本的变异，
// 会把这段带 AIza 的原文送出站。
func TestProbeNotifyUnrecognizedTextIsFolded(t *testing.T) {
	msg := "weird upstream said: credential=AIzaSyFAKEKEY999999 token=ghp_abcdef123456 (no known signature here)"
	got := helper.SanitizeProbeNotifyMessage(msg)
	if strings.Contains(got, "AIza") || strings.Contains(got, "ghp_") || strings.Contains(got, "credential") {
		t.Fatalf("unrecognized text with embedded keys must be fully folded, got %q", got)
	}
	if !strings.Contains(got, "internal failure") {
		t.Fatalf("unrecognized text must fold to internal failure, got %q", got)
	}
}

// T7（受控日志保留排障信息）：外发折叠后，服务端日志路径仍保留完整原因。
// notifyModelProbeFailure 只发外发面；日志面走 relay_logs（recordTestLog 写
// result.Message 原文）——这里断言 sanitize 不改输入、原文仍在 result 里。
func TestSanitizeKeepsDiagnosticOriginalIntact(t *testing.T) {
	raw := `Get "https://upstream.example/v1": dial tcp: lookup upstream.example: no such host`
	original := raw
	_ = helper.SanitizeProbeNotifyMessage(raw) // 外发折叠
	if raw != original {
		t.Fatalf("sanitizer must not mutate the caller's message: diagnostic original lives in relay_logs")
	}
	if got := helper.SanitizeProbeNotifyMessage(raw); strings.Contains(got, "upstream.example") {
		t.Fatalf("sanitized output must not contain the host, got %q", got)
	}
}

// TestProbeDueWithZeroLastProbedAFTERFailure：失败计数存在但 LastProbedAt 已过周期
// → 仍要再探（持续观察坏模型），这是 T-B8"只通知一次但继续计数"的时间维度前提。
func TestProbeDueAgainAfterIntervalEvenWhileFailing(t *testing.T) {
	last := time.Now().Add(-3 * time.Hour)
	if !probeDue(last, time.Now(), 2*time.Hour) {
		t.Fatal("probeDue(3h ago, 2h interval) = false, want true: failing models must keep being probed every interval")
	}
}

// ---- helpers ----

func readProbeState(t *testing.T, groupName string) model.ModelProbeState {
	t.Helper()
	var st model.ModelProbeState
	if err := db.GetDB().Where("group_name = ?", groupName).First(&st).Error; err != nil {
		t.Fatalf("read model_probe_states for %q: %v", groupName, err)
	}
	return st
}

func cachedProbeLogs(t *testing.T) []model.RelayLog {
	t.Helper()
	logs, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	defer lock.Unlock()
	out := make([]model.RelayLog, 0, len(logs))
	for _, l := range logs {
		if l.IsTest {
			out = append(out, l)
		}
	}
	return out
}
