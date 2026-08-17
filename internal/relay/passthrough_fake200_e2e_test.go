package relay

/*
passthrough 出站格式的端到端 fake200 / 计费 / 重试循环守卫。

passthrough 透传客户端原始请求体（只重写顶层 model），响应仍走标准 relay 链路
（outbound.TransformResponse → InternalLLMResponse → fake200 校验 →
inbound.TransformResponse）。本文件守：
  - passthrough 路径的 200+空响应体必须被 isFake200Response 拦为失败、不扣费；
  - passthrough 路径能走完重试循环（retry_shared.go 按 transformer 接口工作，
    passthrough 实现同一接口就应天然兼容）。
*/

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	ch "github.com/gypg/lodestar/internal/op/channel"
	grp "github.com/gypg/lodestar/internal/op/group"
	st "github.com/gypg/lodestar/internal/op/stats"
	"github.com/gypg/lodestar/internal/relay/balancer"
	"github.com/gypg/lodestar/internal/transformer/inbound"
	"github.com/gypg/lodestar/internal/transformer/outbound"
)

// initPassthroughFake200Env 搭建 passthrough 出站格式的端到端环境。
// 渠道类型设为 OpenAIChat（Type=0），但分组 OutboundFormat="passthrough" 使其
// 短路到 OutboundTypePassthrough——验证 passthrough 与任意渠道类型兼容。
// 上游返回 upstreamStatus + upstreamBody。返回 upstream 命中计数指针。
func initPassthroughFake200Env(t *testing.T, requestModel, resolvedModel string, channelID, keyID, groupID int, upstreamStatus int, upstreamBody string) (uint, int, *int) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	uid, kid := initFake200CommercialDB(t, 10.0, requestModel, "5")

	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		// 断言上游收到的请求体：model 被重写为上游模型名，其余原样。
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		wantModel := fmt.Sprintf(`"model":%q`, resolvedModel)
		if !strings.Contains(string(body), wantModel) {
			t.Errorf("upstream body missing rewritten model: %s (want %s)", string(body), wantModel)
		}
		if !strings.Contains(string(body), `"messages"`) {
			t.Errorf("upstream body lost messages field: %s", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upstreamStatus)
		_, _ = w.Write([]byte(upstreamBody))
	}))
	t.Cleanup(upstream.Close)

	ch.GetCache().Clear()
	ch.GetCache().Set(channelID, dbmodel.Channel{
		ID:       channelID,
		Name:     "passthrough-upstream",
		Type:     0, // OutboundTypeOpenAIChat；分组 OutboundFormat=passthrough 会短路
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}},
		Keys: []dbmodel.ChannelKey{
			{ID: keyID, ChannelID: channelID, Enabled: true, ChannelKey: "sk-passthrough-upstream"},
		},
	})

	grp.GetCache().Clear()
	grp.GetCache().Set(groupID, dbmodel.Group{
		ID:             groupID,
		Name:           requestModel,
		EndpointType:   dbmodel.EndpointTypeChat,
		OutboundFormat: "passthrough",
		Mode:           dbmodel.GroupModeFailover,
		Items: []dbmodel.GroupItem{
			{ID: 1, GroupID: groupID, ChannelID: channelID, ModelName: resolvedModel, Priority: 1, Weight: 1},
		},
	})
	grp.RebuildIndexes()

	clearRuntime := func() {
		balancer.RemoveChannelEntries(channelID)
		balancer.RemoveChannelStats(channelID)
		balancer.RemoveAPIKeySticky(kid)
	}
	clearRuntime()
	t.Cleanup(func() {
		clearRuntime()
		ch.GetCache().Clear()
		grp.GetCache().Clear()
		grp.RebuildIndexes()
	})

	return uid, kid, &hits
}

