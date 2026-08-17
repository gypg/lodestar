package relay

/*
429 渠道内延时重试（rate limit hold）的守卫测试。

行为契约（实现见 rate_limit_hold.go 与 retry_shared.go 的 ScopeSameChannel 分支）：
  - 仅「Code==429 且 Scope==ScopeSameChannel」触发；400 等 ScopeNone 终态
    （terminal_error.go 的 R-3 契约）绝不能被 hold 拦住；
  - 等待必须 select ctx.Done() 可被客户端断连中断，不用裸 time.Sleep；
  - 等待本身不是一次 upstream forward，不消耗 relay_max_total_attempts（R-5）；
  - 总等待受 max_wait 封顶，预算耗尽后走原来的换 Key/渠道流程；
  - 默认关闭，默认行为与历史完全一致。

断言全部落在精确值（命中次数、状态码、耗时区间）上。命中次数是核心观测点：
单渠道单 key 拓扑下，hold 是"429 后还有第二次转发"的唯一路径——
关掉 hold，同样的拓扑在第一次 429 后就耗尽（502、1 次命中）。
*/

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	chpkg "github.com/gypg/lodestar/internal/op/channel"
	grppkg "github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/relay/balancer"
	"github.com/gypg/lodestar/internal/transformer/inbound"
)

// holdEnv 记录假上游被真实打到的次数。hold 管的是"同一渠道等多久再试"，
// 只有上游 handler 被执行才算一次转发，等待本身必须不可见。
type holdEnv struct {
	mu   sync.Mutex
	hits int
}

func (e *holdEnv) hitCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hits
}

// initHoldEnv 搭一个单渠道单 key 的最小环境，并按参数设置 hold 开关与节奏。
// respond 以 1 起的命中序号给出每次上游应答，用来构造"先 429 后 200"这类脚本。
// llm=true 时挂 chat 端点（走 Handler），否则挂 images/generations（走 MediaHandler）。
func initHoldEnv(t *testing.T, requestModel string, groupID, channelID int, llm bool,
	holdEnabled bool, intervalSec, maxWaitSec int, respond func(hit int) (int, string)) *holdEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	env := &holdEnv{}

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

	// 钉住重试节奏：1 个 key 轮次、1 个 route 轮次。这样"429 之后还有转发"
	// 只可能来自 hold，不可能来自 key/route 轮次的自然重试。
	if err := setting.SetInt(dbmodel.SettingKeyRelayRetryCount, 1); err != nil {
		t.Fatalf("set retry count: %v", err)
	}
	if err := setting.SetInt(dbmodel.SettingKeyRelayRouteRetries, 1); err != nil {
		t.Fatalf("set route retries: %v", err)
	}

	if holdEnabled {
		if err := setting.SetString(dbmodel.SettingKeyRateLimitHoldEnabled, "true"); err != nil {
			t.Fatalf("set hold enabled: %v", err)
		}
	}
	if intervalSec > 0 {
		if err := setting.SetInt(dbmodel.SettingKeyRateLimitHoldInterval, intervalSec); err != nil {
			t.Fatalf("set hold interval: %v", err)
		}
	}
	if maxWaitSec > 0 {
		if err := setting.SetInt(dbmodel.SettingKeyRateLimitHoldMaxWait, maxWaitSec); err != nil {
			t.Fatalf("set hold max wait: %v", err)
		}
	}
	// 配置读回确认：hold 是否生效是整组断言的前提，静默失败会变成自证。
	cfg := getRateLimitHoldConfig()
	if cfg.Enabled != holdEnabled {
		t.Fatalf("hold enabled = %v, want %v (setting did not take effect)", cfg.Enabled, holdEnabled)
	}

	// 熔断器 / Auto 统计是进程级全局状态，独占 channelID 并前后清理，
	// 防止其它测试的失败记录把本测试的候选直接熔断掉。
	clearRuntime := func() {
		balancer.RemoveChannelEntries(channelID)
		balancer.RemoveChannelStats(channelID)
	}
	clearRuntime()
	t.Cleanup(clearRuntime)
	resetFailureHintCache()
	t.Cleanup(resetFailureHintCache)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.mu.Lock()
		env.hits++
		hit := env.hits
		env.mu.Unlock()
		status, body := respond(hit)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(upstream.Close)

	endpoint := dbmodel.EndpointTypeImageGeneration
	if llm {
		endpoint = dbmodel.EndpointTypeChat
	}

	chpkg.GetCache().Clear()
	chpkg.GetCache().Set(channelID, dbmodel.Channel{
		ID:       channelID,
		Name:     fmt.Sprintf("hold-upstream-%d", channelID),
		Type:     0, // OutboundTypeOpenAIChat（媒体链路不用 Type，LLM 链路用）
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}},
		Keys:     []dbmodel.ChannelKey{{ID: channelID*10 + 1, ChannelID: channelID, Enabled: true, ChannelKey: "sk-hold"}},
	})

	grppkg.GetCache().Clear()
	grppkg.GetCache().Set(groupID, dbmodel.Group{
		ID:           groupID,
		Name:         requestModel,
		EndpointType: endpoint,
		Mode:         dbmodel.GroupModeFailover,
		Items: []dbmodel.GroupItem{
			{ID: 1, GroupID: groupID, ChannelID: channelID, ModelName: "upstream-" + requestModel, Priority: 1, Weight: 1},
		},
	})
	grppkg.RebuildIndexes()

	t.Cleanup(func() {
		chpkg.GetCache().Clear()
		grppkg.GetCache().Clear()
		grppkg.RebuildIndexes()
		_ = db.Close()
	})

	return env
}

