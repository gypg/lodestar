package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	appmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/transformer/outbound"
)

/*
探测**通过**时，上游响应体必须一路带到 GroupModelTestResult.ResponseText
和 relay_logs.response_content。

为什么需要这个守卫：前端三处（channel/Form.tsx、group/Card.tsx、
group/GroupListItem.tsx）现在都会在成功时渲染响应体折叠框，
其前提正是"成功时 ResponseText 非空"。而在此之前**全仓库没有一个测试
断言过 ResponseText / ResponseBody**——唯一沾边的
TestSendGroupProbeRequest_EmbeddingsUseEmbeddingPayload 只对 embeddings
断言"非空"，既不覆盖 chat，也不校验内容。
把成功分支改成 `return resp.StatusCode, "", nil` 曾经不会让任何测试变红。

入口选导出的 TestGroupModels，不直接调 sendGroupProbeRequest：
后者只能守住"函数返不返 body"，守不住"testGroupModelItem 有没有把它
赋给 result.ResponseText"（group_probe.go:403）以及 recordTestLog
有没有落库（:473）——绕过调用点的测试冒充守卫是 [[lodestar-worker-false-evidence]]
第七变体。

断言的是**完整字面响应体**而不是"非空"：一个只回传状态行、
或把 body 换成占位串的实现都能满足宽松断言。

变异验收（全红）：
  - group_probe.go:623 成功分支改 `return resp.StatusCode, "", nil` → 杀
  - group_probe.go:403 删 `result.ResponseText = responseText` → 杀
  - group_probe.go:473 的 `if result.ResponseText != ""` 改成恒 false → 杀（日志腿）
  - 把返回的 body 截断/替换成占位串 → 杀（因为断言全等）
*/

// probeSuccessBody 是上游成功时返回的完整报文。断言全等，所以这里的每个
// 字节都是期望值的一部分。
const probeSuccessBody = `{"id":"chatcmpl-body-1","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`

func TestGroupModelsKeepsUpstreamBodyOnPassingProbe(t *testing.T) {
	ctx := initGroupProbeLogTestEnv(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(probeSuccessBody))
	}))
	defer upstream.Close()

	// 940101：避开既有用例用过的 91xxxx/92xxxx，绑定缓存含永不过期的负向缓存，
	// 复用 channel ID 会串扰（见 [[session-2026-08-13-probe-sitestats]]）。
	const channelID = 940101
	channels := map[int]appmodel.Channel{
		channelID: {
			ID:       channelID,
			Name:     "probe-channel-body",
			Type:     outbound.OutboundTypeOpenAIChat,
			Enabled:  true,
			BaseUrls: []appmodel.BaseUrl{{URL: upstream.URL}},
			Keys:     []appmodel.ChannelKey{{Enabled: true, ChannelKey: "sk-probe"}},
		},
	}
	group := &appmodel.Group{
		Name:         "probe-group-body",
		EndpointType: appmodel.EndpointTypeAll,
		Items: []appmodel.GroupItem{
			{ID: 1, ChannelID: channelID, ModelName: "gpt-4o-mini", Priority: 1, Weight: 1},
		},
	}

	summary, err := TestGroupModels(ctx, group, channels)
	if err != nil {
		t.Fatalf("TestGroupModels() error = %v", err)
	}
	if !summary.Passed {
		t.Fatalf("TestGroupModels() summary.Passed = false, results = %+v", summary.Results)
	}
	if len(summary.Results) != 1 {
		t.Fatalf("summary.Results = %d, want exactly 1", len(summary.Results))
	}

	got := summary.Results[0]
	if !got.Passed {
		t.Fatalf("result.Passed = false, want true; message = %q", got.Message)
	}
	// 这条是前端成功折叠框的全部依据：通过的探测也必须带回响应体。
	if got.ResponseText != probeSuccessBody {
		t.Fatalf("result.ResponseText on a PASSING probe =\n%q\nwant\n%q", got.ResponseText, probeSuccessBody)
	}

	// 第二条腿：落库的 relay log 也要带上同一份响应体，否则历史日志里
	// 成功记录依然是空的。
	logs := cachedTestLogs(t)
	if len(logs) != 1 {
		t.Fatalf("buffered test logs = %d, want exactly 1", len(logs))
	}
	if logs[0].ResponseContent != probeSuccessBody {
		t.Fatalf("relay log ResponseContent on a PASSING probe =\n%q\nwant\n%q", logs[0].ResponseContent, probeSuccessBody)
	}
}

// TestChannelTestKeepsUpstreamBodyOnPassingResult 覆盖另一条独立路径：
// 渠道连通性测试（channel_probe.go）。它不经过 group probe，
// ResponseBody 的赋值点在 Passed 判定之前（channel_probe.go:96），
// 这个顺序正是"成功也有 body"的原因，所以要单独锁住。
func TestChannelTestKeepsUpstreamBodyOnPassingResult(t *testing.T) {
	const modelsBody = `{"object":"list","data":[{"id":"gpt-4o-mini","object":"model"}]}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("request path = %q, want /v1/models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(modelsBody))
	}))
	defer upstream.Close()

	channel := appmodel.Channel{
		Type:     outbound.OutboundTypeOpenAIChat,
		BaseUrls: []appmodel.BaseUrl{{URL: upstream.URL}},
		Keys:     []appmodel.ChannelKey{{Enabled: true, ChannelKey: "sk-probe", Remark: "primary"}},
	}

	summary, err := TestChannel(t.Context(), channel)
	if err != nil {
		t.Fatalf("TestChannel() error = %v", err)
	}
	if len(summary.Results) != 1 {
		t.Fatalf("summary.Results = %d, want exactly 1", len(summary.Results))
	}

	got := summary.Results[0]
	if !got.Passed {
		t.Fatalf("result.Passed = false, want true; message = %q", got.Message)
	}
	if got.ResponseBody != modelsBody {
		t.Fatalf("result.ResponseBody on a PASSING channel test =\n%q\nwant\n%q", got.ResponseBody, modelsBody)
	}
}
