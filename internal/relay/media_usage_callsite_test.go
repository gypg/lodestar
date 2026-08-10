package relay

/*
P1 #11 —— 媒体真实 usage 的**调用点守卫**（入口在 MediaHandler，不是被测函数本身）。

为什么必须走 MediaHandler：见 [[lodestar-worker-false-evidence]] 第七变体和
media_handler_callsite_test.go 的教训——显式调用 parseMediaUsage / recordMediaRelayLog
的测试守的是"函数行为对不对"，守不住"生产代码还调不调它"。本文件的每个测试都
从 MediaHandler 进入，观测点是 relaylog 缓存里的 token 字段和 ChargeKey 的实参，
所以把 handleJSONResponse 的 tee 删掉、或把 usage 参数改回硬编码零值，这里都会红。

覆盖的三条链：
  1. 非流式 JSON（images/generations）→ handleJSONResponse 的 io.MultiWriter tee
  2. SSE 流式 → handleSSEResponse 的逐行 tee，且取最后一帧
  3. 图床开启（响应体被 ReadAll + 重新 marshal）→ usage 既要进计费也不能从响应体丢失
*/

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	billing "github.com/gypg/lodestar/internal/op/billing"
	chpkg "github.com/gypg/lodestar/internal/op/channel"
	grppkg "github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/op/relaylog"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/stats"
)

// mediaUsageEnv 是驱动 MediaHandler 所需的最小环境（照抄 media_handler_callsite_test.go
// 那套三缓存 seam harness：channel 缓存 + group 缓存 + RebuildIndexes）。
type mediaUsageEnv struct {
	charges []float64
}

// initMediaUsageEnv 起一个假上游（响应体由 upstreamBody 决定），把渠道/分组塞进缓存，
// 并给 requestModel 挂 expr。contentType 决定走 JSON 还是 SSE 分支。
func initMediaUsageEnv(t *testing.T, requestModel, expr, contentType, upstreamBody string) *mediaUsageEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	env := &mediaUsageEnv{}

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
	if expr != "" {
		if err := setting.SetString(dbmodel.SettingKeyBillingExpr,
			fmt.Sprintf(`{%q:%q}`, requestModel, expr)); err != nil {
			t.Fatalf("set billing expr: %v", err)
		}
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(upstreamBody))
	}))
	t.Cleanup(upstream.Close)

	const channelID = 4301
	chpkg.GetCache().Clear()
	chpkg.GetCache().Set(channelID, dbmodel.Channel{
		ID:       channelID,
		Name:     "usage-upstream",
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}},
		Keys: []dbmodel.ChannelKey{
			{ID: 93, ChannelID: channelID, Enabled: true, ChannelKey: "sk-usage"},
		},
	})

	grppkg.GetCache().Clear()
	grppkg.GetCache().Set(7301, dbmodel.Group{
		ID:           7301,
		Name:         requestModel,
		EndpointType: dbmodel.EndpointTypeImageGeneration,
		Mode:         dbmodel.GroupModeRoundRobin,
		Items: []dbmodel.GroupItem{
			{ID: 1, GroupID: 7301, ChannelID: channelID, ModelName: "upstream-image-model", Priority: 1, Weight: 1},
		},
	})
	grppkg.RebuildIndexes()

	prev := billing.CallRecorder
	billing.CallRecorder = func(_ int, _ string, _, _ int, cost float64) {
		env.charges = append(env.charges, cost)
	}
	t.Cleanup(func() { billing.CallRecorder = prev })

	t.Cleanup(func() {
		chpkg.GetCache().Clear()
		grppkg.GetCache().Clear()
		grppkg.RebuildIndexes()
		_ = db.Close()
	})

	return env
}