// newHoldRequest 构造发往被测入口的请求。llm=true 走 chat completions。
func newHoldRequest(t *testing.T, llm bool, model string) *http.Request {
	t.Helper()
	var body string
	path := "/v1/images/generations"
	if llm {
		path = "/v1/chat/completions"
		body = fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}]}`, model)
	} else {
		body = fmt.Sprintf(`{"model":%q,"prompt":"a cat","size":"1024x1024"}`, model)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// runHoldRelay 同步驱动一次被测入口，返回 recorder。
func runHoldRelay(llm bool, c *gin.Context) {
	if llm {
		Handler(dbmodel.EndpointTypeChat, inbound.InboundTypeOpenAIChat, c)
	} else {
		MediaHandler(MediaEndpointImageGeneration, c)
	}
}

func newHoldTestContext(rec *httptest.ResponseRecorder, req *http.Request, apiKeyID int) *gin.Context {
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key_id", apiKeyID)
	return c
}

// holdAlways429 / hold429Then200 是两个最常用的上游脚本。
func holdAlways429() func(int) (int, string) {
	return func(int) (int, string) {
		return http.StatusTooManyRequests, `{"error":{"message":"rate limited"}}`
	}
}

func hold429Then200(llm bool) func(int) (int, string) {
	successBody := `{"data":[{"b64_json":"aGk="}]}`
	if llm {
		successBody = `{"id":"chatcmpl-hold","object":"chat.completion","created":1700000000,` +
			`"model":"upstream","choices":[{"index":0,"message":{"role":"assistant","content":"recovered"},` +
			`"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`
	}
	return func(hit int) (int, string) {
		if hit == 1 {
			return http.StatusTooManyRequests, `{"error":{"message":"rate limited"}}`
		}
		return http.StatusOK, successBody
	}
}

// ============ 单元层：触发条件、预算、可中断等待 ============

// TestShouldHoldOnRateLimit_matrix
// ★ 触发条件矩阵：仅「429 + ScopeSameChannel + enabled」为真。
// 把 Code 判断放宽到 429+400、或把 Scope 判断拿掉，本表必红——
// 这是"400 终态不被 hold 拦截"的第一道闸（第二道在 e2e 层）。
func TestShouldHoldOnRateLimit_matrix(t *testing.T) {
	enabled := rateLimitHoldConfig{Enabled: true, Interval: 10 * time.Second, MaxWait: 60 * time.Second}
	disabled := rateLimitHoldConfig{Enabled: false, Interval: 10 * time.Second, MaxWait: 60 * time.Second}
	tests := []struct {
		name     string
		cfg      rateLimitHoldConfig
		decision RetryDecision
		want     bool
	}{
		{name: "429 same channel: hold", cfg: enabled, decision: RetryDecision{Scope: ScopeSameChannel, Code: 429, IsError: true}, want: true},
		{name: "400 scope none: never held (R-3)", cfg: enabled, decision: RetryDecision{Scope: ScopeNone, Code: 400, IsError: true}, want: false},
		{name: "400 mapped as same channel: still not a hold (only 429 qualifies)", cfg: enabled, decision: RetryDecision{Scope: ScopeSameChannel, Code: 400, IsError: true}, want: false},
		{name: "401 same channel: immediate key switch", cfg: enabled, decision: RetryDecision{Scope: ScopeSameChannel, Code: 401, IsError: true}, want: false},
		{name: "403 same channel: immediate key switch", cfg: enabled, decision: RetryDecision{Scope: ScopeSameChannel, Code: 403, IsError: true}, want: false},
		{name: "empty output (200 same channel): immediate key switch", cfg: enabled, decision: RetryDecision{Scope: ScopeSameChannel, Code: 200, IsError: true}, want: false},
		{name: "429 next channel: not a hold case", cfg: enabled, decision: RetryDecision{Scope: ScopeNextChannel, Code: 429, IsError: true}, want: false},
		{name: "429 abort all (stream written): not held", cfg: enabled, decision: RetryDecision{Scope: ScopeAbortAll, Code: 429, IsError: true}, want: false},
		{name: "429 same channel but hold disabled", cfg: disabled, decision: RetryDecision{Scope: ScopeSameChannel, Code: 429, IsError: true}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldHoldOnRateLimit(tt.cfg, tt.decision); got != tt.want {
				t.Errorf("shouldHoldOnRateLimit(%+v) = %v, want %v", tt.decision, got, tt.want)
			}
		})
	}
}

