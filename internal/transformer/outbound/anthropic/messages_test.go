package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/samber/lo"

	anthropicModel "github.com/gypg/lodestar/internal/transformer/inbound/anthropic"
	"github.com/gypg/lodestar/internal/transformer/model"
)

// G1: TransformRequest builds the outbound HTTP request (URL, method, headers) and
// normalizes the trailing slash so /v1/ and /v1 both yield /v1/messages.
func TestTransformRequest_BaseURLTrailingSlash(t *testing.T) {
	o := &MessageOutbound{}
	req, err := o.TransformRequest(context.Background(), &model.InternalLLMRequest{Model: "m"}, "https://api.anthropic.com/v1/", "key")
	if err != nil {
		t.Fatalf("TransformRequest err = %v", err)
	}
	if got := req.URL.String(); got != "https://api.anthropic.com/v1/messages" {
		t.Errorf("URL.String() = %q, want %q", got, "https://api.anthropic.com/v1/messages")
	}
}

func TestTransformRequest_QueryPassedThrough(t *testing.T) {
	o := &MessageOutbound{}
	req, err := o.TransformRequest(context.Background(), &model.InternalLLMRequest{Model: "m", Query: url.Values{"beta": {"true"}}}, "https://api.anthropic.com/v1/", "key")
	if err != nil {
		t.Fatalf("TransformRequest err = %v", err)
	}
	if got := req.URL.String(); got != "https://api.anthropic.com/v1/messages?beta=true" {
		t.Errorf("URL.String() = %q, want %q", got, "https://api.anthropic.com/v1/messages?beta=true")
	}
}

func TestTransformRequest_MethodAndHeaders(t *testing.T) {
	o := &MessageOutbound{}
	req, err := o.TransformRequest(context.Background(), &model.InternalLLMRequest{Model: "m"}, "https://api.anthropic.com/v1", "sk-seq")
	if err != nil {
		t.Fatalf("TransformRequest err = %v", err)
	}
	if req.Method != http.MethodPost {
		t.Errorf("Method = %q, want %q", req.Method, http.MethodPost)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := req.Header.Get("Anthropic-Version"); got != "2023-06-01" {
		t.Errorf("Anthropic-Version = %q, want 2023-06-01", got)
	}
	if got := req.Header.Get("X-API-Key"); got != "sk-seq" {
		t.Errorf("X-API-Key = %q, want sk-seq", got)
	}
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept (non-stream) = %q, want application/json", got)
	}
}

func TestTransformRequest_StreamAcceptHeader(t *testing.T) {
	o := &MessageOutbound{}
	req, err := o.TransformRequest(context.Background(), &model.InternalLLMRequest{Model: "m", Stream: lo.ToPtr(true)}, "https://api.anthropic.com/v1", "k")
	if err != nil {
		t.Fatalf("TransformRequest err = %v", err)
	}
	if got := req.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept (stream) = %q, want text/event-stream", got)
	}
}

func TestTransformRequest_NilRequest(t *testing.T) {
	o := &MessageOutbound{}
	_, err := o.TransformRequest(context.Background(), nil, "https://api.anthropic.com/v1", "k")
	if err == nil || err.Error() != "request is nil" {
		t.Errorf("err = %v, want %q", err, "request is nil")
	}
}

// G1: TransformRequest surfaces an error when the base URL cannot be parsed.
func TestTransformRequest_InvalidBaseURL(t *testing.T) {
	o := &MessageOutbound{}
	_, err := o.TransformRequest(context.Background(), &model.InternalLLMRequest{Model: "m"}, "://bad url", "k")
	if err == nil || !strings.Contains(err.Error(), "failed to parse base url") {
		t.Errorf("err = %v, want to contain failed to parse base url", err)
	}
}

// G2: resolveMaxTokens picks MaxTokens over MaxCompletionTokens, defaults to 8192,
// and clamps the lower bound to 1.
func TestResolveMaxTokens(t *testing.T) {
	cases := []struct {
		name string
		req  *model.InternalLLMRequest
		want int64
	}{
		{"neitherSet", &model.InternalLLMRequest{}, 8192},
		{"maxTokensOnly", &model.InternalLLMRequest{MaxTokens: lo.ToPtr(int64(5))}, 5},
		{"maxCompletionOnly", &model.InternalLLMRequest{MaxCompletionTokens: lo.ToPtr(int64(7))}, 7},
		{"bothSetMaxTokensWins", &model.InternalLLMRequest{MaxTokens: lo.ToPtr(int64(5)), MaxCompletionTokens: lo.ToPtr(int64(7))}, 5},
		{"maxTokensZeroClamped", &model.InternalLLMRequest{MaxTokens: lo.ToPtr(int64(0))}, 1},
		{"maxTokensNegativeClamped", &model.InternalLLMRequest{MaxTokens: lo.ToPtr(int64(-9))}, 1},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveMaxTokens(tt.req); got != tt.want {
				t.Errorf("resolveMaxTokens(%v) = %d, want %d", tt.req, got, tt.want)
			}
		})
	}
}

// G2: getThinkingBudget maps effort strings to fixed budgets, but "max" has no case
// and falls through to the default 8192. current behavior, not an inferred expectation.
func TestGetThinkingBudgetMaxEffortFallsBackToDefault(t *testing.T) {
	cases := []struct {
		name   string
		effort string
		budget *int64
		want   int64
	}{
		{"low", anthropicModel.EffortLow, nil, 1024},
		{"medium", anthropicModel.EffortMedium, nil, 8192},
		{"high", anthropicModel.EffortHigh, nil, 32768},
		{"maxFallsBackToDefault", anthropicModel.EffortMax, nil, 8192},
		{"empty", "", nil, 8192},
		{"bogus", "bogus", nil, 8192},
		{"explicitBudgetOverridesElement", anthropicModel.EffortLow, lo.ToPtr(int64(42)), 42},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := getThinkingBudget(tt.effort, tt.budget)
			if got == nil || *got != tt.want {
				t.Errorf("getThinkingBudget(%q,%v) = %v, want %d", tt.effort, tt.budget, got, tt.want)
			}
		})
	}
}

