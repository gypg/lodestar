package relay

/*
R-4 调用点守卫 — media_relay.go 的 OnSuccess / OnFinalFailure 必须把 attempts 传下去。

缺陷（2026-08-08 复现确认）：两个终态回调原本硬传 nil，于是媒体请求的
relay 日志恒 TotalAttempts=0、relay_log_attempts 一行不写、
StatsSiteModelHourlyRecordAttempts 拿到空切片直接空转。
后果：A 渠道失败→B 渠道成功这种失败转移，在日志和站点模型统计里
**完全不可见**（连成功那一跳都不计），只有走到 OnExhausted（全部候选耗尽）
的请求才有 attempts —— 而那恰恰是最少见的路径。

入口必须在调用点的**上游**：本文件所有测试都跑 MediaHandler，
不直接调 recordMediaRelayLog。若从 recordMediaRelayLog 往下切入，
守的是"这个函数会不会存 attempts"，而不是"回调有没有把 attempts 传给它"——
那正是 [[lodestar-worker-false-evidence]] 第七变体踩过的坑。

观测点选 relaylog 缓存（RelayLogAdd 同步 append 进 relayLogCache），
断言的是 TotalAttempts / Attempts 的**内容与顺序**，不只是"非空"。
*/

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	chpkg "github.com/gypg/lodestar/internal/op/channel"
	grppkg "github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/op/relaylog"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/relay/balancer"
)

// attemptsEnv 是驱动 MediaHandler 做多渠道转移所需的最小环境。
type attemptsEnv struct {
	mu       sync.Mutex
	hitOrder []string // 假上游被打到的先后顺序，用于确认转移真的发生了
}

// upstreamSpec 描述一个假上游渠道：名字 + 它要返回的状态码。
type upstreamSpec struct {
	name       string
	channelID  int
	statusCode int
}

// initMediaAttemptsEnv 起 N 个假上游，按 spec 顺序注册成同一个分组的候选。
// 用 Failover 模式 + 递增 Priority 固定候选顺序（RoundRobin 的 Candidates
// 带全局计数器，跨测试会漂移，断言顺序时不能用）。
func initMediaAttemptsEnv(t *testing.T, requestModel string, specs []upstreamSpec) *attemptsEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	env := &attemptsEnv{}

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := db.GetDB().AutoMigrate(&dbmodel.Setting{}); err != nil {
		t.Fatalf("migrate setting: %v", err)
	}
	if err := setting.RefreshCache(nil); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}

	// 熔断器和 Auto 统计都是进程级全局状态：上一个测试在同一 channelID 上打出的
	// 失败会让本测试的候选被直接 SkipCircuitBreak 掉（attempts 变成 circuit_break
	// 而不是 failed，顺序断言就崩了）。每个测试用独占的 channelID，并在前后各清一次。
	// 用的是生产已有的导出函数，不为测试新增导出面。
	clearChannelRuntime := func() {
		for _, spec := range specs {
			balancer.RemoveChannelEntries(spec.channelID)
			balancer.RemoveChannelStats(spec.channelID)
		}
	}
	clearChannelRuntime()
	t.Cleanup(clearChannelRuntime)

	chpkg.GetCache().Clear()
	grppkg.GetCache().Clear()

	items := make([]dbmodel.GroupItem, 0, len(specs))
	for i, spec := range specs {
		spec := spec
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			env.mu.Lock()
			env.hitOrder = append(env.hitOrder, spec.name)
			env.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(spec.statusCode)
			if spec.statusCode == http.StatusOK {
				_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGk="}]}`))
			} else {
				_, _ = w.Write([]byte(`{"error":{"message":"upstream refused"}}`))
			}
		}))
		t.Cleanup(upstream.Close)

		chpkg.GetCache().Set(spec.channelID, dbmodel.Channel{
			ID:       spec.channelID,
			Name:     spec.name,
			Enabled:  true,
			BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}},
			Keys: []dbmodel.ChannelKey{
				{ID: spec.channelID*10 + 1, ChannelID: spec.channelID, Enabled: true, ChannelKey: "sk-" + spec.name},
			},
		})
		items = append(items, dbmodel.GroupItem{
			ID:        i + 1,
			GroupID:   7201,
			ChannelID: spec.channelID,
			ModelName: "upstream-" + spec.name,
			Priority:  i + 1,
			Weight:    1,
		})
	}

	grppkg.GetCache().Set(7201, dbmodel.Group{
		ID:           7201,
		Name:         requestModel,
		EndpointType: dbmodel.EndpointTypeImageGeneration,
		Mode:         dbmodel.GroupModeFailover,
		Items:        items,
	})
	grppkg.RebuildIndexes()

	// 清空日志缓存，让断言只看到本测试产生的那一条。
	restore := relaylog.SetCacheForTest(nil)
	t.Cleanup(restore)

	t.Cleanup(func() {
		chpkg.GetCache().Clear()
		grppkg.GetCache().Clear()
		grppkg.RebuildIndexes()
		_ = db.Close()
	})

	return env
}