// TestCanContinueRateLimitHold_budget
// 剩余预算不足一整轮 interval 时必须停：这是总等待封顶的核心判断。
func TestCanContinueRateLimitHold_budget(t *testing.T) {
	cfg := rateLimitHoldConfig{Enabled: true, Interval: 10 * time.Second, MaxWait: 60 * time.Second}
	tests := []struct {
		waited time.Duration
		want   bool
	}{
		{waited: 0, want: true},
		{waited: 50 * time.Second, want: true},  // 50+10 <= 60
		{waited: 51 * time.Second, want: false}, // 51+10 > 60
		{waited: 60 * time.Second, want: false},
	}
	for _, tt := range tests {
		if got := canContinueRateLimitHold(cfg, tt.waited); got != tt.want {
			t.Errorf("canContinueRateLimitHold(waited=%s) = %v, want %v", tt.waited, got, tt.want)
		}
	}
	if canContinueRateLimitHold(rateLimitHoldConfig{Enabled: false, Interval: 10 * time.Second, MaxWait: 60 * time.Second}, 0) {
		t.Error("canContinueRateLimitHold(disabled) = true, want false")
	}
}

// TestWaitRateLimitHold_interruptibleByContext
// ★ 可中断性单元守卫：ctx 取消必须立刻返回 false，不能睡满 interval。
// 换回裸 time.Sleep 后 elapsed 断言必红。
func TestWaitRateLimitHold_interruptibleByContext(t *testing.T) {
	cfg := rateLimitHoldConfig{Enabled: true, Interval: 5 * time.Second, MaxWait: 60 * time.Second}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if waitRateLimitHold(ctx, cfg, "test-channel", 0) {
		t.Fatal("waitRateLimitHold() = true on cancelled context, want false")
	}
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("cancelled wait took %s, want < 1s (interval is 5s — the wait is not interruptible)", elapsed)
	}

	// 正常等完一轮（用小预算，别拖慢测试）。
	small := rateLimitHoldConfig{Enabled: true, Interval: 50 * time.Millisecond, MaxWait: time.Second}
	start = time.Now()
	if !waitRateLimitHold(context.Background(), small, "test-channel", 0) {
		t.Fatal("waitRateLimitHold() = false on live context, want true")
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("full wait took %s, want >= 40ms (interval is 50ms)", elapsed)
	}

	// 预算已耗尽：立即放弃，不再等待。
	start = time.Now()
	if waitRateLimitHold(context.Background(), cfg, "test-channel", cfg.MaxWait) {
		t.Fatal("waitRateLimitHold() = true with no remaining budget, want false")
	}
	if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
		t.Fatalf("no-budget wait took %s, want immediate return", elapsed)
	}

	// nil ctx 不得 panic（与上游 octopus 实现保持一致的防御）。
	if !waitRateLimitHold(nil, small, "test-channel", 0) {
		t.Fatal("waitRateLimitHold(nil ctx) = false, want true")
	}
}

