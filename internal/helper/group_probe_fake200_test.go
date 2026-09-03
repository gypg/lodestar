package helper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/transformer/outbound"
)

/*
WO-027 阶段 A 的守卫：探测器必须把"假 200"判为失败。

背景：上游会返回 HTTP 200 但响应体解析不出任何有效载荷（choices: null、
"该接口未接入"这类错误体）。中继侧早有 isFake200Response 拦截，但探测器
sendGroupProbeRequest 的成功判据只有"2xx + TransformResponse 不报错"，
而出站 TransformResponse 对空 choices 照样解析成功——假 200 模型会被判
"通过"，在模型广场里看起来是健康的，客户点了就失败。

判据本体在 model.InternalLLMResponse.IsFake200（判据单点，relay 与探测器
共用）。本文件守的是探测器这层的接线；relay 层行为由既有的
fake200_defense_test.go / passthrough_fake200_e2e_test.go 继续守（T-A3）。

变异验收（全红）：
  - M-A1 去掉 sendGroupProbeRequest 的 IsFake200 判定 → T-A1 / T-A1E2E / T-A4 红
  - M-A2 判据改成"只看 Choices 为空"（忽略 EmbeddingData）→ T-A2 的
    embedding 有 data 腿红（注意：T-A4 本身在 M-A2 下依然绿——空 data
    在两种判据下都判失败，杀死 M-A2 靠的是"合法 embedding 仍通过"的对照腿）
  - M-A3 判据改成恒返回 true（一切都算假 200）→ T-A2 两腿全红
*/

// fake200ChatBody 是上游假 200 的典型报文：HTTP 200、JSON 合法、choices 为 null。
const fake200ChatBody = `{"id":"chatcmpl-fake-1","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":null}`

// fake200EmbeddingBody 是 embedding 端点的假 200：HTTP 200、object=list、data 为 null。
const fake200EmbeddingBody = `{"id":"emb-fake-1","object":"list","created":1,"model":"text-embedding-3-small","data":null}`

// legitEmbeddingBody 是合法 embedding 报文：Choices 恒为空（embedding 响应的合法
// 形态），有效载荷在 data 里。判据若忽略 EmbeddingData（M-A2），这条会被误判。
const legitEmbeddingBody = `{"id":"emb-1","object":"list","created":1,"model":"text-embedding-3-small","data":[{"object":"embedding","index":0,"embedding":[0.1]}]}`

// TestSendGroupProbeRequest_Fake200ChatBodyJudgedFailed（T-A1）：HTTP 200 +
// choices: null 必须判为失败，且错误信息含可识别的 "fake 200" 字样。
// 断言 statusCode == 200：证明拦下它的是载荷判据而不是状态码门。
func TestSendGroupProbeRequest_Fake200ChatBodyJudgedFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fake200ChatBody))
	}))
	defer server.Close()

	channel := &appmodel.Channel{
		Type:     outbound.OutboundTypeOpenAIChat,
		BaseUrls: []appmodel.BaseUrl{{URL: server.URL}},
	}

	statusCode, responseText, err := sendGroupProbeRequest(
		context.Background(),
		outbound.Get(outbound.OutboundTypeOpenAIChat),
		channel,
		"sk-test",
		appmodel.EndpointTypeAll,
		"gpt-4o-mini",
	)
	if err == nil {
		t.Fatal("sendGroupProbeRequest() error = nil, want fake-200 failure: a 200 body with choices:null must not pass the probe")
	}
	if !strings.Contains(err.Error(), "fake 200") {
		t.Fatalf("sendGroupProbeRequest() error = %q, want it to contain \"fake 200\" so operators can tell it apart from a transport error", err.Error())
	}
	if statusCode != http.StatusOK {
		t.Fatalf("sendGroupProbeRequest() status = %d, want 200: the upstream really returned 200, the failure is body-level", statusCode)
	}
	if responseText != fake200ChatBody {
		t.Fatalf("sendGroupProbeRequest() responseText = %q, want the raw upstream body so the operator can diagnose it", responseText)
	}
}