// newJSONImageRequest 造一个 images/generations 的 JSON 请求。
func newJSONImageRequest(t *testing.T, model string) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]any{"model": model, "prompt": "a cat", "size": "1024x1024"})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// runMediaHandler 发一次请求并返回响应记录器。
func runMediaHandler(t *testing.T, requestModel string, apiKeyID int) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = newJSONImageRequest(t, requestModel)
	c.Set("api_key_id", apiKeyID)
	MediaHandler(MediaEndpointImageGeneration, c)
	return rec
}

// loggedRelay 取出本次请求写入的那条 relay 日志。
func loggedRelay(t *testing.T, requestModel string) dbmodel.RelayLog {
	t.Helper()
	logs, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	defer lock.Unlock()
	var found []dbmodel.RelayLog
	for _, l := range logs {
		if l.RequestModelName == requestModel {
			found = append(found, l)
		}
	}
	if len(found) != 1 {
		t.Fatalf("relay log count for model %s: want exactly 1, got %d", requestModel, len(found))
	}
	return found[0]
}

// attemptSummary 把 attempts 压成 "渠道名:状态" 列表，便于断言顺序。
func attemptSummary(attempts []dbmodel.ChannelAttempt) []string {
	out := make([]string, 0, len(attempts))
	for _, a := range attempts {
		out = append(out, fmt.Sprintf("%s:%s", a.ChannelName, a.Status))
	}
	return out
}