// TestGetRateLimitHoldConfig_defaultsAndClamp
// 默认关闭（不改变默认行为）；interval 超过 maxWait 时被压回 maxWait，
// 否则一次等待就会击穿总预算。
func TestGetRateLimitHoldConfig_defaultsAndClamp(t *testing.T) {
	initHoldSettingDB(t)

	cfg := getRateLimitHoldConfig()
	if cfg.Enabled {
		t.Error("default rate_limit_hold_enabled = true, want false (must not change default behavior)")
	}
	if cfg.Interval != 10*time.Second {
		t.Errorf("default interval = %s, want 10s", cfg.Interval)
	}
	if cfg.MaxWait != 60*time.Second {
		t.Errorf("default max wait = %s, want 60s", cfg.MaxWait)
	}

	if err := setting.SetInt(dbmodel.SettingKeyRateLimitHoldInterval, 5); err != nil {
		t.Fatalf("set interval: %v", err)
	}
	if err := setting.SetInt(dbmodel.SettingKeyRateLimitHoldMaxWait, 3); err != nil {
		t.Fatalf("set max wait: %v", err)
	}
	cfg = getRateLimitHoldConfig()
	if cfg.Interval != 3*time.Second {
		t.Errorf("interval = %s, want 3s (clamped to max wait)", cfg.Interval)
	}
	if cfg.MaxWait != 3*time.Second {
		t.Errorf("max wait = %s, want 3s", cfg.MaxWait)
	}

	if err := setting.SetString(dbmodel.SettingKeyRateLimitHoldEnabled, "true"); err != nil {
		t.Fatalf("set enabled: %v", err)
	}
	if cfg = getRateLimitHoldConfig(); !cfg.Enabled {
		t.Error("rate_limit_hold_enabled = false after setting true, want true")
	}
}

// initHoldSettingDB 只搭 settings 所需的最小 DB（不挂渠道），给纯配置测试用。
func initHoldSettingDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
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
	t.Cleanup(func() { _ = db.Close() })
}

// ============ e2e 层（媒体链路）：走 MediaHandler 的完整重试链 ============

