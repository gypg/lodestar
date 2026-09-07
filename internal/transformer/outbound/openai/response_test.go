package openai

import (
	"encoding/json"
	"testing"

	"github.com/gypg/lodestar/internal/transformer/model"
)

// TestConvertToResponsesRequest_ForwardsIncludeAndTopLogprobs 钉死两个曾被静默丢弃的
// 客户端字段。
//
// inbound/openai/response.go 解析 include 与 top_logprobs 并接进 InternalLLMRequest
// （Include / TopLogprobs 都是有主的字段，不是摆设），但出站的 ResponsesRequest 从来
// 没有这两个字段，于是客户端要什么都到不了上游，且不报错。
//
// Include 只在 Responses 这条出站路上合法 —— chat.go 是显式 request.Include = nil
// （chat completions 没有这个参数）。所以这里是它唯一的出口，漏了就等于无条件丢弃。
//
// 断言走 JSON 而不只看结构体字段：结构体字段对了但 json tag 写错，上游同样收不到。
func TestConvertToResponsesRequest_ForwardsIncludeAndTopLogprobs(t *testing.T) {
	topLogprobs := int64(5)
	req := &model.InternalLLMRequest{
		Model:       "gpt-5",
		Include:     []string{"reasoning.encrypted_content", "message.output_text.logprobs"},
		TopLogprobs: &topLogprobs,
	}

	got := ConvertToResponsesRequest(req)

	if len(got.Include) != 2 || got.Include[0] != "reasoning.encrypted_content" || got.Include[1] != "message.output_text.logprobs" {
		t.Fatalf("Include = %#v, want the two requested include values", got.Include)
	}
	if got.TopLogprobs == nil {
		t.Fatal("TopLogprobs = nil, want 5")
	}
	if *got.TopLogprobs != 5 {
		t.Fatalf("TopLogprobs = %d, want 5", *got.TopLogprobs)
	}

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	include, ok := wire["include"].([]any)
	if !ok || len(include) != 2 {
		t.Fatalf("wire include = %#v, want a 2-element array under key \"include\"", wire["include"])
	}
	if v, ok := wire["top_logprobs"].(float64); !ok || v != 5 {
		t.Fatalf("wire top_logprobs = %#v, want 5", wire["top_logprobs"])
	}
}

// TestConvertToResponsesRequest_OmitsEmptyIncludeAndTopLogprobs 空值不得出现在线格上。
// include: [] 与 top_logprobs: null 都可能被上游当成显式请求而报 400，
// 所以两个字段必须是 omitempty 且默认不发。
func TestConvertToResponsesRequest_OmitsEmptyIncludeAndTopLogprobs(t *testing.T) {
	got := ConvertToResponsesRequest(&model.InternalLLMRequest{Model: "gpt-5"})

	if got.Include != nil {
		t.Fatalf("Include = %#v, want nil", got.Include)
	}
	if got.TopLogprobs != nil {
		t.Fatalf("TopLogprobs = %v, want nil", got.TopLogprobs)
	}

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if _, present := wire["include"]; present {
		t.Errorf("wire carries \"include\" for a request that asked for nothing: %s", body)
	}
	if _, present := wire["top_logprobs"]; present {
		t.Errorf("wire carries \"top_logprobs\" for a request that asked for nothing: %s", body)
	}
}