// TestMediaHandler_successAfterFailover_recordsAllAttempts
// ★ R-4 核心守卫（成功路径 / OnSuccess）。
//
// 拓扑：ch-a 恒 500（ScopeNextChannel → 换渠道），ch-b 恒 200。
// 期望 attempts == [ch-a:failed, ch-b:success]，TotalAttempts == 2。
//
// 回归时的表现：OnSuccess 传 nil → attempts 为空、TotalAttempts == 0，
// 即"失败转移这件事在日志里彻底消失，连成功那跳也不计"。
// 断言写死顺序而不只是长度：attempts 的价值就在于能看出先打了谁、谁救了场。
func TestMediaHandler_successAfterFailover_recordsAllAttempts(t *testing.T) {
	const requestModel = "attempts-image-model"
	env := initMediaAttemptsEnv(t, requestModel, []upstreamSpec{
		{name: "ch-a", channelID: 4201, statusCode: http.StatusInternalServerError},
		{name: "ch-b", channelID: 4202, statusCode: http.StatusOK},
	})

	rec := runMediaHandler(t, requestModel, 56001)
	if rec.Code != http.StatusOK {
		t.Fatalf("MediaHandler status: want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}

	// 前置校验：转移真的发生了，否则下面的断言是空转。
	env.mu.Lock()
	hits := append([]string(nil), env.hitOrder...)
	env.mu.Unlock()
	if len(hits) != 2 || hits[0] != "ch-a" || hits[1] != "ch-b" {
		t.Fatalf("upstream hit order: want [ch-a ch-b], got %v — failover did not happen as designed", hits)
	}

	relayLog := loggedRelay(t, requestModel)

	// ★ 核心断言：两跳都在，且顺序正确。
	got := attemptSummary(relayLog.Attempts)
	want := []string{"ch-a:failed", "ch-b:success"}
	if len(got) != len(want) {
		t.Fatalf("attempts: want %v, got %v — empty means OnSuccess passed attempts=nil (R-4 regression)", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("attempts[%d]: want %q, got %q (full: %v)", i, want[i], got[i], got)
		}
	}
	if relayLog.TotalAttempts != 2 {
		t.Errorf("TotalAttempts: want 2, got %d (0 means attempts=nil was passed)", relayLog.TotalAttempts)
	}
}

// TestMediaHandler_terminalClientError_recordsAttempts
// ★ R-4 第二个调用点守卫（终态失败路径 / OnFinalFailure）。
//
// 400 是 ScopeNone：不重试，直接走 OnFinalFailure（不是 OnExhausted）。
// 这正是 R-4 的第二处 nil。期望 attempts == [ch-solo:failed]。
//
// 注意这条路径与 OnExhausted 互斥（OnFinalFailure 返回 true 即 return），
// 所以它无法被"OnExhausted 那处本来就传了 allAttempts"覆盖到 —— 必须单独守。
func TestMediaHandler_terminalClientError_recordsAttempts(t *testing.T) {
	const requestModel = "attempts-image-model-400"
	env := initMediaAttemptsEnv(t, requestModel, []upstreamSpec{
		{name: "ch-solo", channelID: 4203, statusCode: http.StatusBadRequest},
	})

	rec := runMediaHandler(t, requestModel, 56002)
	// R-3 已修：400 原样回写下游，不吞成 502。
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("MediaHandler status: want 400 passthrough, got %d (body=%s)", rec.Code, rec.Body.String())
	}

	env.mu.Lock()
	hits := len(env.hitOrder)
	env.mu.Unlock()
	if hits != 1 {
		t.Fatalf("upstream hit count: want exactly 1 (400 must not be retried), got %d", hits)
	}

	relayLog := loggedRelay(t, requestModel)

	got := attemptSummary(relayLog.Attempts)
	want := []string{"ch-solo:failed"}
	if len(got) != len(want) {
		t.Fatalf("attempts: want %v, got %v — empty means OnFinalFailure passed attempts=nil (R-4 regression)", want, got)
	}
	if got[0] != want[0] {
		t.Errorf("attempts[0]: want %q, got %q", want[0], got[0])
	}
	if relayLog.TotalAttempts != 1 {
		t.Errorf("TotalAttempts: want 1, got %d (0 means attempts=nil was passed)", relayLog.TotalAttempts)
	}
}

// TestMediaHandler_singleSuccess_recordsOneAttempt
// 位置/顺序敏感性：一跳成功时必须**恰好**记 1 条 success。
//
// 这条挡的是"把 snapshotAttempts 挪到错误位置"这类变异，比如挪到
// span.End 之前（拿到 0 条）或在两个回调间复用同一份快照。
// 只断言"非空"是挡不住的 —— 顺序/位置类变异要求死数字。
func TestMediaHandler_singleSuccess_recordsOneAttempt(t *testing.T) {
	const requestModel = "attempts-image-model-solo-ok"
	initMediaAttemptsEnv(t, requestModel, []upstreamSpec{
		{name: "ch-only", channelID: 4204, statusCode: http.StatusOK},
	})

	rec := runMediaHandler(t, requestModel, 56003)
	if rec.Code != http.StatusOK {
		t.Fatalf("MediaHandler status: want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}

	relayLog := loggedRelay(t, requestModel)

	got := attemptSummary(relayLog.Attempts)
	if len(got) != 1 {
		t.Fatalf("attempts: want exactly [ch-only:success], got %v", got)
	}
	if got[0] != "ch-only:success" {
		t.Errorf("attempts[0]: want %q, got %q", "ch-only:success", got[0])
	}
	if relayLog.TotalAttempts != 1 {
		t.Errorf("TotalAttempts: want 1, got %d", relayLog.TotalAttempts)
	}
}