// TestRateLimitHold_media429RetriesSameChannelAndSucceeds
// ★ 主行为：429 后在同一渠道内等一轮 interval 再试，第二次成功。
// 拓扑是单渠道单 key + retryCount=1/routeRetries=1：关掉 hold 时第一次 429
// 就把 key 与渠道耗尽（1 次命中、502），所以"2 次命中 + 200"只能来自 hold。
// elapsed >= 900ms 同时证明第二次转发前真的等了（而不是立即重发）。
func TestRateLimitHold_media429RetriesSameChannelAndSucceeds(t *testing.T) {
	const requestModel = "hold-media-retry"
	env := initHoldEnv(t, requestModel, 7401, 9411, false, true, 1, 5, hold429Then200(false))

	rec := httptest.NewRecorder()
	start := time.Now()
	runHoldRelay(false, newHoldTestContext(rec, newHoldRequest(t, false, requestModel), 59401))

	hits := env.hitCount()
	if hits != 2 {
		t.Fatalf("upstream hit count = %d, want exactly 2 (first 429, held retry succeeds); "+
			"1 means the hold never retried, 3+ means it retried more than configured", hits)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("client status = %d, want 200 (held retry should deliver the success payload); body=%s",
			rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "b64_json") {
		t.Fatalf("client body does not carry the upstream success payload: %s", rec.Body.String())
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("relay finished in %s, want >= 900ms — the retry must wait one interval (1s) before forwarding again", elapsed)
	}
}

// TestRateLimitHold_disabledByDefaultKeepsImmediateFailover
// ★ 默认关闭：同样的"429 后 200"上游，默认配置下不得出现第二次转发。
// 断言落在精确值上：1 次命中、502 终态。hold 误默认开启时这里必红
// （第二次命中出现，且响应晚一个 interval）。
func TestRateLimitHold_disabledByDefaultKeepsImmediateFailover(t *testing.T) {
	const requestModel = "hold-media-disabled"
	env := initHoldEnv(t, requestModel, 7402, 9412, false, false, 0, 0, hold429Then200(false))

	rec := httptest.NewRecorder()
	start := time.Now()
	runHoldRelay(false, newHoldTestContext(rec, newHoldRequest(t, false, requestModel), 59402))

	if hits := env.hitCount(); hits != 1 {
		t.Fatalf("upstream hit count = %d, want exactly 1 — with hold disabled the single key must be "+
			"exhausted on the first 429 with no delayed retry", hits)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("client status = %d, want 502 (all channels failed after immediate failover); body=%s",
			rec.Code, rec.Body.String())
	}
	if elapsed := time.Since(start); elapsed >= 900*time.Millisecond {
		t.Fatalf("relay took %s with hold disabled, want < 900ms — a default-on hold would have waited an interval", elapsed)
	}
}

// TestRateLimitHold_maxWaitCapsTotalHoldBudget
// ★ 总等待封顶：interval=1s、maxWait=2s、上游恒 429。
// 期望恰好 3 次转发（1 次初始 + 2 轮被预算允许的等待重试），第 3 次 429 后
// 预算耗尽（waited=2s，再等 1s 会超 2s），走回立即换 Key/渠道的老路并耗尽。
// canContinueRateLimitHold 被改坏（恒真）时转发数会一路涨到 maxTotalAttempts=5
// 的闸门才停 → 红，且不会死循环。
func TestRateLimitHold_maxWaitCapsTotalHoldBudget(t *testing.T) {
	const requestModel = "hold-media-cap"
	env := initHoldEnv(t, requestModel, 7403, 9413, false, true, 1, 2, holdAlways429())

	// 安全闸：预算判断被改坏时由它兜底终止，测试变红而不是挂死。
	if err := setting.SetInt(dbmodel.SettingKeyRelayMaxTotalAttempts, 5); err != nil {
		t.Fatalf("set max total attempts: %v", err)
	}

	rec := httptest.NewRecorder()
	start := time.Now()
	runHoldRelay(false, newHoldTestContext(rec, newHoldRequest(t, false, requestModel), 59403))

	hits := env.hitCount()
	if hits != 3 {
		t.Fatalf("upstream hit count = %d, want exactly 3 (initial forward + ceil(2s/1s) held retries); "+
			"more means max_wait no longer caps the hold budget", hits)
	}
	if elapsed := time.Since(start); elapsed < 1900*time.Millisecond {
		t.Fatalf("relay finished in %s, want >= 1.9s — two held waits of 1s each must actually elapse", elapsed)
	}
	// 上界：两道预算闸（canContinue 预检 + waitRateLimitHold 的剩余预算钳制）
	// 都被改坏时会出现第三次 1s 等待，耗时越过 3.2s → 红。只坏 canContinue
	// 一道时 e2e 仍绿（另一道把行为兜住了），由 TestCanContinueRateLimitHold_budget
	// 在单元层负责抓 —— 分层检测。
	if elapsed := time.Since(start); elapsed > 3200*time.Millisecond {
		t.Fatalf("relay took %s, want <= 3.2s — the hold waited past its max_wait budget", elapsed)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("client status = %d, want 502 after the budget is spent; body=%s", rec.Code, rec.Body.String())
	}
}

// TestRateLimitHold_mediaClientDisconnectInterruptsHold
// ★ 可中断性 e2e：interval=5s，上游恒 429，150ms 后取消请求上下文。
// MediaHandler 必须在远小于 interval 的时间内返回（此处 3s 死线），
// 且上游只被打 1 次（hold 被打断，没有第二次转发）。
// 等待若不可中断（裸 time.Sleep / 等 operationCtx），3s 死线必红。
func TestRateLimitHold_mediaClientDisconnectInterruptsHold(t *testing.T) {
	const requestModel = "hold-media-cancel"
	env := initHoldEnv(t, requestModel, 7404, 9414, false, true, 5, 60, holdAlways429())

	req := newHoldRequest(t, false, requestModel)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	rec := httptest.NewRecorder()
	c := newHoldTestContext(rec, req, 59404)
	done := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(done)
		runHoldRelay(false, c)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("MediaHandler did not return within 3s of client disconnect — the hold wait is not interruptible")
	}
	if elapsed := time.Since(start); elapsed >= 3*time.Second {
		t.Fatalf("relay took %s to notice the disconnect, want < 3s (hold interval is 5s)", elapsed)
	}
	if hits := env.hitCount(); hits != 1 {
		t.Fatalf("upstream hit count = %d, want exactly 1 — a cancelled hold must not forward again", hits)
	}
}

