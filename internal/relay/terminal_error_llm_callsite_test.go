package relay

/*
LLM relay 上 ScopeNone 终态回写的调用点守卫 —— 堵住 relay.go OnFinalFailure 的盲点。

为什么 media 那个守卫不够（2026-08-08 变异实测）：把 relay.go 里的
writeClientTerminalError 换回 resp.BadGateway(req.c)，internal/relay 整包全绿 ——
terminal_error_test.go 守的是函数本身，terminal_error_callsite_test.go 只驱动
MediaHandler，两者都碰不到 LLM 这条路。而 LLM 路径才是 R-3 的主场景：
Claude Code / oh-my-pi 这类客户端要靠 400 + context_length_exceeded 决定压缩上下文。

本文件驱动的是 Handler(EndpointTypeChat, ...)，入口在回写点的上游。
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
	"github.com/gypg/lodestar/internal/transformer/inbound"
)

// initLLMScopeNoneEnv 搭起驱动 Handler 所需的最小环境：内存 DB + 缓存 +
// 一个指向假上游的 chat 渠道。假上游按 upstreamStatus/upstreamBody 应答。
// 返回上游命中计数指针。
func initLLMScopeNoneEnv(t *testing.T, requestModel string, upstreamStatus int, upstreamBody string) *int {
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

	const channelID = 4301
	chpkg.GetCache().Clear()
	chpkg.GetCache().Set(channelID, dbmodel.Channel{
		ID:       channelID,
		Name:     "llm-scopenone-upstream",
		Type:     0, // OutboundTypeOpenAIChat
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}},
		Keys: []dbmodel.ChannelKey{
			{ID: 93, ChannelID: channelID, Enabled: true, ChannelKey: "sk-llm-scopenone"},
		},
	})

	grppkg.GetCache().Clear()
	grppkg.GetCache().Set(7301, dbmodel.Group{
		ID:           7301,
		Name:         requestModel,
		EndpointType: dbmodel.EndpointTypeChat,
		Mode:         dbmodel.GroupModeRoundRobin,
		Items: []dbmodel.GroupItem{
			{ID: 1, GroupID: 7301, ChannelID: channelID, ModelName: "upstream-chat-model", Priority: 1, Weight: 1},
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

func newChatCompletionRequest(t *testing.T, model string) *http.Request {
	t.Helper()
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}]}`, model)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// TestHandler_scopeNone400_passesUpstreamBodyToClient
// ★ LLM 路径的调用点守卫：入口是 Handler，不是 writeClientTerminalError。
//
// 把 relay.go OnFinalFailure 里的 writeClientTerminalError 换回
// resp.BadGateway(req.c)：状态码从 400 变 502 → 红。
// 实测（2026-08-08）：没有本文件时，那个变异在整包 go test 下存活。
func TestHandler_scopeNone400_passesUpstreamBodyToClient(t *testing.T) {
	const requestModel = "llm-scopenone-model"
	hits := initLLMScopeNoneEnv(t, requestModel, http.StatusBadRequest, upstream400Body)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = newChatCompletionRequest(t, requestModel)
	c.Set("api_key_id", 55201)

	Handler(dbmodel.EndpointTypeChat, inbound.InboundTypeOpenAIChat, c)

	if *hits == 0 {
		t.Fatal("upstream never received the request — nothing was forwarded")
	}
	// ★ 核心断言 1：状态码原样透传。
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("client status = %d, want 400 (upstream status passed through); "+
			"502 means the LLM-path ScopeNone was swallowed into a gateway error again "+
			"(R-3 regression). body=%s", rec.Code, rec.Body.String())
	}
	// ★ 核心断言 2：错误码活着到下游。
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
		t.Errorf("error.code = %q, want context_length_exceeded — the signal the client "+
			"needs to compress context is gone; body=%s", payload.Error.Code, rec.Body.String())
	}
	if !strings.Contains(payload.Error.Message, "maximum context length") {
		t.Errorf("error.message lost upstream wording: %q", payload.Error.Message)
	}
}

// TestHandler_scopeNone400_doesNotRetryUpstream
// ScopeNone 是终态。若 shouldTryAdapterFallback 的 ScopeNone 排除被撤掉，
// OpenAI chat 渠道会拿 chat→responses 两个 adapter 各打一次同样的 400。
// 断言"恰好 1 次"，这才锁得住 R-3 的另一半。
func TestHandler_scopeNone400_doesNotRetryUpstream(t *testing.T) {
	const requestModel = "llm-scopenone-model-2"
	hits := initLLMScopeNoneEnv(t, requestModel, http.StatusBadRequest, upstream400Body)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = newChatCompletionRequest(t, requestModel)
	c.Set("api_key_id", 55202)

	Handler(dbmodel.EndpointTypeChat, inbound.InboundTypeOpenAIChat, c)

	if *hits != 1 {
		t.Errorf("upstream hit count = %d, want exactly 1 — a 400 client error must not be "+
			"re-attempted with another outbound adapter", *hits)
	}
}

// TestHandler_upstream500_stillFailsClosed
// 反方向对照：500 可重试，耗尽后仍是网关风格失败，不能被误伤成"原样透传"。
func TestHandler_upstream500_stillFailsClosed(t *testing.T) {
	const requestModel = "llm-scopenone-model-3"
	hits := initLLMScopeNoneEnv(t, requestModel, http.StatusInternalServerError, `{"error":{"message":"boom"}}`)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = newChatCompletionRequest(t, requestModel)
	c.Set("api_key_id", 55203)

	Handler(dbmodel.EndpointTypeChat, inbound.InboundTypeOpenAIChat, c)

	if *hits == 0 {
		t.Fatal("upstream never received the request")
	}
	if rec.Code == http.StatusInternalServerError {
		t.Errorf("client status = 500 — a retryable upstream failure must not be passed "+
			"through as a terminal client error; body=%s", rec.Body.String())
	}
	if rec.Code < 400 {
		t.Errorf("client status = %d, want a 4xx/5xx failure", rec.Code)
	}
}
