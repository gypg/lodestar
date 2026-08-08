package relay

/*
ScopeNone 终态回写的调用点守卫 —— 堵住 media_relay.go OnFinalFailure 的测试盲点。

为什么 terminal_error_test.go 不够：那些测试**直接调用** writeClientTerminalError，
守的是"这个函数写得对不对"，不是"MediaHandler 还调不调它"。把 OnFinalFailure 里那行
换回 resp.Error(c, http.StatusBadGateway, ...)，terminal_error_test.go 全绿 —— 这正是
上次会话（2026-08-07）在 media_relay.go:81 上栽的同一个坑。

本文件的做法：真的跑 MediaHandler，入口在回写点的上游。
  1. 起 httptest 假上游，回 400 + 真实的 context_length_exceeded JSON；
  2. 走 group/channel 缓存注册这个渠道（无需真 DB 表）；
  3. 发一个正常的 image generation 请求；
  4. 断言下游看到的是 400 + 上游原样 body，而不是 502。

接线在   → 400 + {"error":{"code":"context_length_exceeded"}}
接线换回 → 502 + "upstream service unavailable" → 断言红。
*/

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	chpkg "github.com/gypg/lodestar/internal/op/channel"
	grppkg "github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/op/setting"
)

// upstream400Body 是真实上游在超上下文时回的形状。测试全程按字节比对它，
// 因为下游客户端就是靠 code 字段决定该压缩上下文还是换模型。
const upstream400Body = `{"error":{"message":"This model's maximum context length is 8192 tokens.","type":"invalid_request_error","param":"messages","code":"context_length_exceeded"}}`

// initScopeNoneCallsiteEnv 搭起驱动 MediaHandler 所需的最小环境，
// 假上游按 upstreamStatus/upstreamBody 应答。
func initScopeNoneCallsiteEnv(t *testing.T, requestModel string, upstreamStatus int, upstreamBody string) *int {
	t.Helper()
	gin.SetMode(gin.TestMode)

	hits := 0

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

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upstreamStatus)
		_, _ = w.Write([]byte(upstreamBody))
	}))
	t.Cleanup(upstream.Close)

	const channelID = 4201
	chpkg.GetCache().Clear()
	chpkg.GetCache().Set(channelID, dbmodel.Channel{
		ID:       channelID,
		Name:     "scopenone-upstream",
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}},
		Keys: []dbmodel.ChannelKey{
			{ID: 92, ChannelID: channelID, Enabled: true, ChannelKey: "sk-scopenone"},
		},
	})

	grppkg.GetCache().Clear()
	grppkg.GetCache().Set(7201, dbmodel.Group{
		ID:           7201,
		Name:         requestModel,
		EndpointType: dbmodel.EndpointTypeImageGeneration,
		Mode:         dbmodel.GroupModeRoundRobin,
		Items: []dbmodel.GroupItem{
			{ID: 1, GroupID: 7201, ChannelID: channelID, ModelName: "upstream-image-model", Priority: 1, Weight: 1},
		},
	})
	grppkg.RebuildIndexes()

	t.Cleanup(func() {
		chpkg.GetCache().Clear()
		grppkg.GetCache().Clear()
		grppkg.RebuildIndexes()
		_ = db.Close()
	})

	return &hits
}

func newImageGenerationRequest(t *testing.T, model string) *http.Request {
	t.Helper()
	body := fmt.Sprintf(`{"model":%q,"prompt":"a cat","size":"1024x1024"}`, model)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// TestMediaHandler_scopeNone400_passesUpstreamBodyToClient
// ★ 真正的调用点守卫：入口是 MediaHandler，不是 writeClientTerminalError。
//
// 把 OnFinalFailure 里的 writeClientTerminalError 换回 resp.Error(c, 502, ...)：
// 状态码断言从 400 变 502 → 红；body 断言也拿不到 context_length_exceeded → 红。
func TestMediaHandler_scopeNone400_passesUpstreamBodyToClient(t *testing.T) {
	const requestModel = "scopenone-image-model"
	hits := initScopeNoneCallsiteEnv(t, requestModel, http.StatusBadRequest, upstream400Body)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = newImageGenerationRequest(t, requestModel)
	c.Set("api_key_id", 55101)

	MediaHandler(MediaEndpointImageGeneration, c)

	// 前置校验：请求真的打到了上游（否则下面全是空转）。
	if *hits == 0 {
		t.Fatal("upstream never received the request — nothing was forwarded")
	}
	// ★ 核心断言 1：状态码原样透传。502 说明 ScopeNone 又被吞成网关错误了（R-3 回归）。
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("client status = %d, want 400 (upstream status passed through); "+
			"502 means ScopeNone was swallowed into a gateway error again (R-3 regression). body=%s",
			rec.Code, rec.Body.String())
	}
	// ★ 核心断言 2：错误码必须活着到下游 —— 客户端要靠它决定压缩上下文还是换模型。
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("client body is not JSON: %v; body=%s", err, rec.Body.String())
	}
	if payload.Error.Code != "context_length_exceeded" {
		t.Errorf("error.code = %q, want context_length_exceeded — the signal the client needs "+
			"to compress context is gone; body=%s", payload.Error.Code, rec.Body.String())
	}
	if !strings.Contains(payload.Error.Message, "maximum context length") {
		t.Errorf("error.message lost upstream wording: %q", payload.Error.Message)
	}
}

// TestMediaHandler_scopeNone400_doesNotRetryUpstream
// ScopeNone 是终态：不该重试。若 shouldTryAdapterFallback 的 ScopeNone 排除被撤掉，
// 或 OnFinalFailure 不再 return true，上游会挨第二次同样的 400 —— 白烧重试预算。
// 断言"恰好 1 次"而非"至少 1 次"，这才锁得住。
func TestMediaHandler_scopeNone400_doesNotRetryUpstream(t *testing.T) {
	const requestModel = "scopenone-image-model-2"
	hits := initScopeNoneCallsiteEnv(t, requestModel, http.StatusBadRequest, upstream400Body)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = newImageGenerationRequest(t, requestModel)
	c.Set("api_key_id", 55102)

	MediaHandler(MediaEndpointImageGeneration, c)

	if *hits != 1 {
		t.Errorf("upstream hit count = %d, want exactly 1 — a 400 client error must not be retried", *hits)
	}
}

// TestMediaHandler_upstream500_stillFailsClosed
// 反方向对照：500 是 ScopeNextChannel（可重试），耗尽后仍应是网关风格的失败，
// 不能被这次改动误伤成"把 500 原样当客户端错误回"。
// 没有这个对照，上面两个测试也能被"所有错误都原样透传"这种过头实现骗过。
func TestMediaHandler_upstream500_stillFailsClosed(t *testing.T) {
	const requestModel = "scopenone-image-model-3"
	hits := initScopeNoneCallsiteEnv(t, requestModel, http.StatusInternalServerError, `{"error":{"message":"boom"}}`)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = newImageGenerationRequest(t, requestModel)
	c.Set("api_key_id", 55103)

	MediaHandler(MediaEndpointImageGeneration, c)

	if *hits == 0 {
		t.Fatal("upstream never received the request")
	}
	if rec.Code == http.StatusInternalServerError {
		t.Errorf("client status = 500 — a retryable upstream failure must not be passed through "+
			"as if it were a terminal client error; body=%s", rec.Body.String())
	}
	if rec.Code < 400 {
		t.Errorf("client status = %d, want a 4xx/5xx failure", rec.Code)
	}
}