// TestRateLimitHold_scopeNone400NotHeld
// ★ R-3 回归守卫：hold 开启时，400 + context_length_exceeded 仍必须原样
// 回给下游、恰好 1 次命中、不等待。把 hold 触发条件放宽到 400（无论怎么改，
// 只要 400 被引流进 hold），命中数/状态码/耗时三者之一必红。
func TestRateLimitHold_scopeNone400NotHeld(t *testing.T) {
	const requestModel = "hold-media-400"
	env := initHoldEnv(t, requestModel, 7405, 9415, false, true, 5, 60, func(int) (int, string) {
		return http.StatusBadRequest, upstream400Body
	})

	rec := httptest.NewRecorder()
	start := time.Now()
	runHoldRelay(false, newHoldTestContext(rec, newHoldRequest(t, false, requestModel), 59405))

	if hits := env.hitCount(); hits != 1 {
		t.Fatalf("upstream hit count = %d, want exactly 1 — a 400 terminal error must never be held or retried (R-3)", hits)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("client status = %d, want 400 (upstream status passed through); 502 means R-3 regressed. body=%s",
			rec.Code, rec.Body.String())
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("client body is not JSON: %v; body=%s", err, rec.Body.String())
	}
	if payload.Error.Code != "context_length_exceeded" {
		t.Errorf("error.code = %q, want context_length_exceeded; body=%s", payload.Error.Code, rec.Body.String())
	}
	if elapsed := time.Since(start); elapsed >= 900*time.Millisecond {
		t.Fatalf("relay took %s on a terminal 400, want < 900ms — a held 400 would have waited an interval first", elapsed)
	}
}