// TestSendGroupProbeRequest_NormalBodiesStillPass（T-A2，双腿）：合法 chat 响应
// 与合法 embedding 响应都必须仍判通过。embedding 腿是 M-A2 的杀手：
// 判据若只看 Choices，合法 embedding（Choices 恒空）会被误杀。
func TestSendGroupProbeRequest_NormalBodiesStillPass(t *testing.T) {
	cases := []struct {
		name         string
		outboundType outbound.OutboundType
		endpointType string
		modelName    string
		body         string
	}{
		{
			name:         "chat with choices",
			outboundType: outbound.OutboundTypeOpenAIChat,
			endpointType: appmodel.EndpointTypeAll,
			modelName:    "gpt-4o-mini",
			body:         `{"id":"chatcmpl-ok-1","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
		},
		{
			name:         "embedding with data",
			outboundType: outbound.OutboundTypeOpenAIEmbedding,
			endpointType: appmodel.EndpointTypeEmbeddings,
			modelName:    "text-embedding-3-small",
			body:         legitEmbeddingBody,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			channel := &appmodel.Channel{
				Type:     tc.outboundType,
				BaseUrls: []appmodel.BaseUrl{{URL: server.URL}},
			}

			statusCode, responseText, err := sendGroupProbeRequest(
				context.Background(),
				outbound.Get(tc.outboundType),
				channel,
				"sk-test",
				tc.endpointType,
				tc.modelName,
			)
			if err != nil {
				t.Fatalf("sendGroupProbeRequest() error = %v, want nil: a legitimate 200 payload must still pass the probe", err)
			}
			if statusCode != http.StatusOK {
				t.Fatalf("sendGroupProbeRequest() status = %d, want 200", statusCode)
			}
			if responseText != tc.body {
				t.Fatalf("sendGroupProbeRequest() responseText = %q, want the full upstream body", responseText)
			}
		})
	}
}

// TestSendGroupProbeRequest_EmbeddingEmptyDataJudgedFailed（T-A4）：embedding
// 端点返回 200 但 data 为空 → 判据是 Choices 与 EmbeddingData **都**为空才算
// 合法，这条必须判失败。
func TestSendGroupProbeRequest_EmbeddingEmptyDataJudgedFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fake200EmbeddingBody))
	}))
	defer server.Close()

	channel := &appmodel.Channel{
		Type:     outbound.OutboundTypeOpenAIEmbedding,
		BaseUrls: []appmodel.BaseUrl{{URL: server.URL}},
	}

	statusCode, _, err := sendGroupProbeRequest(
		context.Background(),
		outbound.Get(outbound.OutboundTypeOpenAIEmbedding),
		channel,
		"sk-test",
		appmodel.EndpointTypeEmbeddings,
		"text-embedding-3-small",
	)
	if err == nil {
		t.Fatal("sendGroupProbeRequest() error = nil, want fake-200 failure: a 200 embedding body with empty data must not pass")
	}
	if !strings.Contains(err.Error(), "fake 200") {
		t.Fatalf("sendGroupProbeRequest() error = %q, want it to contain \"fake 200\"", err.Error())
	}
	if statusCode != http.StatusOK {
		t.Fatalf("sendGroupProbeRequest() status = %d, want 200", statusCode)
	}
}

// TestGroupModelsJudgesFake200AsFailure（T-A1 的端到端腿）：走导出入口
// TestGroupModels，断言失败一路传到 summary——运营者看到的是"该模型探测
// 失败，原因是 fake 200"，而不是一个孤立的函数返回值。
func TestGroupModelsJudgesFake200AsFailure(t *testing.T) {
	ctx := initGroupProbeLogTestEnv(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fake200ChatBody))
	}))
	defer upstream.Close()

	// 950101：避开既有用例用过的 91xxxx/92xxxx/94xxxx 段，避免绑定缓存串扰。
	const channelID = 950101
	channels := map[int]appmodel.Channel{
		channelID: {
			ID:       channelID,
			Name:     "probe-channel-fake200",
			Type:     outbound.OutboundTypeOpenAIChat,
			Enabled:  true,
			BaseUrls: []appmodel.BaseUrl{{URL: upstream.URL}},
			Keys:     []appmodel.ChannelKey{{Enabled: true, ChannelKey: "sk-probe"}},
		},
	}
	group := &appmodel.Group{
		Name:         "probe-group-fake200",
		EndpointType: appmodel.EndpointTypeAll,
		Items: []appmodel.GroupItem{
			{ID: 1, ChannelID: channelID, ModelName: "gpt-4o-mini", Priority: 1, Weight: 1},
		},
	}

	summary, err := TestGroupModels(ctx, group, channels)
	if err != nil {
		t.Fatalf("TestGroupModels() error = %v", err)
	}
	if summary.Passed {
		t.Fatalf("TestGroupModels() summary.Passed = true, want false: a fake-200 model must not look healthy (results = %+v)", summary.Results)
	}
	if len(summary.Results) != 1 {
		t.Fatalf("TestGroupModels() results len = %d, want 1", len(summary.Results))
	}
	got := summary.Results[0]
	if got.Passed {
		t.Fatalf("result.Passed = true, want false for a fake-200 upstream (message = %q)", got.Message)
	}
	if !strings.Contains(got.Message, "fake 200") {
		t.Fatalf("result.Message = %q, want it to contain \"fake 200\": the operator-facing message must name the failure mode", got.Message)
	}
	if got.StatusCode != http.StatusOK {
		t.Fatalf("result.StatusCode = %d, want 200", got.StatusCode)
	}
}