// G2: convertToAnthropicRequest thinking branch — adaptive emits output_config without
// budget_tokens, enabled emits budget_tokens without output_config; they are mutually exclusive.
func TestConvertToAnthropicRequestThinkingBranch(t *testing.T) {
	cases := []struct {
		name string
		req  *model.InternalLLMRequest
		want string
	}{
		{"adaptive", &model.InternalLLMRequest{Model: "m", ReasoningEffort: "high", AdaptiveThinking: true}, `{"max_tokens":8192,"messages":[],"model":"m","thinking":{"type":"adaptive"},"output_config":{"effort":"high"}}`},
		{"enabled", &model.InternalLLMRequest{Model: "m", ReasoningEffort: "high", AdaptiveThinking: false}, `{"max_tokens":8192,"messages":[],"model":"m","thinking":{"type":"enabled","budget_tokens":32768}}`},
		{"noEffort", &model.InternalLLMRequest{Model: "m"}, `{"max_tokens":8192,"messages":[],"model":"m"}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := convertToAnthropicRequest(tt.req)
			b, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("json.Marshal err = %v", err)
			}
			if string(b) != tt.want {
				t.Errorf("convertToAnthropicRequest(%v) = %s, want %s", tt.req, string(b), tt.want)
			}
		})
	}
}

// G3: convertSystemPrompt only collects system-role messages and returns nil when absent.
func TestConvertSystemPromptNoSystem(t *testing.T) {
	req := &model.InternalLLMRequest{Messages: []model.Message{{Role: "user", Content: model.MessageContent{Content: lo.ToPtr("hi")}}}}
	if got := convertSystemPrompt(req); got != nil {
		t.Errorf("convertSystemPrompt = %v, want nil", got)
	}
}

// G3: convertSystemPrompt single system message serializes as a text block array.
func TestConvertSystemPromptSingle(t *testing.T) {
	req := &model.InternalLLMRequest{Messages: []model.Message{{Role: "system", Content: model.MessageContent{Content: lo.ToPtr("sys1")}}}}
	got := convertSystemPrompt(req)
	if got == nil {
		t.Fatal("convertSystemPrompt = nil, want non-nil")
	}
	if got.Prompt != nil {
		t.Errorf("Prompt = %v, want nil", *got.Prompt)
	}
	if len(got.MultiplePrompts) != 1 {
		t.Fatalf("len(MultiplePrompts) = %d, want 1", len(got.MultiplePrompts))
	}
	b, _ := json.Marshal(got)
	if string(b) != `[{"type":"text","text":"sys1"}]` {
		t.Errorf("json.Marshal = %s, want %s", string(b), `[{"type":"text","text":"sys1"}]`)
	}
}

// G3: convertSystemPrompt multiple system messages serialize in order, carrying cache_control.
func TestConvertSystemPromptMultiple(t *testing.T) {
	req := &model.InternalLLMRequest{Messages: []model.Message{
		{Role: "system", Content: model.MessageContent{Content: lo.ToPtr("a")}},
		{Role: "system", Content: model.MessageContent{Content: lo.ToPtr("b")}, CacheControl: &model.CacheControl{Type: "ephemeral"}},
	}}
	got := convertSystemPrompt(req)
	b, _ := json.Marshal(got)
	want := `[{"type":"text","text":"a"},{"type":"text","text":"b","cache_control":{"type":"ephemeral"}}]`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

// G3.1: convertSystemPrompt drops a system message whose content lives in MultipleContent
// (Content is nil) to an empty string. current behavior: multi-part system content is
// silently lost; locked in to catch future change.
func TestConvertSystemPromptDropsMultipleContentSystemMessage(t *testing.T) {
	req := &model.InternalLLMRequest{Messages: []model.Message{{Role: "system", Content: model.MessageContent{MultipleContent: []model.MessageContentPart{{Type: "text", Text: lo.ToPtr("real")}}}}}}
	got := convertSystemPrompt(req)
	b, _ := json.Marshal(got)
	want := `[{"type":"text","text":""}]`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

// G4.1: convertMessages merges consecutive same-role (tool) messages into one user message
// with both tool_result blocks in order.
func TestConvertMessagesMergesConsecutiveToolMessages(t *testing.T) {
	req := &model.InternalLLMRequest{Messages: []model.Message{
		{Role: "tool", ToolCallID: lo.ToPtr("t1"), Content: model.MessageContent{Content: lo.ToPtr("r1")}},
		{Role: "tool", ToolCallID: lo.ToPtr("t2"), Content: model.MessageContent{Content: lo.ToPtr("r2")}},
	}}
	got := convertMessages(req)
	if len(got) != 1 {
		t.Fatalf("len(convertMessages) = %d, want 1", len(got))
	}
	b, _ := json.Marshal(got)
	want := `[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"r1"},{"type":"tool_result","tool_use_id":"t2","content":"r2"}]}]`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

// G4.2: convertMessages groups tool messages sharing a MessageIndex, appends the linked
// user message content after the tool_results, and emits exactly one user message.
func TestConvertMessagesGroupsToolMessagesByIndexAndAppendsUserContent(t *testing.T) {
	idx := 0
	req := &model.InternalLLMRequest{Messages: []model.Message{
		{Role: "tool", MessageIndex: &idx, ToolCallID: lo.ToPtr("t1"), Content: model.MessageContent{Content: lo.ToPtr("r1")}},
		{Role: "tool", MessageIndex: &idx, ToolCallID: lo.ToPtr("t2"), Content: model.MessageContent{Content: lo.ToPtr("r2")}},
		{Role: "user", MessageIndex: &idx, Content: model.MessageContent{Content: lo.ToPtr("extra user")}},
	}}
	got := convertMessages(req)
	if len(got) != 1 {
		t.Fatalf("len(convertMessages) = %d, want 1", len(got))
	}
	b, _ := json.Marshal(got)
	want := `[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"r1"},{"type":"tool_result","tool_use_id":"t2","content":"r2"},{"type":"text","text":"extra user"}]}]`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

// G4.3: contentToBlocks returns empty for both nil content and a non-nil empty pointer,
// but only non-empty content yields a block.
func TestContentToBlocksEmptySemantics(t *testing.T) {
	if got := contentToBlocks(anthropicModel.MessageContent{Content: lo.ToPtr("")}); len(got) != 0 {
		t.Errorf("contentToBlocks(empty ptr) len = %d, want 0", len(got))
	}
	if got := contentToBlocks(anthropicModel.MessageContent{}); len(got) != 0 {
		t.Errorf("contentToBlocks(zero) len = %d, want 0", len(got))
	}
	one := contentToBlocks(anthropicModel.MessageContent{MultipleContent: []anthropicModel.MessageContentBlock{{Type: "text", Text: lo.ToPtr("x")}}})
	if len(one) != 1 {
		t.Errorf("contentToBlocks(one block) len = %d, want 1", len(one))
	}
}

// G5: convertAssistantWithToolCalls emits thinking, then text, then tool_use blocks in a
// fixed order; invalid and empty JSON arguments are silently replaced with {}.
func TestConvertAssistantWithToolCalls(t *testing.T) {
	msg := model.Message{
		Role:               "assistant",
		Content:            model.MessageContent{Content: lo.ToPtr("thinking out loud")},
		ReasoningContent:   lo.ToPtr("secret reasoning"),
		ReasoningSignature: lo.ToPtr("sig"),
		ToolCalls: []model.ToolCall{
			{ID: "tc1", Function: model.FunctionCall{Name: "f1", Arguments: `{"a":1}`}},
			{ID: "tc2", Function: model.FunctionCall{Name: "f2", Arguments: `not json`}},
			{ID: "tc3", Function: model.FunctionCall{Name: "f3", Arguments: ``}},
		},
	}
	got := convertAssistantWithToolCalls(msg)
	b, _ := json.Marshal(got)
	want := `[{"role":"assistant","content":[{"type":"thinking","thinking":"secret reasoning","signature":"sig"},{"type":"text","text":"thinking out loud"},{"type":"tool_use","id":"tc1","name":"f1","input":{"a":1}},{"type":"tool_use","id":"tc2","name":"f2","input":{}},{"type":"tool_use","id":"tc3","name":"f3","input":{}}]}]`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

// G5: convertAssistantWithToolCalls returns nil when the assistant message has no content
// or tool calls (zero blocks).
func TestConvertAssistantWithToolCallsEmptyReturnsNil(t *testing.T) {
	if got := convertAssistantWithToolCalls(model.Message{Role: "assistant"}); got != nil {
		t.Errorf("convertAssistantWithToolCalls = %v, want nil", got)
	}
}

// G5: convertAssistantMessage routes assistant messages with tool calls to the tool-call
// path and otherwise emits a simple assistant message.
func TestConvertAssistantMessage(t *testing.T) {
	plain := convertAssistantMessage(model.Message{Role: "assistant", Content: model.MessageContent{Content: lo.ToPtr("hi")}})
	b, _ := json.Marshal(plain)
	if string(b) != `[{"role":"assistant","content":"hi"}]` {
		t.Errorf("plain = %s, want %s", string(b), `[{"role":"assistant","content":"hi"}]`)
	}
	withTool := convertAssistantMessage(model.Message{Role: "assistant", ToolCalls: []model.ToolCall{{ID: "tc", Function: model.FunctionCall{Name: "f", Arguments: `{}`}}}})
	b2, _ := json.Marshal(withTool)
	if !strings.Contains(string(b2), `"tool_use"`) {
		t.Errorf("withTool = %s, want a tool_use block", string(b2))
	}
}

// G4: convertSingleMessage routes each role; a user message whose index was already
// processed is dropped; unknown roles yield nil; a second tool message with an
// already-processed index is dropped (dedup).
func TestConvertSingleMessageRouting(t *testing.T) {
	user := convertSingleMessage(model.Message{Role: "user", Content: model.MessageContent{Content: lo.ToPtr("u")}}, nil, map[int]bool{})
	if len(user) != 1 || user[0].Role != "user" {
		t.Errorf("user = %+v, want one user message", user)
	}
	if got := convertSingleMessage(model.Message{Role: "bogus"}, nil, map[int]bool{}); got != nil {
		t.Errorf("bogus role = %+v, want nil", got)
	}
	idx := 0
	if got := convertSingleMessage(model.Message{Role: "user", MessageIndex: &idx}, nil, map[int]bool{0: true}); got != nil {
		t.Errorf("processed user = %+v, want nil", got)
	}
	req := &model.InternalLLMRequest{Messages: []model.Message{
		{Role: "tool", MessageIndex: &idx, ToolCallID: lo.ToPtr("t1"), Content: model.MessageContent{Content: lo.ToPtr("r1")}},
		{Role: "tool", MessageIndex: &idx, ToolCallID: lo.ToPtr("t2"), Content: model.MessageContent{Content: lo.ToPtr("r2")}},
	}}
	if got := convertToolMessage(req.Messages[1], req.Messages, map[int]bool{0: true}); got != nil {
		t.Errorf("processed tool = %+v, want nil", got)
	}
}

// G4: convertToolResultBlock builds a tool_result block from string, multiple-text-part,
// or absent content.
func TestConvertToolResultBlock(t *testing.T) {
	cases := []struct {
		name string
		msg  model.Message
		want string
	}{
		{"stringContent", model.Message{Role: "tool", ToolCallID: lo.ToPtr("t1"), Content: model.MessageContent{Content: lo.ToPtr("r1")}}, `{"type":"tool_result","tool_use_id":"t1","content":"r1"}`},
		{"multipleText", model.Message{Role: "tool", ToolCallID: lo.ToPtr("t1"), Content: model.MessageContent{MultipleContent: []model.MessageContentPart{
			{Type: "text", Text: lo.ToPtr("a")},
			{Type: "text", Text: lo.ToPtr("b")},
			{Type: "image_url", ImageURL: &model.ImageURL{URL: "https://x"}},
		}}}, `{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}`},
		{"noContent", model.Message{Role: "tool", ToolCallID: lo.ToPtr("t1")}, `{"type":"tool_result","tool_use_id":"t1"}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			b, _ := json.Marshal(convertToolResultBlock(tt.msg))
			if string(b) != tt.want {
				t.Errorf("json.Marshal = %s, want %s", string(b), tt.want)
			}
		})
	}
}