// TestRateLimitHold_waitDoesNotConsumeForwardBudget
// ★ R-5 回归守卫：hold 等待不算一次 upstream forward。
// interval=1s、maxWait=60s、上游恒 429、relay_max_total_attempts=2：
// 两次真实转发被两次 1s 等待隔开，随后闸门收尾。
// 若等待被算进 forward 计数（比如等待时写 attempt 记录或推进计数器），
// 第一次等待后配额即被视为耗尽 → 命中数变 1 → 红。
// body 里必须出现配额闸门的错误文案，证明确实是闸门收尾而不是别的失败路径。
func TestRateLimitHold_waitDoesNotConsumeForwardBudget(t *testing.T) {
	const requestModel = "hold-media-budget"
	env := initHoldEnv(t, requestModel, 7406, 9416, false, true, 1, 60, holdAlways429())

	if err := setting.SetInt(dbmodel.SettingKeyRelayMaxTotalAttempts, 2); err != nil {
		t.Fatalf("set max total attempts: %v", err)
	}

	rec := httptest.NewRecorder()
	start := time.Now()
	runHoldRelay(false, newHoldTestContext(rec, newHoldRequest(t, false, requestModel), 59406))

	hits := env.hitCount()
	if hits != 2 {
		t.Fatalf("upstream hit count = %d, want exactly 2 — the cap counts real forwards only, so both "+
			"budget slots must be spent on actual 429 forwards straddling a 1s hold; "+
			"1 means the wait consumed a forward slot (R-5 regression)", hits)
	}
	if elapsed := time.Since(start); elapsed < 1900*time.Millisecond {
		t.Fatalf("relay finished in %s, want >= 1.9s — two real forwards must be separated by two 1s holds", elapsed)
	}
	if !strings.Contains(rec.Body.String(), "max total attempts") {
		t.Fatalf("response body must come from the forward-budget gate, got: %s", rec.Body.String())
	}
}

// ============ e2e 层（LLM 链路）：走 Handler 的完整重试链 ============

// TestRateLimitHold_llm429RetriesSameChannelAndSucceeds
// ★ LLM 链路同步生效：同一份 retryWithChannels 接线 + HoldCtx=req.clientCtx。
// 单渠道单 key、429 后 200：期望 2 次命中、200、且等满一个 interval。
// LLM 侧的额外意义：attempt() 在 429 时写了 per-(key,model) 冷却（relay.go
// RecordKeyModelCooldown），hold 复试前必须清掉，否则唯一 key 选不回来，
// 这里就会退化成 1 次命中 + 502。
func TestRateLimitHold_llm429RetriesSameChannelAndSucceeds(t *testing.T) {
	const requestModel = "hold-llm-retry"
	env := initHoldEnv(t, requestModel, 7407, 9417, true, true, 1, 5, hold429Then200(true))

	rec := httptest.NewRecorder()
	start := time.Now()
	runHoldRelay(true, newHoldTestContext(rec, newHoldRequest(t, true, requestModel), 59407))

	hits := env.hitCount()
	if hits != 2 {
		t.Fatalf("upstream hit count = %d, want exactly 2 (first 429, held retry succeeds; "+
			"1 likely means the per-model key cooldown was not cleared before the held retry)", hits)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("client status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "recovered") {
		t.Fatalf("client body does not carry the held retry's completion content: %s", rec.Body.String())
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("relay finished in %s, want >= 900ms — the held retry must wait one interval", elapsed)
	}
}

// TestRateLimitHold_llmClientDisconnectInterruptsHold
// ★ LLM 侧的可中断性：这条专门守 HoldCtx 的接线。LLM 的 Ctx 是
// operationCtx（context.Background 派生，客户端断连不会取消它），
// 若 hold 等在 Ctx 而不是 clientCtx 上，取消请求上下文后 Handler 会
// 睡满 5s interval 才返回 → 3s 死线必红。
func TestRateLimitHold_llmClientDisconnectInterruptsHold(t *testing.T) {
	const requestModel = "hold-llm-cancel"
	env := initHoldEnv(t, requestModel, 7408, 9418, true, true, 5, 60, holdAlways429())

	req := newHoldRequest(t, true, requestModel)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	rec := httptest.NewRecorder()
	c := newHoldTestContext(rec, req, 59408)
	done := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(done)
		runHoldRelay(true, c)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Handler did not return within 3s of client disconnect — the hold is waiting on a context " +
			"that is not cancelled by client disconnect (HoldCtx wiring broken)")
	}
	if elapsed := time.Since(start); elapsed >= 3*time.Second {
		t.Fatalf("relay took %s to notice the disconnect, want < 3s (hold interval is 5s)", elapsed)
	}
	if hits := env.hitCount(); hits != 1 {
		t.Fatalf("upstream hit count = %d, want exactly 1 — a cancelled hold must not forward again", hits)
	}
}
