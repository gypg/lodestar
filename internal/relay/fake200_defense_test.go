package relay

/*
LLM 假 200 计费暴露面的纵深防御守卫。

背景（ad71355/dd8f26d 之后仍存在的单点问题）：isEmptyOutputResponse 的假 200
判定此前只在 handleResponse 一处被调用，且受 isRetryEmptyOutputEnabled() 门控——
retry_empty_output=false 时，"该接口未接入"这类 200 错误体会以"成功"身份走完
attempt() 成功分支（RecordSuccess 重置熔断器失败计数、RecordAutoSuccess/SetSticky
把坏渠道排到首选并粘住、RequestSuccess 压住错误率告警），并在 metrics.Save 里
经 ChargeKeyWithExpr 扣费（表达式计费时连固定费一起扣）。

本文件守两层职责分离后的不变式：
  - L1（relay 层，handleResponse）：假 200 判定不受 retry_empty_output 门控，
    返回 errFake200Response，经 ClassifyRelayError 归为 ScopeNextChannel 失败，
    从而走 OnFailure → RecordFailure/RecordAutoFailure（计入熔断器与 auto 失败），
    且不再触发 RecordSuccess/RecordAutoSuccess/SetSticky；
  - L2（计费层，metrics.Save）：独立校验 InternalResponse，假 200 一律不扣费；
    以"成功"身份到达计费层时降级为失败入账（RequestFailed，不压错误率告警）。

反向守卫（防过度防御）：合法 embedding（EmbeddingData 非空）、合法空 completion
（有 tool_calls / 多模态内容）、失败但已记录真实用量的请求，均不受新守卫影响。
*/

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/apikey"
	ch "github.com/gypg/lodestar/internal/op/channel"
	grp "github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/op/setting"
	st "github.com/gypg/lodestar/internal/op/stats"
	"github.com/gypg/lodestar/internal/op/user"
	"github.com/gypg/lodestar/internal/relay/balancer"
	"github.com/gypg/lodestar/internal/transformer/inbound"
	transmodel "github.com/gypg/lodestar/internal/transformer/model"
)

// ---------------------------------------------------------------------------
// L1 单元级：handleResponse 的假 200 判定与豁免
// ---------------------------------------------------------------------------

// stubFake200Outbound 把预构造的 InternalLLMResponse 直接交给 handleResponse，
// 让测试只聚焦"响应形状 → 判定结果"这一个变量。
type stubFake200Outbound struct {
	resp *transmodel.InternalLLMResponse
}

func (o *stubFake200Outbound) TransformRequest(context.Context, *transmodel.InternalLLMRequest, string, string) (*http.Request, error) {
	return nil, errors.New("not used in this test")
}

func (o *stubFake200Outbound) TransformResponse(context.Context, *http.Response) (*transmodel.InternalLLMResponse, error) {
	return o.resp, nil
}

func (o *stubFake200Outbound) TransformStream(context.Context, []byte) (*transmodel.InternalLLMResponse, error) {
	return nil, errors.New("not used in this test")
}

type stubFake200Inbound struct{}

func (stubFake200Inbound) TransformRequest(context.Context, []byte) (*transmodel.InternalLLMRequest, error) {
	return nil, errors.New("not used in this test")
}

func (stubFake200Inbound) TransformResponse(_ context.Context, _ *transmodel.InternalLLMResponse) ([]byte, error) {
	return []byte(`{"ok":true}`), nil
}

func (stubFake200Inbound) TransformStream(context.Context, *transmodel.InternalLLMResponse) ([]byte, error) {
	return nil, nil
}

func (stubFake200Inbound) GetInternalResponse(context.Context) (*transmodel.InternalLLMResponse, error) {
	return nil, errors.New("not used in this test")
}

func newFake200HandleAttempt(t *testing.T, resp *transmodel.InternalLLMResponse) *relayAttempt {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return &relayAttempt{
		relayRequest: &relayRequest{
			c:               c,
			clientCtx:       context.Background(),
			operationCtx:    context.Background(),
			inAdapter:       stubFake200Inbound{},
			internalRequest: &transmodel.InternalLLMRequest{Model: "fake200-unit-model"},
		},
		outAdapter: &stubFake200Outbound{resp: resp},
		channel:    &dbmodel.Channel{Name: "fake200-unit-channel"},
	}
}