// G5: convertSingleMessage routes assistant role through convertAssistantMessage.
func TestConvertSingleMessageRoutesAssistant(t *testing.T) {
	// assistant without tool calls -> plain message
	got := convertSingleMessage(model.Message{Role: "assistant", Content: model.MessageContent{Content: lo.ToPtr("hi")}}, nil, map[int]bool{})
	if len(got) != 1 || got[0].Role != "assistant" {
		t.Errorf("assistant = %+v, want one assistant message", got)
	}
}

// G4: convertToolMessage with a MessageIndex but no matching tool message in the list
// yields nil (nothing to merge).
func TestConvertToolMessageIndexedNoMatchingToolInList(t *testing.T) {
	idx := 0
	req := &model.InternalLLMRequest{Messages: []model.Message{
		{Role: "tool", MessageIndex: &idx, ToolCallID: lo.ToPtr("t1"), Content: model.MessageContent{Content: lo.ToPtr("r1")}},
	}}
	// build a fresh list where the tool message is absent, so toolMsgs is empty
	got := convertToolMessage(req.Messages[0], []model.Message{{Role: "user", Content: model.MessageContent{Content: lo.ToPtr("u")}}}, map[int]bool{})
	if got != nil {
		t.Errorf("convertToolMessage = %+v, want nil", got)
	}
}