// TestConvertToResponsesRequest_DoesNotForwardReasoningBudget 钉死一个刻意的不转发。
//
// 入站接受 reasoning.max_tokens 并存进 InternalLLMRequest.ReasoningBudget，是 Lodestar
// 自己的扩展，用来让 /v1/responses 客户端表达思考预算并送达 Anthropic 上游
// （outbound/anthropic/messages.go 的 getThinkingBudget 是唯一消费者）。
// OpenAI 的 reasoning 对象只有 effort / summary，没有 max_tokens。整个 OpenAI 系
// 出站都不发这个字段 —— 发了会把"少一个可选参数"换成上游硬 400。
//
// 所以入站有、出站无不是漂移，是对的。这条测试防止后来者"顺手补齐字段"。
func TestConvertToResponsesRequest_DoesNotForwardReasoningBudget(t *testing.T) {
	budget := int64(4096)
	got := ConvertToResponsesRequest(&model.InternalLLMRequest{
		Model:           "gpt-5",
		ReasoningEffort: "high",
		ReasoningBudget: &budget,
	})

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var wire struct {
		Reasoning map[string]any `json:"reasoning"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if wire.Reasoning["effort"] != "high" {
		t.Fatalf("reasoning.effort = %#v, want high", wire.Reasoning["effort"])
	}
	if v, present := wire.Reasoning["max_tokens"]; present {
		t.Errorf("reasoning.max_tokens = %#v on the wire; OpenAI has no such field and would 400", v)
	}
}

func TestConvertToResponsesRequest_OmitsNoneReasoningEffort(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model:           "mimo-v2.5-pro",
		ReasoningEffort: "none",
	}

	got := ConvertToResponsesRequest(req)
	if got.Reasoning != nil {
		t.Fatalf("expected reasoning to be omitted, got %#v", got.Reasoning)
	}
}

func TestConvertToResponsesRequest_PreservesValidReasoningEffort(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model:           "o3",
		ReasoningEffort: "high",
	}

	got := ConvertToResponsesRequest(req)
	if got.Reasoning == nil {
		t.Fatalf("expected reasoning to be present")
	}
	if got.Reasoning.Effort != "high" {
		t.Fatalf("expected reasoning effort high, got %q", got.Reasoning.Effort)
	}
}

// WO-043：转换器硬化。历史 function_call / function_call_output 标 status=completed，
// 空 content 的 assistant 消息不再生成 output_text——两者都是严格 Responses 网关
// "No tool output found for tool call" 400 的根因。

func TestConvertAssistantMessageToResponses_MarksToolCallsCompletedAndSkipsEmptyContent(t *testing.T) {
	empty := ""
	msg := model.Message{
		Role:    "assistant",
		Content: model.MessageContent{Content: &empty},
		ToolCalls: []model.ToolCall{
			{
				ID: "call_01_test",
				Function: model.FunctionCall{
					Name:      "terminal",
					Arguments: `{"command":"ls"}`,
				},
			},
		},
	}

	items := convertAssistantMessageToResponses(msg)
	if len(items) != 1 {
		t.Fatalf("expected only function_call item (no empty message), got %#v", items)
	}
	if items[0].Type != "function_call" {
		t.Fatalf("type = %q, want function_call", items[0].Type)
	}
	if items[0].CallID != "call_01_test" {
		t.Fatalf("call_id = %q", items[0].CallID)
	}
	if items[0].Status == nil || *items[0].Status != "completed" {
		t.Fatalf("status = %#v, want completed", items[0].Status)
	}
}

func TestConvertAssistantMessageToResponses_KeepsNonEmptyTextMessage(t *testing.T) {
	msg := model.Message{
		Role:    "assistant",
		Content: model.MessageContent{Content: strPtr("done")},
	}
	items := convertAssistantMessageToResponses(msg)
	if len(items) != 1 || items[0].Type != "message" {
		t.Fatalf("expected one message item, got %#v", items)
	}
}

func TestConvertAssistantMessageToResponses_TrimsWhitespaceOnlyContent(t *testing.T) {
	msg := model.Message{
		Role:    "assistant",
		Content: model.MessageContent{Content: strPtr("   \n\t ")},
		ToolCalls: []model.ToolCall{
			{ID: "call_x", Function: model.FunctionCall{Name: "f", Arguments: "{}"}},
		},
	}
	items := convertAssistantMessageToResponses(msg)
	for _, it := range items {
		if it.Type == "message" {
			t.Fatalf("whitespace-only content must not produce a message item, got %#v", items)
		}
	}
}

func TestConvertToolMessageToResponses_MarksOutputCompleted(t *testing.T) {
	toolCallID := "call_01_test"
	msg := model.Message{
		Role:       "tool",
		ToolCallID: &toolCallID,
		Content:    model.MessageContent{Content: strPtr(`{"ok":true}`)},
	}
	item := convertToolMessageToResponses(msg)
	if item.Type != "function_call_output" {
		t.Fatalf("type = %q", item.Type)
	}
	if item.Status == nil || *item.Status != "completed" {
		t.Fatalf("status = %#v, want completed", item.Status)
	}
	if item.CallID != toolCallID {
		t.Fatalf("call_id = %q", item.CallID)
	}
}