func runFake200HandleResponse(t *testing.T, ra *relayAttempt) error {
	t.Helper()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"该接口未接入"}}`)),
	}
	return ra.handleResponse(context.Background(), resp)
}

// TestHandleResponseFake200FailsEvenWhenRetryDisabled
// ★ L1 核心守卫：retry_empty_output=false 时假 200 仍然必须被拦为失败。
// 变异"把假 200 判定挪回 isRetryEmptyOutputEnabled() 门控之内"→ 本测试红。
func TestHandleResponseFake200FailsEvenWhenRetryDisabled(t *testing.T) {
	seedRetryEmptyOutputSetting(t, "false")

	fake200 := &transmodel.InternalLLMResponse{
		ID:            "fake-200",
		EmbeddingData: []transmodel.EmbeddingObject{},
		Choices:       []transmodel.Choice{},
	}
	ra := newFake200HandleAttempt(t, fake200)

	err := runFake200HandleResponse(t, ra)
	if !errors.Is(err, errFake200Response) {
		t.Fatalf("handleResponse() err = %v, want errFake200Response when retry_empty_output=false", err)
	}
	if errors.Is(err, errEmptyOutput) {
		t.Fatalf("handleResponse() err = %v; fake 200 must not be conflated with the retry-gated errEmptyOutput", err)
	}
}

// retry_empty_output=true（默认）时假 200 同样按失败处理，只是不再依赖该路径兜底。
func TestHandleResponseFake200FailsAlsoWhenRetryEnabled(t *testing.T) {
	seedRetryEmptyOutputSetting(t, "true")

	fake200 := &transmodel.InternalLLMResponse{ID: "fake-200"}
	ra := newFake200HandleAttempt(t, fake200)

	if err := runFake200HandleResponse(t, ra); !errors.Is(err, errFake200Response) {
		t.Fatalf("handleResponse() err = %v, want errFake200Response when retry_empty_output=true", err)
	}
}

// TestHandleResponseLegalEmptyCompletionDeliveredWhenRetryDisabled
// ★ 防过度防御：retry_empty_output=false 时，"有 Choices 但无可见文本"的合法
// 空输出（max_tokens=0、只有 tool_calls、只有多模态内容）必须原样交付（nil），
// 不得被假 200 守卫误伤。变异"把合法空 completion 也判成假 200"→ 本测试红。
func TestHandleResponseLegalEmptyCompletionDeliveredWhenRetryDisabled(t *testing.T) {
	seedRetryEmptyOutputSetting(t, "false")

	tests := []struct {
		name string
		resp *transmodel.InternalLLMResponse
	}{
		{
			name: "empty text content, zero completion tokens",
			resp: &transmodel.InternalLLMResponse{
				Choices: []transmodel.Choice{{
					Index:   0,
					Message: &transmodel.Message{Content: transmodel.MessageContent{Content: strPtr("")}},
				}},
				Usage: &transmodel.Usage{CompletionTokens: 0},
			},
		},
		{
			name: "empty completion with tool_calls",
			resp: &transmodel.InternalLLMResponse{
				Choices: []transmodel.Choice{{
					Index: 0,
					Message: &transmodel.Message{
						Content:   transmodel.MessageContent{Content: strPtr("")},
						ToolCalls: []transmodel.ToolCall{{ID: "call_1", Type: "function"}},
					},
				}},
			},
		},
		{
			name: "empty completion with multimodal content",
			resp: &transmodel.InternalLLMResponse{
				Choices: []transmodel.Choice{{
					Index: 0,
					Message: &transmodel.Message{
						Content: transmodel.MessageContent{
							MultipleContent: []transmodel.MessageContentPart{{Type: "text", Text: strPtr("")}},
						},
					},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ra := newFake200HandleAttempt(t, tt.resp)
			if err := runFake200HandleResponse(t, ra); err != nil {
				t.Fatalf("handleResponse() err = %v, want nil — legal empty output must be delivered when retry_empty_output=false", err)
			}
		})
	}
}

// TestHandleResponseLegalEmbeddingDelivered
// ★ ad71355 配对守卫：合法 embedding（EmbeddingData 非空、零 Choices）不得被
// 判成假 200，与 retry_empty_output 设置无关。变异"去掉 embedding 豁免"→ 红。
func TestHandleResponseLegalEmbeddingDelivered(t *testing.T) {
	validEmbedding := &transmodel.InternalLLMResponse{
		ID: "embedding-id",
		EmbeddingData: []transmodel.EmbeddingObject{{
			Index:     0,
			Embedding: transmodel.Embedding{FloatArray: []float64{0.1, 0.2, 0.3}},
		}},
	}

	for _, retrySetting := range []string{"true", "false"} {
		t.Run("retry_empty_output="+retrySetting, func(t *testing.T) {
			seedRetryEmptyOutputSetting(t, retrySetting)
			ra := newFake200HandleAttempt(t, validEmbedding)
			if err := runFake200HandleResponse(t, ra); err != nil {
				t.Fatalf("handleResponse() err = %v, want nil — valid embedding must not be treated as fake 200", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 决策级：errFake200Response 的重试分类（熔断器信号的前提）
// ---------------------------------------------------------------------------

// TestClassifyRelayErrorFake200IsRouteFailure
// ★ 假 200 必须归为"换渠道"的路由级失败（IsError=true），retryWithChannels 才会
// 调 OnFailure → RecordFailure/RecordAutoFailure，熔断器失败计数才会 +1。
// 变异"把假 200 当成功报（err 置 nil）"→ Scope 变 ScopeNone → 红；
// 变异"归为 ScopeSameChannel"→ 不触发 OnFailure → 红。
func TestClassifyRelayErrorFake200IsRouteFailure(t *testing.T) {
	got := ClassifyRelayError(http.StatusOK, errFake200Response, false)
	if got.Scope != ScopeNextChannel {
		t.Fatalf("ClassifyRelayError(fake200) Scope = %v, want ScopeNextChannel (drives OnFailure → RecordFailure)", got.Scope)
	}
	if !got.IsError {
		t.Fatal("ClassifyRelayError(fake200) IsError = false, want true")
	}
	if got.Code != http.StatusOK {
		t.Fatalf("ClassifyRelayError(fake200) Code = %d, want 200 (upstream status preserved for logging)", got.Code)
	}

	// 流式已写出后发生的假 200 不可重试（ScopeAbortAll），保持既有安全语义。
	if written := ClassifyRelayError(http.StatusOK, errFake200Response, true); written.Scope != ScopeAbortAll {
		t.Fatalf("ClassifyRelayError(fake200, written) Scope = %v, want ScopeAbortAll", written.Scope)
	}
}

// ---------------------------------------------------------------------------
// L2 计费层：metrics.Save 的独立校验
// ---------------------------------------------------------------------------

// initFake200CommercialDB 搭一个商业模式内存库：用户余额 quota、其名下 key，
// 并给 modelName 挂常量计费表达式 exprCost。返回 (userID, keyID)。
func initFake200CommercialDB(t *testing.T, quota float64, modelName string, exprCost string) (uint, int) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := setting.SetString(dbmodel.SettingKeyCommercialMode, "true"); err != nil {
		t.Fatalf("enable commercial mode: %v", err)
	}
	if err := setting.SetString(dbmodel.SettingKeyBillingExpr, fmt.Sprintf(`{%q:%q}`, modelName, exprCost)); err != nil {
		t.Fatalf("set billing expr: %v", err)
	}
	u := dbmodel.User{Username: "fake200-user-" + t.Name(), Password: "x", Quota: quota}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	key := dbmodel.APIKey{UserID: u.ID, Name: "fake200-key", APIKey: "sk-fake200-" + t.Name()}
	if err := apikey.Create(&key, context.Background()); err != nil {
		t.Fatalf("create key: %v", err)
	}
	return u.ID, key.ID
}

func fake200Quota(t *testing.T, uid uint) float64 {
	t.Helper()
	rem, _, err := user.GetQuota(uid, context.Background())
	if err != nil {
		t.Fatalf("get quota: %v", err)
	}
	return rem
}

// TestSaveFake200SuccessDemotedAndNotCharged
// ★ L2 核心守卫：即使 relay 层的判定被绕过、假 200 以"成功"身份到达计费层，
// 也不扣费，并降级为失败入账。变异"删掉计费层独立校验"→ 余额被扣 5 → 红；
// 该测试直接驱动 Save，不经过 handleResponse，所以守的是第二层本身。
func TestSaveFake200SuccessDemotedAndNotCharged(t *testing.T) {
	const modelName = "fake200-expr-model"
	uid, kid := initFake200CommercialDB(t, 10.0, modelName, "5")

	m := NewRelayMetrics(kid, modelName, "chat", "chat", "127.0.0.1", nil)
	// 模拟"relay 层漏判"：假 200 响应被收集后按成功收尾。
	m.SetInternalResponse(&transmodel.InternalLLMResponse{
		ID:            "fake-200",
		EmbeddingData: []transmodel.EmbeddingObject{},
		Choices:       []transmodel.Choice{},
	}, modelName)

	beforeFailed, beforeSuccess := st.TotalGet().RequestFailed, st.TotalGet().RequestSuccess
	m.Save(true, nil, nil)

	if rem := fake200Quota(t, uid); math.Abs(rem-10.0) > 1e-9 {
		t.Fatalf("balance after fake-200 Save = %.9f, want 10.0 (no charge, expr fixed fee must not apply)", rem)
	}
	// 降级为失败入账：不得写 RequestSuccess（否则压住错误率告警）。
	after := st.TotalGet()
	if after.RequestSuccess-beforeSuccess != 0 {
		t.Fatalf("RequestSuccess delta = %d, want 0 — fake 200 must not be counted as a success in stats", after.RequestSuccess-beforeSuccess)
	}
	if after.RequestFailed-beforeFailed < 1 {
		t.Fatalf("RequestFailed delta = %d, want >= 1 — fake 200 must be recorded as a failure for error-rate alerting", after.RequestFailed-beforeFailed)
	}
}

// TestSaveValidResponsesStillCharged
// ★ L2 反向守卫（防过度防御）：合法响应照常扣费——合法 embedding（零 Choices、
// EmbeddingData 非空）与合法空 completion（有 tool_calls）都不能被计费层误拦。
// 变异"计费层校验把'零 Choices'也当假 200（丢掉 embedding 豁免）"→ 扣不了 5 → 红。
func TestSaveValidResponsesStillCharged(t *testing.T) {
	t.Run("valid embedding", func(t *testing.T) {
		const modelName = "fake200-valid-embedding"
		uid, kid := initFake200CommercialDB(t, 10.0, modelName, "5")

		m := NewRelayMetrics(kid, modelName, "chat", "chat", "127.0.0.1", nil)
		m.SetInternalResponse(&transmodel.InternalLLMResponse{
			ID: "embedding-id",
			EmbeddingData: []transmodel.EmbeddingObject{{
				Index:     0,
				Embedding: transmodel.Embedding{FloatArray: []float64{0.1}},
			}},
		}, modelName)
		m.Save(true, nil, nil)

		if rem := fake200Quota(t, uid); math.Abs(rem-5.0) > 1e-9 {
			t.Fatalf("balance after valid-embedding Save = %.9f, want 5.0 — valid embedding must still be charged", rem)
		}
	})

	t.Run("legal empty completion with tool_calls", func(t *testing.T) {
		const modelName = "fake200-valid-empty"
		uid, kid := initFake200CommercialDB(t, 10.0, modelName, "5")

		m := NewRelayMetrics(kid, modelName, "chat", "chat", "127.0.0.1", nil)
		m.SetInternalResponse(&transmodel.InternalLLMResponse{
			Choices: []transmodel.Choice{{
				Index: 0,
				Message: &transmodel.Message{
					Content:   transmodel.MessageContent{Content: strPtr("")},
					ToolCalls: []transmodel.ToolCall{{ID: "call_1", Type: "function"}},
				},
			}},
		}, modelName)
		m.Save(true, nil, nil)

		if rem := fake200Quota(t, uid); math.Abs(rem-5.0) > 1e-9 {
			t.Fatalf("balance after legal-empty-completion Save = %.9f, want 5.0 — legal empty output must still be charged", rem)
		}
	})

	// 零载荷但带真实用量的响应（流式聚合后只剩 usage 的形态）不是假 200：
	// 上游确实消耗了 token，必须照常计费。变异"计费层判定去掉 usage 豁免"
	// → 扣不了 5 → 红（与 TestSaveNonZeroCostOnFailureIsStillCharged 互补，
	// 那条守失败路径，这条守成功路径）。
	t.Run("usage-only zero-payload response still charged", func(t *testing.T) {
		const modelName = "fake200-usage-only"
		uid, kid := initFake200CommercialDB(t, 10.0, modelName, "5")

		m := NewRelayMetrics(kid, modelName, "chat", "chat", "127.0.0.1", nil)
		m.SetInternalResponse(&transmodel.InternalLLMResponse{
			Usage: &transmodel.Usage{PromptTokens: 100, CompletionTokens: 20},
		}, modelName)
		m.Save(true, nil, nil)

		if rem := fake200Quota(t, uid); math.Abs(rem-5.0) > 1e-9 {
			t.Fatalf("balance after usage-only Save = %.9f, want 5.0 — recorded usage must still be charged", rem)
		}
	})
}

// TestSaveFailureWithoutRecordedResponseSkipsExprFixedFee
// ★ L2 补充守卫：所有渠道都回假 200 耗尽后，Save(false) 收到的是 nil 响应——
// 没有可交付的内容，表达式计费的固定费不得收取。变异"删掉该分支"→ 余额被扣
// 固定费 → 红。失败但已记录真实用量的请求仍照常扣费（对照组）。
func TestSaveFailureWithoutRecordedResponseSkipsExprFixedFee(t *testing.T) {
	const modelName = "fake200-exhausted-model"
	uid, kid := initFake200CommercialDB(t, 10.0, modelName, "5")

	// 被测路径：失败 + 无任何已记录响应（假 200 耗尽的典型收尾形态）。
	m := NewRelayMetrics(kid, modelName, "chat", "chat", "127.0.0.1", nil)
	m.Save(false, errFake200Response, nil)
	if rem := fake200Quota(t, uid); math.Abs(rem-10.0) > 1e-9 {
		t.Fatalf("balance after nil-response failure Save = %.9f, want 10.0 (no expr fixed fee without any deliverable)", rem)
	}

	// 对照组：失败但上游确实消耗了 token（流中断，用量已收集）→ 仍扣费。
	m2 := NewRelayMetrics(kid, modelName, "chat", "chat", "127.0.0.1", nil)
	m2.SetInternalResponse(&transmodel.InternalLLMResponse{
		Choices: []transmodel.Choice{{
			Index:   0,
			Message: &transmodel.Message{Content: transmodel.MessageContent{Content: strPtr("partial")}},
		}},
		Usage: &transmodel.Usage{PromptTokens: 100, CompletionTokens: 50},
	}, modelName)
	m2.Save(false, errors.New("stream broke mid-delivery"), nil)
	if rem := fake200Quota(t, uid); math.Abs(rem-5.0) > 1e-9 {
		t.Fatalf("balance after failed-but-used Save = %.9f, want 5.0 — failures with recorded usage must still be charged", rem)
	}
}

// ---------------------------------------------------------------------------
// Handler 级端到端：retry_empty_output=false 下的完整链路
// ---------------------------------------------------------------------------

// initFake200HandlerEnv 搭起驱动 Handler 的完整环境：商业计费内存库 +
// 单渠道指向恒回假 200 的上游。返回 (userID, apiKeyID, 上游命中计数)。
func initFake200HandlerEnv(t *testing.T, requestModel string, resolvedModel string, channelID, keyID, groupID int) (uint, int, *int) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	uid, kid := initFake200CommercialDB(t, 10.0, requestModel, "5")

	hits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		// 假 200：HTTP 200 + 错误体。OpenAI Chat 出站 transformer 会把它解析成
		// Choices/EmbeddingData 全空的 InternalLLMResponse（chat.go TransformResponse
		// 对 2xx 只做 unmarshal，不识别 error 字段）。
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"error":{"message":"该接口未接入此模型","type":"invalid_request_error"}}`))
	}))
	t.Cleanup(upstream.Close)

	ch.GetCache().Clear()
	ch.GetCache().Clear()
	ch.GetCache().Set(channelID, dbmodel.Channel{
		ID:       channelID,
		Name:     "fake200-upstream",
		Type:     0, // OutboundTypeOpenAIChat
		Enabled:  true,
		BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}},
		Keys: []dbmodel.ChannelKey{
			{ID: keyID, ChannelID: channelID, Enabled: true, ChannelKey: "sk-fake200-upstream"},
		},
	})

	grp.GetCache().Clear()
	grp.GetCache().Clear()
	grp.GetCache().Set(groupID, dbmodel.Group{
		ID:           groupID,
		Name:         requestModel,
		EndpointType: dbmodel.EndpointTypeChat,
		Mode:         dbmodel.GroupModeFailover,
		Items: []dbmodel.GroupItem{
			{ID: 1, GroupID: groupID, ChannelID: channelID, ModelName: resolvedModel, Priority: 1, Weight: 1},
		},
	})
	grp.RebuildIndexes()

	// 熔断器阈值设为 2 并预置 1 次失败：假 200 若计为失败（第 2 次）则熔断；
	// 若被当成功上报（RecordSuccess 重置计数）则不熔断——两种变异可分。
	if err := setting.SetInt(dbmodel.SettingKeyCircuitBreakerThreshold, 2); err != nil {
		t.Fatalf("set circuit threshold: %v", err)
	}
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