// G4: findUserMessageByIndex returns the user message sharing the given index.
func TestFindUserMessageByIndex(t *testing.T) {
	idx := 0
	msgs := []model.Message{
		{Role: "user", MessageIndex: &idx, Content: model.MessageContent{Content: lo.ToPtr("u")}},
		{Role: "user", Content: model.MessageContent{Content: lo.ToPtr("plain")}},
	}
	if got := findUserMessageByIndex(msgs, 0); got == nil || got.Content.Content == nil || *got.Content.Content != "u" {
		t.Errorf("findUserMessageByIndex = %+v, want the indexed user message", got)
	}
	if got := findUserMessageByIndex(msgs, 99); got != nil {
		t.Errorf("findUserMessageByIndex(99) = %+v, want nil", got)
	}
}

// G6.1: buildMessageContent with reasoning content routes through the multi-content
// builder, prepending a thinking block.
func TestBuildMessageContentWithReasoning(t *testing.T) {
	got := buildMessageContent(model.Message{Content: model.MessageContent{Content: lo.ToPtr("txt")}, ReasoningContent: lo.ToPtr("secret")})
	if got.Content != nil || len(got.MultipleContent) != 2 {
		t.Fatalf("Content=%v MultipleContent len=%d, want nil and 2", got.Content, len(got.MultipleContent))
	}
	b, _ := json.Marshal(got)
	want := `[{"type":"thinking","thinking":"secret"},{"type":"text","text":"txt"}]`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

// G6.3: convertMultiplePartContent also appends tool_calls when present, with the same
// invalid-JSON-becomes-{} rule as the assistant path.
func TestConvertMultiplePartContentWithToolCalls(t *testing.T) {
	got := convertMultiplePartContent(model.Message{Content: model.MessageContent{MultipleContent: []model.MessageContentPart{{Type: "text", Text: lo.ToPtr("t")}}},
		ToolCalls: []model.ToolCall{{ID: "tc", Function: model.FunctionCall{Name: "f", Arguments: `{"a":1}`}}, {ID: "tc2", Function: model.FunctionCall{Name: "g", Arguments: `bad`}}},
	})
	b, _ := json.Marshal(got)
	want := `[{"type":"text","text":"t"},{"type":"tool_use","id":"tc","name":"f","input":{"a":1}},{"type":"tool_use","id":"tc2","name":"g","input":{}}]`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

// G7: convertToAnthropicRequest carries metadata, tools, and stop sequences into the
// Anthropic request.
func TestConvertToAnthropicRequestMetadataToolsStop(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model:    "m",
		Metadata: map[string]string{"user_id": "u1"},
		Tools:    []model.Tool{{Type: "function", Function: model.Function{Name: "f"}}},
		Stop:     &model.Stop{Stop: lo.ToPtr("X")},
	}
	got := convertToAnthropicRequest(req)
	b, _ := json.Marshal(got)
	want := `{"max_tokens":8192,"messages":[],"model":"m","metadata":{"user_id":"u1"},"stop_sequences":["X"],"tools":[{"name":"f","description":"","input_schema":null}]}`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

// G4: convertMessages skips system-role messages entirely.
func TestConvertMessagesSkipsSystem(t *testing.T) {
	req := &model.InternalLLMRequest{Messages: []model.Message{
		{Role: "system", Content: model.MessageContent{Content: lo.ToPtr("sys")}},
		{Role: "user", Content: model.MessageContent{Content: lo.ToPtr("u")}},
	}}
	got := convertMessages(req)
	if len(got) != 1 || got[0].Role != "user" {
		t.Errorf("convertMessages = %+v, want only the user message", got)
	}
}

// G5: convertAssistantWithToolCalls with content only in MultipleContent emits text blocks
// from those parts.
func TestConvertAssistantWithToolCallsMultipleContentText(t *testing.T) {
	got := convertAssistantWithToolCalls(model.Message{Role: "assistant", Content: model.MessageContent{MultipleContent: []model.MessageContentPart{
		{Type: "text", Text: lo.ToPtr("a")},
		{Type: "text", Text: lo.ToPtr("b")},
		{Type: "image_url", ImageURL: &model.ImageURL{URL: "https://x"}},
	}}})
	b, _ := json.Marshal(got)
	want := `[{"role":"assistant","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}]`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

// G6.1: buildMessageContent routes MultipleContent through convertMultiplePartContent.
func TestBuildMessageContentMultipleContent(t *testing.T) {
	got := buildMessageContent(model.Message{Content: model.MessageContent{MultipleContent: []model.MessageContentPart{{Type: "text", Text: lo.ToPtr("t")}}}})
	if got.Content != nil || len(got.MultipleContent) != 1 {
		t.Errorf("Content=%v MultipleContent len=%d, want nil and 1", got.Content, len(got.MultipleContent))
	}
}

// G6.3: convertMultiplePartContent returns an empty MessageContent when no supported blocks
// remain.
func TestConvertMultiplePartContentEmptyResult(t *testing.T) {
	got := convertMultiplePartContent(model.Message{Content: model.MessageContent{MultipleContent: []model.MessageContentPart{{Type: "input_audio"}}}})
	if got.Content != nil || len(got.MultipleContent) != 0 {
		t.Errorf("Content=%v MultipleContent len=%d, want empty", got.Content, len(got.MultipleContent))
	}
}

// G6.1: buildMessageContent returns a bare string unless CacheControl is present, in which
// case it switches to an array form. Shape-level switch, assert JSON not just fields.
func TestBuildMessageContentBareString(t *testing.T) {
	got := buildMessageContent(model.Message{Content: model.MessageContent{Content: lo.ToPtr("txt")}})
	if got.Content == nil || len(got.MultipleContent) != 0 {
		t.Errorf("bare: Content=%v MultipleContent len=%d, want non-nil Content and 0 parts", got.Content, len(got.MultipleContent))
	}
	b, _ := json.Marshal(got)
	if string(b) != `"txt"` {
		t.Errorf("json.Marshal = %s, want %q", string(b), `"txt"`)
	}
}

func TestBuildMessageContentWithCacheControlBecomesArray(t *testing.T) {
	got := buildMessageContent(model.Message{Content: model.MessageContent{Content: lo.ToPtr("txt")}, CacheControl: &model.CacheControl{Type: "ephemeral"}})
	if got.Content != nil || len(got.MultipleContent) != 1 {
		t.Errorf("cached: Content=%v MultipleContent len=%d, want nil Content and 1 part", got.Content, len(got.MultipleContent))
	}
	b, _ := json.Marshal(got)
	want := `[{"type":"text","text":"txt","cache_control":{"type":"ephemeral"}}]`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

func TestBuildMessageContentZeroValue(t *testing.T) {
	got := buildMessageContent(model.Message{})
	if got.Content != nil || len(got.MultipleContent) != 0 {
		t.Errorf("zero: Content=%v MultipleContent len=%d, want nil and 0", got.Content, len(got.MultipleContent))
	}
}

// G6.2: convertImageURLToBlock dispatches data URLs to base64 source and http URLs to url
// source; nil ImageURL yields nil.
func TestConvertImageURLToBlockDataURL(t *testing.T) {
	got := convertImageURLToBlock(model.MessageContentPart{Type: "image_url", ImageURL: &model.ImageURL{URL: "data:image/png;base64,iVBORw0KGgo="}})
	b, _ := json.Marshal(got)
	want := `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}}`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

func TestConvertImageURLToBlockHTTPURL(t *testing.T) {
	got := convertImageURLToBlock(model.MessageContentPart{Type: "image_url", ImageURL: &model.ImageURL{URL: "https://x.test/a.png"}})
	b, _ := json.Marshal(got)
	want := `{"type":"image","source":{"type":"url","media_type":"","data":"","url":"https://x.test/a.png"}}`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

func TestConvertImageURLToBlockNil(t *testing.T) {
	if got := convertImageURLToBlock(model.MessageContentPart{Type: "image_url"}); got != nil {
		t.Errorf("convertImageURLToBlock = %v, want nil", got)
	}
}

// G6.3: convertMultiplePartContent keeps text and image_url parts, silently dropping
// unknown types (input_audio/file). current behavior: audio/file content is not sent.
func TestConvertMultiplePartContentDropsUnsupportedTypes(t *testing.T) {
	got := convertMultiplePartContent(model.Message{Content: model.MessageContent{MultipleContent: []model.MessageContentPart{
		{Type: "text", Text: lo.ToPtr("t")},
		{Type: "input_audio"},
		{Type: "image_url", ImageURL: &model.ImageURL{URL: "https://x.test/i.png"}},
	}}})
	if len(got.MultipleContent) != 2 {
		t.Fatalf("len(MultipleContent) = %d, want 2", len(got.MultipleContent))
	}
	b, _ := json.Marshal(got)
	want := `[{"type":"text","text":"t"},{"type":"image","source":{"type":"url","media_type":"","data":"","url":"https://x.test/i.png"}}]`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

// G7: convertTools keeps only function-typed tools and serializes a nil Parameters
// into input_schema:null.
func TestConvertToolsSkipsNonFunction(t *testing.T) {
	got := convertTools([]model.Tool{
		{Type: "function", Function: model.Function{Name: "ok", Description: "d"}},
		{Type: "web_search", Function: model.Function{Name: "skipme"}},
	})
	if len(got) != 1 {
		t.Fatalf("len(convertTools) = %d, want 1", len(got))
	}
	b, _ := json.Marshal(got)
	want := `[{"name":"ok","description":"d","input_schema":null}]`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

// G7: convertStopSequences maps stop variants to a slice, returning nil (not empty) when
// nothing is set.
func TestConvertStopSequences(t *testing.T) {
	cases := []struct {
		name string
		in   *model.Stop
		want []string
	}{
		{"nil", nil, nil},
		{"single", &model.Stop{Stop: lo.ToPtr("X")}, []string{"X"}},
		{"multiple", &model.Stop{MultipleStop: []string{"a", "b"}}, []string{"a", "b"}},
		{"emptyBoth", &model.Stop{}, nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := convertStopSequences(tt.in)
			if tt.want == nil {
				if got != nil {
					t.Errorf("convertStopSequences(%v) = %v, want nil", tt.in, got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("convertStopSequences(%v) = %v, want %v", tt.in, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("convertStopSequences(%v)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// G8.1: convertStopReason maps Anthropic stop reasons to OpenAI-compatible ones, passing
// unknown values through unchanged.
func TestConvertStopReason(t *testing.T) {
	cases := []struct {
		name string
		in   *string
		want *string
	}{
		{"endTurn", lo.ToPtr("end_turn"), lo.ToPtr("stop")},
		{"maxTokens", lo.ToPtr("max_tokens"), lo.ToPtr("length")},
		{"stopSequence", lo.ToPtr("stop_sequence"), lo.ToPtr("stop")},
		{"pauseTurn", lo.ToPtr("pause_turn"), lo.ToPtr("stop")},
		{"toolUse", lo.ToPtr("tool_use"), lo.ToPtr("tool_calls")},
		{"refusal", lo.ToPtr("refusal"), lo.ToPtr("content_filter")},
		{"unknownPassthrough", lo.ToPtr("weird_new_reason"), lo.ToPtr("weird_new_reason")},
		{"nil", nil, nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := convertStopReason(tt.in)
			if tt.want == nil {
				if got != nil {
					t.Errorf("convertStopReason(%v) = %v, want nil", tt.in, got)
				}
				return
			}
			if got == nil || *got != *tt.want {
				t.Errorf("convertStopReason(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// G8.2: convertAnthropicUsage totals all four token buckets but only sets PromptTokens to
// input tokens, so PromptTokens+CompletionTokens != TotalTokens when cache is present.
// current behavior: cache tokens count into the total but not into prompt tokens.
func TestConvertAnthropicUsageWithCache(t *testing.T) {
	got := convertAnthropicUsage(&anthropicModel.Usage{InputTokens: 10, OutputTokens: 3, CacheReadInputTokens: 5, CacheCreationInputTokens: 2})
	if got == nil {
		t.Fatal("convertAnthropicUsage = nil")
	}
	if got.PromptTokens != 10 || got.CompletionTokens != 3 || got.TotalTokens != 20 {
		t.Errorf("got prompt=%d completion=%d total=%d, want 10/3/20", got.PromptTokens, got.CompletionTokens, got.TotalTokens)
	}
	if !got.AnthropicUsage {
		t.Error("AnthropicUsage = false, want true")
	}
	if got.CacheCreationInputTokens != 2 {
		t.Errorf("CacheCreationInputTokens = %d, want 2", got.CacheCreationInputTokens)
	}
	if got.PromptTokensDetails == nil || got.PromptTokensDetails.CachedTokens != 5 {
		t.Errorf("PromptTokensDetails = %+v, want CachedTokens=5", got.PromptTokensDetails)
	}
}

// G8.2: without cache read tokens, PromptTokensDetails stays nil (not created).
func TestConvertAnthropicUsageNoCache(t *testing.T) {
	got := convertAnthropicUsage(&anthropicModel.Usage{InputTokens: 10, OutputTokens: 3})
	if got == nil {
		t.Fatal("convertAnthropicUsage = nil")
	}
	if got.TotalTokens != 13 {
		t.Errorf("TotalTokens = %d, want 13", got.TotalTokens)
	}
	if got.PromptTokensDetails != nil {
		t.Errorf("PromptTokensDetails = %+v, want nil", got.PromptTokensDetails)
	}
}

// G8.2: nil usage returns nil.
func TestConvertAnthropicUsageNil(t *testing.T) {
	if got := convertAnthropicUsage(nil); got != nil {
		t.Errorf("convertAnthropicUsage(nil) = %v, want nil", got)
	}
}

// G8.3: convertToLLMResponse converts multiple text blocks into a single concatenated
// string in a simple content field.
func TestConvertToLLMResponseJoinsTextBlocks(t *testing.T) {
	resp := convertToLLMResponse(&anthropicModel.Message{
		Content:    []anthropicModel.MessageContentBlock{{Type: "text", Text: lo.ToPtr("Hello ")}, {Type: "text", Text: lo.ToPtr("world")}},
		StopReason: lo.ToPtr("end_turn"),
	})
	msg := resp.Choices[0].Message
	if msg.Content.Content == nil || *msg.Content.Content != "Hello world" {
		t.Errorf("Content.Content = %v, want %q", msg.Content.Content, "Hello world")
	}
	if len(msg.Content.MultipleContent) != 0 {
		t.Errorf("len(MultipleContent) = %d, want 0", len(msg.Content.MultipleContent))
	}
	if resp.Choices[0].FinishReason == nil || *resp.Choices[0].FinishReason != "stop" {
		t.Errorf("FinishReason = %v, want stop", resp.Choices[0].FinishReason)
	}
}

// G8.3: convertToLLMResponse with a mixed block set keeps simple text content (despite
// tool_use blocks), extracts reasoning and tool calls, and leaves empty text blocks out.
func TestConvertToLLMResponseMixedBlocks(t *testing.T) {
	resp := convertToLLMResponse(&anthropicModel.Message{Content: []anthropicModel.MessageContentBlock{
		{Type: "text", Text: lo.ToPtr("t")},
		{Type: "thinking", Thinking: lo.ToPtr("th"), Signature: lo.ToPtr("sg")},
		{Type: "tool_use", ID: "tu1", Name: lo.ToPtr("fn"), Input: json.RawMessage(`{"k":1}`)},
		{Type: "tool_use", ID: "tu2", Name: lo.ToPtr("fn2")},
		{Type: "text", Text: lo.ToPtr("")},
	}})
	msg := resp.Choices[0].Message
	if msg.ReasoningContent == nil || *msg.ReasoningContent != "th" {
		t.Errorf("ReasoningContent = %v, want th", msg.ReasoningContent)
	}
	if msg.ReasoningSignature == nil || *msg.ReasoningSignature != "sg" {
		t.Errorf("ReasoningSignature = %v, want sg", msg.ReasoningSignature)
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("len(ToolCalls) = %d, want 2", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].ID != "tu1" || msg.ToolCalls[0].Function.Name != "fn" || msg.ToolCalls[0].Function.Arguments != `{"k":1}` {
		t.Errorf("ToolCalls[0] = %+v, want {tu1,fn,{\"k\":1}}", msg.ToolCalls[0])
	}
	if msg.ToolCalls[1].ID != "tu2" || msg.ToolCalls[1].Function.Arguments != "{}" {
		t.Errorf("ToolCalls[1] = %+v, want {tu2,{}}", msg.ToolCalls[1])
	}
	if msg.Content.Content == nil || *msg.Content.Content != "t" {
		t.Errorf("Content.Content = %v, want t", msg.Content.Content)
	}
	if len(msg.Content.MultipleContent) != 0 {
		t.Errorf("len(MultipleContent) = %d, want 0", len(msg.Content.MultipleContent))
	}
	if resp.Usage != nil {
		t.Errorf("Usage = %+v, want nil", resp.Usage)
	}
	if resp.Choices[0].FinishReason != nil {
		t.Errorf("FinishReason = %v, want nil", resp.Choices[0].FinishReason)
	}
}

// G8.3: convertToLLMResponse(nil) returns a non-nil zero response with the chat object type.
func TestConvertToLLMResponseNil(t *testing.T) {
	resp := convertToLLMResponse(nil)
	if resp == nil {
		t.Fatal("convertToLLMResponse(nil) = nil, want non-nil")
	}
	if resp.Object != "chat.completion" {
		t.Errorf("Object = %q, want chat.completion", resp.Object)
	}
	if len(resp.Choices) != 0 {
		t.Errorf("len(Choices) = %d, want 0", len(resp.Choices))
	}
}

// G8.4: TransformResponse handles nil response, empty body, structured 4xx errors, plain
// text 5xx errors, and body unmarshal failures.
func TestTransformResponse_Nil(t *testing.T) {
	o := &MessageOutbound{}
	_, err := o.TransformResponse(context.Background(), nil)
	if err == nil || err.Error() != "response is nil" {
		t.Errorf("err = %v, want %q", err, "response is nil")
	}
}

func TestTransformResponse_EmptyBody(t *testing.T) {
	o := &MessageOutbound{}
	_, err := o.TransformResponse(context.Background(), &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}})
	if err == nil || err.Error() != "response body is empty" {
		t.Errorf("err = %v, want %q", err, "response body is empty")
	}
}

func TestTransformResponse_Structured4xx(t *testing.T) {
	o := &MessageOutbound{}
	_, err := o.TransformResponse(context.Background(), &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"invalid_request_error","message":"bad thing"}}`)), Header: http.Header{}})
	var re *model.ResponseError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v, want *model.ResponseError", err)
	}
	if re.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", re.StatusCode)
	}
	if re.Detail.Message != "bad thing" {
		t.Errorf("Detail.Message = %q, want bad thing", re.Detail.Message)
	}
	if re.Detail.Type != "invalid_request_error" {
		t.Errorf("Detail.Type = %q, want invalid_request_error", re.Detail.Type)
	}
}

func TestTransformResponse_Plain5xx(t *testing.T) {
	o := &MessageOutbound{}
	_, err := o.TransformResponse(context.Background(), &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("oops")), Header: http.Header{}})
	if err == nil {
		t.Fatal("err = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "HTTP error 500") || !strings.Contains(err.Error(), "oops") {
		t.Errorf("err = %q, want to contain HTTP error 500 and oops", err.Error())
	}
}

func TestTransformResponse_Valid200(t *testing.T) {
	o := &MessageOutbound{}
	resp, err := o.TransformResponse(context.Background(), &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"model":"claude","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":2}}`)), Header: http.Header{}})
	if err != nil {
		t.Fatalf("TransformResponse err = %v", err)
	}
	if resp.Object != "chat.completion" {
		t.Errorf("Object = %q, want chat.completion", resp.Object)
	}
	if resp.Model != "claude" || resp.ID != "m" {
		t.Errorf("Model/ID = %q/%q, want claude/m", resp.Model, resp.ID)
	}
	if resp.Choices[0].Message.Content.Content == nil || *resp.Choices[0].Message.Content.Content != "hello" {
		t.Errorf("Content = %v, want hello", resp.Choices[0].Message.Content.Content)
	}
}

func TestTransformResponse_BadJSON200(t *testing.T) {
	o := &MessageOutbound{}
	_, err := o.TransformResponse(context.Background(), &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{oops")), Header: http.Header{}})
	if err == nil || !strings.Contains(err.Error(), "failed to unmarshal anthropic response") {
		t.Errorf("err = %v, want to contain failed to unmarshal anthropic response", err)
	}
}

// G8.4: a 4xx response with an empty error message degrades to a plain HTTP error, not a
// structured ResponseError.
func TestTransformResponse_4xxEmptyMessageDegrades(t *testing.T) {
	o := &MessageOutbound{}
	_, err := o.TransformResponse(context.Background(), &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"x","message":""}}`)), Header: http.Header{}})
	var re *model.ResponseError
	if errors.As(err, &re) {
		t.Errorf("err = %v, want plain (non-ResponseError) error", err)
	}
	if err == nil || !strings.Contains(err.Error(), "HTTP error 401") {
		t.Errorf("err = %v, want to contain HTTP error 401", err)
	}
}

// G9.1: TransformStream content_block_start with an unknown content block type yields
// nil,nil (content is only produced in delta events).
func TestTransformStreamContentBlockStartUnknownType(t *testing.T) {
	o := &MessageOutbound{}
	resp, err := o.TransformStream(context.Background(), []byte(`{"type":"content_block_start","index":0,"content_block":{"type":"weird_block"}}`))
	if resp != nil || err != nil {
		t.Errorf("TransformStream = %v, %v; want nil, nil", resp, err)
	}
}

// G9.1: TransformStream returns nil,nil for empty data and unknown/ping events, handles
// [DONE], and surfaces unmarshal errors.
func TestTransformStreamIsolatedEvents(t *testing.T) {
	cases := []struct {
		name string
		ev   string
	}{
		{"empty", ""},
		{"ping", `{"type":"ping"}`},
		{"contentBlockStop", `{"type":"content_block_stop"}`},
		{"unknownType", `{"type":"never_heard_of_it"}`},
		{"textStartHandledLater", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{"unknownDelta", `{"type":"content_block_delta","delta":{"type":"zzz_delta"}}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			o := &MessageOutbound{}
			resp, err := o.TransformStream(context.Background(), []byte(tt.ev))
			if resp != nil || err != nil {
				t.Errorf("TransformStream(%q) = %v, %v; want nil, nil", tt.ev, resp, err)
			}
		})
	}
}

func TestTransformStream_DONE(t *testing.T) {
	o := &MessageOutbound{}
	resp, err := o.TransformStream(context.Background(), []byte("[DONE]"))
	if err != nil {
		t.Fatalf("TransformStream err = %v", err)
	}
	if resp == nil || resp.Object != "[DONE]" {
		t.Errorf("resp = %+v, want Object [DONE]", resp)
	}
}

func TestTransformStream_BadJSON(t *testing.T) {
	o := &MessageOutbound{}
	_, err := o.TransformStream(context.Background(), []byte("{oops"))
	if err == nil || !strings.Contains(err.Error(), "failed to unmarshal stream event") {
		t.Errorf("err = %v, want to contain failed to unmarshal stream event", err)
	}
}

// G9.2: a full stream flow reuses one MessageOutbound; state (id, model, toolIndex,
// usage) accumulates across events and is emitted per chunk.
func TestTransformStreamFullFlow(t *testing.T) {
	o := &MessageOutbound{}
	events := []struct {
		name string
		ev   string
		want string
	}{
		{
			"messageStart",
			`{"type":"message_start","message":{"id":"msg_9","model":"claude-y","role":"assistant","usage":{"input_tokens":11,"output_tokens":0,"cache_read_input_tokens":4}}}`,
			`{"id":"msg_9","choices":[{"index":0,"delta":{"role":"assistant"}}],"object":"chat.completion.chunk","created":0,"model":"claude-y","usage":{"prompt_tokens":11,"completion_tokens":0,"total_tokens":15,"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":4,"image_tokens":0,"video_tokens":0},"completion_tokens_details":null}}`,
		},
		{
			"toolUseStart",
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu_1","name":"myfn"}}`,
			`{"id":"msg_9","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"id":"tu_1","type":"function","function":{"name":"myfn","arguments":""},"index":0}]}}],"object":"chat.completion.chunk","created":0,"model":"claude-y"}`,
		},
		{
			"textDelta",
			`{"type":"content_block_delta","delta":{"type":"text_delta","text":"chunk"}}`,
			`{"id":"msg_9","choices":[{"index":0,"delta":{"role":"assistant","content":"chunk"}}],"object":"chat.completion.chunk","created":0,"model":"claude-y"}`,
		},
		{
			"inputJsonDelta",
			`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"a\":"}}`,
			`{"id":"msg_9","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"id":"tu_1","type":"function","function":{"name":"","arguments":"{\"a\":"},"index":0}]}}],"object":"chat.completion.chunk","created":0,"model":"claude-y"}`,
		},
		{
			"thinkingDelta",
			`{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"hmm"}}`,
			`{"id":"msg_9","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"hmm"}}],"object":"chat.completion.chunk","created":0,"model":"claude-y"}`,
		},
		{
			"signatureDelta",
			`{"type":"content_block_delta","delta":{"type":"signature_delta","signature":"sig9"}}`,
			`{"id":"msg_9","choices":[{"index":0,"delta":{"role":"assistant","reasoning_signature":"sig9"}}],"object":"chat.completion.chunk","created":0,"model":"claude-y"}`,
		},
		{
			"messageDelta",
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":0,"output_tokens":7}}`,
			`{"id":"msg_9","choices":[{"index":0,"finish_reason":"tool_calls"}],"object":"chat.completion.chunk","created":0,"model":"claude-y"}`,
		},
		{
			"messageStop",
			`{"type":"message_stop"}`,
			`{"id":"msg_9","object":"chat.completion.chunk","created":0,"model":"claude-y","usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18,"prompt_tokens_details":null,"completion_tokens_details":null}}`,
		},
	}
	for _, tt := range events {
		resp, err := o.TransformStream(context.Background(), []byte(tt.ev))
		if err != nil {
			t.Fatalf("%s: TransformStream err = %v", tt.name, err)
		}
		b, _ := json.Marshal(resp)
		if string(b) != tt.want {
			t.Errorf("%s:\n got %s\nwant %s", tt.name, string(b), tt.want)
		}
	}
}

// G9.3: input_json_delta with no preceding tool_use (toolIndex -1) must not panic and must
// not emit tool_calls. Guards the nil-map deref path.
func TestTransformStreamInputJsonDeltaWithoutToolUse(t *testing.T) {
	o := &MessageOutbound{}
	resp, err := o.TransformStream(context.Background(), []byte(`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"x"}}`))
	if err != nil {
		t.Fatalf("TransformStream err = %v", err)
	}
	b, _ := json.Marshal(resp)
	want := `{"id":"","choices":[{"index":0,"delta":{"role":"assistant"}}],"object":"chat.completion.chunk","created":0,"model":""}`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

// G9.3: message_delta without stop_reason emits no choices but still updates instance usage.
func TestTransformStreamMessageDeltaNoStopReason(t *testing.T) {
	o := &MessageOutbound{}
	resp, err := o.TransformStream(context.Background(), []byte(`{"type":"message_delta","usage":{"input_tokens":0,"output_tokens":5,"cache_read_input_tokens":3}}`))
	if err != nil {
		t.Fatalf("TransformStream err = %v", err)
	}
	b, _ := json.Marshal(resp)
	want := `{"id":"","object":"chat.completion.chunk","created":0,"model":""}`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
	if o.streamUsage == nil || o.streamUsage.TotalTokens != 8 {
		t.Errorf("streamUsage.TotalTokens = %d, want 8", o.streamUsage.TotalTokens)
	}
}

// G9.3: message_stop after message_delta emits the accumulated (cache-aware) usage.
func TestTransformStreamMessageStopEmitsAccumulatedUsage(t *testing.T) {
	o := &MessageOutbound{}
	if _, err := o.TransformStream(context.Background(), []byte(`{"type":"message_delta","usage":{"input_tokens":0,"output_tokens":5,"cache_read_input_tokens":3}}`)); err != nil {
		t.Fatalf("message_delta err = %v", err)
	}
	resp, err := o.TransformStream(context.Background(), []byte(`{"type":"message_stop"}`))
	if err != nil {
		t.Fatalf("message_stop err = %v", err)
	}
	b, _ := json.Marshal(resp)
	want := `{"id":"","object":"chat.completion.chunk","created":0,"model":"","usage":{"prompt_tokens":0,"completion_tokens":5,"total_tokens":8,"prompt_tokens_details":{"audio_tokens":0,"cached_tokens":3,"image_tokens":0,"video_tokens":0},"completion_tokens_details":null}}`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}