// runMediaImageRequest 发一个 JSON 图片生成请求走 MediaHandler，返回响应记录。
func runMediaImageRequest(t *testing.T, requestModel string, apiKeyID int, extraBody string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"model":%q,"prompt":"a cat"%s}`, requestModel, extraBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key_id", apiKeyID)

	MediaHandler(MediaEndpointImageGeneration, c)
	return rec
}

// lastRelayLog 取 relaylog 内存缓存里最新一条（RelayLogAdd 只进缓存）。
func lastRelayLog(t *testing.T) dbmodel.RelayLog {
	t.Helper()
	logs, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	defer lock.Unlock()
	if len(logs) == 0 {
		t.Fatalf("relay log cache is empty — recordMediaRelayLog never ran")
	}
	return logs[len(logs)-1]
}

// TestMediaHandler_jsonUsage_persistedToRelayLog
// ★ 核心调用点守卫：非流式 JSON 响应里的 usage 必须落到 relay_logs 的 token 字段。
// 删掉 handleJSONResponse 的 tee（或把 recordMediaRelayLog 的 usage 传回零值）
// → input_tokens/output_tokens 回到 0 → 本测试红。
// 期望值写死（100/40/9），不写"非零"。
func TestMediaHandler_jsonUsage_persistedToRelayLog(t *testing.T) {
	const requestModel = "usage-image-model"
	upstreamBody := `{"created":1,"data":[{"b64_json":"aGk="}],` +
		`"usage":{"input_tokens":100,"output_tokens":40,` +
		`"input_tokens_details":{"text_tokens":30,"image_tokens":70,"cached_tokens":9}}}`
	initMediaUsageEnv(t, requestModel, "", "application/json", upstreamBody)

	rec := runMediaImageRequest(t, requestModel, 66001, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("MediaHandler status: want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}

	rl := lastRelayLog(t)
	if rl.InputTokens != 100 {
		t.Errorf("relayLog.InputTokens: want 100, got %d — upstream usage was not captured "+
			"(0 means the handleJSONResponse tee or the recordMediaRelayLog usage argument is gone)", rl.InputTokens)
	}
	if rl.OutputTokens != 40 {
		t.Errorf("relayLog.OutputTokens: want 40, got %d", rl.OutputTokens)
	}
	if rl.CacheReadTokens != 9 {
		t.Errorf("relayLog.CacheReadTokens: want 9, got %d", rl.CacheReadTokens)
	}
}

// TestMediaHandler_jsonUsage_responseBodyUnchanged
// usage 采集是旁路 tee，客户端拿到的响应体必须逐字节不变（不能因为扫描而被缓冲改写）。
func TestMediaHandler_jsonUsage_responseBodyUnchanged(t *testing.T) {
	const requestModel = "usage-image-model-passthrough"
	upstreamBody := `{"created":1,"data":[{"b64_json":"aGk="}],"usage":{"input_tokens":8,"output_tokens":2}}`
	initMediaUsageEnv(t, requestModel, "", "application/json", upstreamBody)

	rec := runMediaImageRequest(t, requestModel, 66002, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	if rec.Body.String() != upstreamBody {
		t.Errorf("response body was altered by usage capture:\n want %q\n got  %q", upstreamBody, rec.Body.String())
	}
}

// TestMediaHandler_usageDrivesExprCost
// ★ 真实 usage 必须进入计费表达式的 token 维度。表达式按 p+c 计价：
// p=30（text leg）、c=40 → 70 * 0.5 = 35。
// 若 usage 没接上（TokenParams 全零），表达式算出 0 → 红。
// 这同时证明"有 usage 用 usage"这条优先级规则真的生效。
func TestMediaHandler_usageDrivesExprCost(t *testing.T) {
	const requestModel = "usage-priced-model"
	upstreamBody := `{"data":[{"b64_json":"aGk="}],` +
		`"usage":{"input_tokens":100,"output_tokens":40,` +
		`"input_tokens_details":{"text_tokens":30,"image_tokens":70}}}`
	env := initMediaUsageEnv(t, requestModel, `(p + c) * 0.5`, "application/json", upstreamBody)

	rec := runMediaImageRequest(t, requestModel, 66003, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if len(env.charges) != 1 {
		t.Fatalf("ChargeKey call count: want exactly 1, got %d (%v)", len(env.charges), env.charges)
	}
	// 35 = (30 text + 40 completion) * 0.5。0 意味着 TokenParams 是空的。
	if math.Abs(env.charges[0]-35.0) > 1e-9 {
		t.Errorf("charged cost: want 35.0 (real usage tokens fed into expr), got %.9f — "+
			"0 means usage never reached ComputeExprCostFullWithRequest", env.charges[0])
	}
	if rl := lastRelayLog(t); math.Abs(rl.Cost-35.0) > 1e-9 {
		t.Errorf("relayLog.Cost: want 35.0, got %.9f", rl.Cost)
	}
}

// TestMediaHandler_noUsage_keepsParamPricing
// ★ SPEC 硬规则的反方向守卫：上游不报 usage 时，param() 定价必须**原样保留**，
// token 必须留 0（不许伪造）。若谁把"总是用 TokenParams"写死，这条会红。
// 三档表达式：param('size') 命中 → 7，否则 1；同时若 usage 被误当成有值，
// p/c 为 0 也不会改变这个结果，所以额外断言 token 字段确实是 0。
func TestMediaHandler_noUsage_keepsParamPricing(t *testing.T) {
	const requestModel = "no-usage-image-model"
	// 上游完全不给 usage。
	upstreamBody := `{"created":1,"data":[{"b64_json":"aGk="}]}`
	expr := `param('size') == '1024x1024' ? 7.0 : (param('size') == '512x512' ? 3.0 : 1.0)`
	env := initMediaUsageEnv(t, requestModel, expr, "application/json", upstreamBody)

	rec := runMediaImageRequest(t, requestModel, 66004, `,"size":"1024x1024"`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if len(env.charges) != 1 {
		t.Fatalf("ChargeKey call count: want exactly 1, got %d (%v)", len(env.charges), env.charges)
	}
	if math.Abs(env.charges[0]-7.0) > 1e-9 {
		t.Errorf("charged cost: want 7.0 (param pricing preserved when upstream reports no usage), got %.9f", env.charges[0])
	}
	rl := lastRelayLog(t)
	if rl.InputTokens != 0 || rl.OutputTokens != 0 {
		t.Errorf("tokens must stay 0 when upstream reported no usage (never fabricate): got in=%d out=%d",
			rl.InputTokens, rl.OutputTokens)
	}
}

// TestMediaHandler_usageReachesAPIKeyStats
// ★ 这条是 M7 变异存活后补的：把 stats 的 InputToken/OutputToken 改回硬 0 时，
// 上面那些只看 relay_logs 的断言**全部照绿**——而 stats 这条腿才是
// middleware/auth.go 里 per-key MaxTokens 上限读的累加器（P1 #11 的主要收益之一：
// 纯媒体 key 的 MaxTokens 原本永久失效 = 无限免费）。
// 观测点是 stats.APIKeyGet 的真实增量（副作用），不是日志字段。
func TestMediaHandler_usageReachesAPIKeyStats(t *testing.T) {
	const requestModel = "usage-stats-model"
	const keyID = 66008
	upstreamBody := `{"data":[{"b64_json":"aGk="}],"usage":{"input_tokens":100,"output_tokens":40}}`
	initMediaUsageEnv(t, requestModel, "", "application/json", upstreamBody)

	before := stats.APIKeyGet(keyID).StatsMetrics

	rec := runMediaImageRequest(t, requestModel, keyID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}

	after := stats.APIKeyGet(keyID).StatsMetrics
	if gotIn := after.InputToken - before.InputToken; gotIn != 100 {
		t.Errorf("APIKey stats InputToken delta: want 100, got %d — "+
			"0 means media tokens never reach the accumulator behind the per-key MaxTokens ceiling", gotIn)
	}
	if gotOut := after.OutputToken - before.OutputToken; gotOut != 40 {
		t.Errorf("APIKey stats OutputToken delta: want 40, got %d", gotOut)
	}
}

// TestMediaHandler_mimoTTSUsage_captured
// ★ M9 变异（删掉 handleMimoTTSResponse 里的 usage.Scan）存活后补的。
// MiMo TTS 是唯一"给客户端回二进制、但上游是 JSON chat 响应"的端点，所以它
// **有** usage 可采（普通 TTS 的 handleBinaryResponse 没有，那是刻意不扫的）。
// media_usage_failover_test.go 虽然也走 MiMo 路径，但它断言的是 0/0，
// 删掉 Scan 照样成立 —— 必须有一条正向断言真实数字的测试。
func TestMediaHandler_mimoTTSUsage_captured(t *testing.T) {
	const requestModel = "usage-mimo-tts"
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

	// 上游是 chat/completions 形状：带 audio 数据 + usage。
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"audio":{"data":"aGk="}}}],` +
			`"usage":{"prompt_tokens":31,"completion_tokens":12}}`))
	}))
	t.Cleanup(upstream.Close)

	const channelID = 4401
	chpkg.GetCache().Clear()
	chpkg.GetCache().Set(channelID, dbmodel.Channel{
		ID: channelID, Name: "mimo-upstream", Enabled: true,
		BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}},
		Keys:     []dbmodel.ChannelKey{{ID: 95, ChannelID: channelID, Enabled: true, ChannelKey: "sk-mimo"}},
	})
	grppkg.GetCache().Clear()
	grppkg.GetCache().Set(7401, dbmodel.Group{
		ID: 7401, Name: requestModel, EndpointType: dbmodel.EndpointTypeAudioSpeech,
		Mode: dbmodel.GroupModeRoundRobin, EndpointProvider: "mimo",
		Items: []dbmodel.GroupItem{{ID: 1, GroupID: 7401, ChannelID: channelID, ModelName: "up-mimo", Priority: 1, Weight: 1}},
	})
	grppkg.RebuildIndexes()
	t.Cleanup(func() {
		chpkg.GetCache().Clear()
		grppkg.GetCache().Clear()
		grppkg.RebuildIndexes()
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/audio/speech",
		strings.NewReader(fmt.Sprintf(`{"model":%q,"input":"hello","voice":"alloy"}`, requestModel)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key_id", 66009)

	MediaHandler(MediaEndpointAudioSpeech, c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	// 客户端拿到的是解码后的二进制音频（"aGk=" → "hi"），不是 JSON。
	if rec.Body.String() != "hi" {
		t.Fatalf("client should receive decoded audio, got %q", rec.Body.String())
	}

	rl := lastRelayLog(t)
	if rl.InputTokens != 31 {
		t.Errorf("relayLog.InputTokens: want 31, got %d — 0 means the usage.Scan in handleMimoTTSResponse is gone", rl.InputTokens)
	}
	if rl.OutputTokens != 12 {
		t.Errorf("relayLog.OutputTokens: want 12, got %d", rl.OutputTokens)
	}
}

// TestMediaHandler_sseUsage_takesFinalFrame
// SSE 链的调用点守卫：逐行 tee 必须抓到**最后一帧**的结算 usage（25 而非 1）。
func TestMediaHandler_sseUsage_takesFinalFrame(t *testing.T) {
	const requestModel = "usage-sse-model"
	upstreamBody := "data: {\"type\":\"image_generation.partial_image\",\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}\n\n" +
		"data: {\"type\":\"image_generation.completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":25}}\n\n" +
		"data: [DONE]\n\n"
	initMediaUsageEnv(t, requestModel, "", "text/event-stream", upstreamBody)

	rec := runMediaImageRequest(t, requestModel, 66005, `,"stream":true`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != upstreamBody {
		t.Errorf("SSE body was altered by usage capture:\n want %q\n got  %q", upstreamBody, rec.Body.String())
	}

	rl := lastRelayLog(t)
	if rl.OutputTokens != 25 {
		t.Errorf("relayLog.OutputTokens: want 25 (final SSE frame), got %d — "+
			"1 means an earlier partial frame won; 0 means the handleSSEResponse tee is gone", rl.OutputTokens)
	}
	if rl.InputTokens != 10 {
		t.Errorf("relayLog.InputTokens: want 10, got %d", rl.InputTokens)
	}
}

// TestMediaHandler_imageBedUsage_billedAndNotDroppedFromResponse
// 图床链的守卫（图床开启时响应体被 ReadAll + 重新 marshal，是最容易丢 usage 的路径）：
//  1. usage 必须仍进计费/日志；
//  2. 重写后的响应体**不能把 usage 字段丢掉**（客户端要能读到 token 数）。
//
// 图床上传会失败（配置指向一个不存在的 endpoint），rewriteImageGenResponse 返回
// false → 走 writeOriginalResponse 原样回写，此时 usage 天然还在，但计费仍必须
// 拿到它 —— 这一条锁的是 tryImageBedRewrite 里的 usage.Scan 调用点。
func TestMediaHandler_imageBedUsage_billedAndNotDroppedFromResponse(t *testing.T) {
	const requestModel = "usage-imagebed-model"
	upstreamBody := `{"created":1,"data":[{"b64_json":"aGk="}],"usage":{"input_tokens":50,"output_tokens":6}}`
	env := initMediaUsageEnv(t, requestModel, `(p + c) * 1.0`, "application/json", upstreamBody)

	// 开启图床，指向一个必然失败的上传端点。tryImageBedRewrite 因此走
	// "读了 body 但没能重写" 的分支 —— 这正是 usage.Scan 必须已经跑过的位置。
	if err := setting.SetString(dbmodel.SettingKeyImageBedEnabled, "true"); err != nil {
		t.Fatalf("enable image bed: %v", err)
	}
	if err := setting.SetString(dbmodel.SettingKeyImageBedEndpoint, "http://127.0.0.1:1/upload"); err != nil {
		t.Fatalf("set image bed endpoint: %v", err)
	}

	rec := runMediaImageRequest(t, requestModel, 66006, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}

	rl := lastRelayLog(t)
	if rl.InputTokens != 50 || rl.OutputTokens != 6 {
		t.Errorf("image bed path lost usage: want in=50 out=6, got in=%d out=%d — "+
			"the usage.Scan call in tryImageBedRewrite is missing", rl.InputTokens, rl.OutputTokens)
	}
	// 50 text（无 details 明细，全部算 P）+ 6 = 56。
	if len(env.charges) != 1 || math.Abs(env.charges[0]-56.0) > 1e-9 {
		t.Errorf("charged cost: want 56.0, got %v", env.charges)
	}

	// 客户端仍能看到 usage 字段。
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response body is not valid JSON: %v (body=%s)", err, rec.Body.String())
	}
	if _, ok := got["usage"]; !ok {
		t.Errorf("usage field was dropped from the client response by the image bed rewrite (body=%s)", rec.Body.String())
	}
}

// TestMediaHandler_failedRequest_recordsNoTokens
// 失败请求（上游 500）不能记 token、不能扣钱。锁住 OnExhausted 传零值 usage 这条决定：
// 若谁把 billedUsage 直接传进 OnExhausted，失败请求会带上最后一跳的 usage。
func TestMediaHandler_failedRequest_recordsNoTokens(t *testing.T) {
	const requestModel = "usage-failing-model"
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

	// 上游 500 且响应体里带 usage —— 即便如此也不许计入。
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom","usage":{"input_tokens":999,"output_tokens":888}}`))
	}))
	t.Cleanup(upstream.Close)

	const channelID = 4302
	chpkg.GetCache().Clear()
	chpkg.GetCache().Set(channelID, dbmodel.Channel{
		ID: channelID, Name: "failing-upstream", Enabled: true,
		BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}},
		Keys:     []dbmodel.ChannelKey{{ID: 94, ChannelID: channelID, Enabled: true, ChannelKey: "sk-fail"}},
	})
	grppkg.GetCache().Clear()
	grppkg.GetCache().Set(7302, dbmodel.Group{
		ID: 7302, Name: requestModel, EndpointType: dbmodel.EndpointTypeImageGeneration,
		Mode:  dbmodel.GroupModeRoundRobin,
		Items: []dbmodel.GroupItem{{ID: 1, GroupID: 7302, ChannelID: channelID, ModelName: "upstream-image-model", Priority: 1, Weight: 1}},
	})
	grppkg.RebuildIndexes()
	t.Cleanup(func() {
		chpkg.GetCache().Clear()
		grppkg.GetCache().Clear()
		grppkg.RebuildIndexes()
	})

	rec := runMediaImageRequest(t, requestModel, 66007, "")
	if rec.Code == http.StatusOK {
		t.Fatalf("upstream returned 500, handler should not report 200 (body=%s)", rec.Body.String())
	}

	rl := lastRelayLog(t)
	if rl.InputTokens != 0 || rl.OutputTokens != 0 {
		t.Errorf("failed request must record 0 tokens, got in=%d out=%d — "+
			"a non-2xx upstream body's usage was billed", rl.InputTokens, rl.OutputTokens)
	}
}