// TestHandlerFake200RetryDisabledNotChargedAndBreakerCountsFailure
// ★ 端到端主守卫：retry_empty_output=false 时假 200 必须——
//   - 不再以 200 交付客户端（耗尽后回 502）；
//   - 不扣费（余额不变，含表达式固定费）；
//   - 计入熔断器失败（预置 1 次 + 本次 = 阈值 2 → 熔断），不重置失败计数；
//   - 不写粘性会话（不把客户端钉在坏渠道上）；
//   - auto 统计记失败不记成功（不把坏渠道排到首选）；
//   - 全局统计记 RequestFailed（不压错误率告警）。
func TestHandlerFake200RetryDisabledNotChargedAndBreakerCountsFailure(t *testing.T) {
	const requestModel = "fake200-e2e-model"
	const resolvedModel = "fake200-e2e-upstream"
	const channelID = 4411
	const keyID = 8811
	const groupID = 7411

	uid, kid, hits := initFake200HandlerEnv(t, requestModel, resolvedModel, channelID, keyID, groupID)
	seedRetryEmptyOutputSetting(t, "false")

	// 预置 1 次熔断失败计数（阈值 2）：假 200 计为失败 → 熔断；
	// 假 200 被当成功上报（重置计数）→ 不熔断。两种走向可区分。
	balancer.RecordFailure(channelID, keyID, resolvedModel)

	beforeStats := st.TotalGet()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}]}`, requestModel)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("api_key_id", kid)

	Handler(dbmodel.EndpointTypeChat, inbound.InboundTypeOpenAIChat, c)

	if *hits == 0 {
		t.Fatal("upstream never received the request — nothing was forwarded")
	}
	// ★ 不扣费：余额必须一分不动（表达式固定费也不收）。
	if rem := fake200Quota(t, uid); math.Abs(rem-10.0) > 1e-9 {
		t.Fatalf("balance after fake-200 request = %.9f, want 10.0 — fake 200 must not be charged even with retry_empty_output=false", rem)
	}
	// ★ 不以 200 交付：假 200 被拦为渠道失败，单渠道耗尽后是 502。
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("client status = %d, want 502 — fake 200 must not be delivered as a success (body=%s)", rec.Code, rec.Body.String())
	}
	// ★ 熔断器：预置 1 次 + 假 200 计为失败 = 阈值 2 → 熔断。
	// 若假 200 走了成功上报（RecordSuccess 重置计数），这里会是 false。
	if tripped, _ := balancer.IsTripped(channelID, keyID, resolvedModel); !tripped {
		t.Fatal("circuit breaker not tripped — fake 200 must count as a channel failure, not reset the failure count")
	}
	// ★ 粘性：不得把 (apiKey, model) 钉在回假 200 的渠道上。
	if entry := balancer.GetSticky(kid, requestModel, time.Minute); entry != nil {
		t.Fatalf("sticky entry set to channel %d — fake 200 must not pin clients to the bad channel", entry.ChannelID)
	}
	// ★ auto 评分：记失败不记成功。
	successRate, samples := balancer.GetAutoStats(channelID, resolvedModel)
	if samples < 1 {
		t.Fatal("auto stats has no samples — fake 200 outcome was not recorded for the Auto strategy")
	}
	if successRate != 0 {
		t.Fatalf("auto successRate = %f, want 0 — fake 200 must be recorded as failure, not success", successRate)
	}
	// ★ 错误率告警信号：全局统计记 RequestFailed、不记 RequestSuccess。
	afterStats := st.TotalGet()
	if d := afterStats.RequestSuccess - beforeStats.RequestSuccess; d != 0 {
		t.Fatalf("RequestSuccess delta = %d, want 0 — fake 200 must not suppress the error-rate alert", d)
	}
	if d := afterStats.RequestFailed - beforeStats.RequestFailed; d < 1 {
		t.Fatalf("RequestFailed delta = %d, want >= 1 — fake 200 must show up as a failure in global stats", d)
	}
}