// runPassthroughHandler 触发一次 chat 请求，走 passthrough 出站路径。
func runPassthroughHandler(t *testing.T, kid int, requestModel string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}]}`, requestModel)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("api_key_id", kid)
	Handler(dbmodel.EndpointTypeChat, inbound.InboundTypeOpenAIChat, c)
	return rec
}

// TestPassthroughFake200_NotChargedAndNotDelivered
// ★ passthrough 路径的 200+空响应体必须——不扣费、不以 200 交付、计入全局失败。
// 变异「让 passthrough TransformResponse 对空体返回 error」→ handleResponse 因
// err 直接返回，跳过 isFake200Response，fake200 防御被绕过 → 余额被扣 → 红。
func TestPassthroughFake200_NotChargedAndNotDelivered(t *testing.T) {
	const requestModel = "pt-e2e-model"
	const resolvedModel = "pt-e2e-upstream"
	uid, kid, hits := initPassthroughFake200Env(t, requestModel, resolvedModel, 4421, 8821, 7421, http.StatusOK, "")
	seedRetryEmptyOutputSetting(t, "false")
	beforeStats := st.TotalGet()

	rec := runPassthroughHandler(t, kid, requestModel)

	if *hits == 0 {
		t.Fatal("upstream never received the request — passthrough did not forward")
	}
	// ★ 不扣费：余额必须一分不动。
	if rem := fake200Quota(t, uid); math.Abs(rem-10.0) > 1e-9 {
		t.Fatalf("balance after passthrough fake-200 = %.9f, want 10.0 — fake 200 on passthrough path must not be charged", rem)
	}
	// ★ 不以 200 交付：假 200 被拦为渠道失败，单渠道耗尽后是 502。
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("client status = %d, want 502 — passthrough fake 200 must not be delivered as success (body=%s)", rec.Code, rec.Body.String())
	}
	// ★ 全局统计记失败。
	afterStats := st.TotalGet()
	if d := afterStats.RequestSuccess - beforeStats.RequestSuccess; d != 0 {
		t.Fatalf("RequestSuccess delta = %d, want 0 — passthrough fake 200 must not count as success", d)
	}
	if d := afterStats.RequestFailed - beforeStats.RequestFailed; d < 1 {
		t.Fatalf("RequestFailed delta = %d, want >= 1 — passthrough fake 200 must show up as failure", d)
	}
}

// TestPassthroughLegalResponse_DeliveredAndCharged
// ★ 防过度防御：passthrough 路径的上游合法响应必须正常交付并扣费。
// 变异「passthrough 把合法响应也判成 fake200」→ 余额不变 + 502 → 红。
func TestPassthroughLegalResponse_DeliveredAndCharged(t *testing.T) {
	const requestModel = "pt-legal-model"
	const resolvedModel = "pt-legal-upstream"
	legalBody := `{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`
	uid, kid, hits := initPassthroughFake200Env(t, requestModel, resolvedModel, 4422, 8822, 7422, http.StatusOK, legalBody)
	seedRetryEmptyOutputSetting(t, "false")

	rec := runPassthroughHandler(t, kid, requestModel)

	if *hits == 0 {
		t.Fatal("upstream never received the request — passthrough did not forward")
	}
	// ★ 合法响应以 200 交付。
	if rec.Code != http.StatusOK {
		t.Fatalf("client status = %d, want 200 for legal passthrough response (body=%s)", rec.Code, rec.Body.String())
	}
	// ★ 合法响应扣费（表达式固定费 5，余额 10 → 5）。
	if rem := fake200Quota(t, uid); math.Abs(rem-5.0) > 1e-9 {
		t.Fatalf("balance after legal passthrough response = %.9f, want 5.0 — legal response must be charged", rem)
	}
}

// TestPassthrough_RetryLoopCompletes
// ★ e2e 重试兼容：passthrough 实现 model.Outbound 接口，retry_shared.go 按
// transformer 接口工作，passthrough 路径的请求能走完重试循环。
// 单渠道返回 429，failover 模式下耗尽渠道后回 502。
// 变异「passthrough 让 retry 误判为成功」→ 不会 502 → 红。
func TestPassthrough_RetryLoopCompletes(t *testing.T) {
	const requestModel = "pt-retry-model"
	const resolvedModel = "pt-retry-upstream"
	errBody := `{"error":{"message":"rate limited","type":"rate_limit_error"}}`
	_, kid, hits := initPassthroughFake200Env(t, requestModel, resolvedModel, 4423, 8823, 7423, http.StatusTooManyRequests, errBody)
	seedRetryEmptyOutputSetting(t, "false")

	rec := runPassthroughHandler(t, kid, requestModel)

	if *hits == 0 {
		t.Fatal("upstream never received the request — passthrough did not forward into retry loop")
	}
	// passthrough 路径的 429 走标准重试循环，单渠道耗尽后回 502。
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("client status = %d, want 502 — passthrough 429 must drain the channel and return 502 (body=%s)", rec.Code, rec.Body.String())
	}
}

// 确保未直接使用的 import 不会触发编译失败。
var _ = op.InitCache
var _ = context.Background
var _ = outbound.OutboundTypePassthrough
