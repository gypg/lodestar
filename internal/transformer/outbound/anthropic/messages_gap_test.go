package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/samber/lo"

	anthropicModel "github.com/gypg/lodestar/internal/transformer/inbound/anthropic"
	"github.com/gypg/lodestar/internal/transformer/model"
)

// This file closes gaps found by mutation-testing messages_test.go: behaviors
// that are real but that no existing assertion observed, so a regression in any
// of them would have shipped green. Each test names the mutation it kills.

// Kills "Temperature: nil" / "TopP: nil" / "Stream: nil": the scalar sampling
// params were never asserted, so silently dropping any of them stayed green.
func TestRequestScalarPassthrough(t *testing.T) {
	got := convertToAnthropicRequest(&model.InternalLLMRequest{
		Model:       "m",
		Temperature: lo.ToPtr(0.5),
		TopP:        lo.ToPtr(0.9),
		Stream:      lo.ToPtr(true),
	})
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal err = %v", err)
	}
	want := `{"max_tokens":8192,"messages":[],"model":"m","temperature":0.5,"top_p":0.9,"stream":true}`
	if string(b) != want {
		t.Errorf("json.Marshal =\n %s\nwant %s", string(b), want)
	}
}

// Kills `if req.Metadata != nil` (dropping the user_id emptiness check): a
// metadata map without a user_id key must not emit a metadata object.
func TestConvertToAnthropicRequestMetadataEmptyUserID(t *testing.T) {
	got := convertToAnthropicRequest(&model.InternalLLMRequest{
		Model:    "m",
		Metadata: map[string]string{"other": "v"},
	})
	if got.Metadata != nil {
		t.Errorf("Metadata = %+v, want nil", got.Metadata)
	}
	b, _ := json.Marshal(got)
	want := `{"max_tokens":8192,"messages":[],"model":"m"}`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

// Kills "IsError: nil": tool_result blocks must carry is_error through, or an
// errored tool result is reported to the model as a success.
func TestConvertToolResultBlockIsError(t *testing.T) {
	b, _ := json.Marshal(convertToolResultBlock(model.Message{
		Role:            "tool",
		ToolCallID:      lo.ToPtr("t1"),
		Content:         model.MessageContent{Content: lo.ToPtr("boom")},
		ToolCallIsError: lo.ToPtr(true),
	}))
	want := `{"type":"tool_result","tool_use_id":"t1","content":"boom","is_error":true}`
	if string(b) != want {
		t.Errorf("json.Marshal = %s, want %s", string(b), want)
	}
}

// Kills `return c.MultipleContent` (dropping the defensive copy). contentToBlocks
// documents that it returns a copy so a later append cannot corrupt the source;
// nothing asserted it. Uses spare capacity so an aliased return would be written
// into the source's backing array.
func TestContentToBlocksReturnsCopy(t *testing.T) {
	backing := make([]anthropicModel.MessageContentBlock, 1, 8)
	backing[0] = anthropicModel.MessageContentBlock{Type: "text", Text: lo.ToPtr("a")}
	src := anthropicModel.MessageContent{MultipleContent: backing}

	got := contentToBlocks(src)
	_ = append(got, anthropicModel.MessageContentBlock{Type: "text", Text: lo.ToPtr("INJECTED")})

	if len(src.MultipleContent) != 1 {
		t.Fatalf("len(src.MultipleContent) = %d, want 1", len(src.MultipleContent))
	}
	if aliased := src.MultipleContent[:2]; aliased[1].Text != nil && *aliased[1].Text == "INJECTED" {
		t.Error("contentToBlocks aliased the source slice; want a copy")
	}
}

// Kills `Role: ""`: the assistant role from the Anthropic response was never
// asserted on the converted message.
func TestConvertToLLMResponseCarriesRole(t *testing.T) {
	resp := convertToLLMResponse(&anthropicModel.Message{
		Role:    "assistant",
		Content: []anthropicModel.MessageContentBlock{{Type: "text", Text: lo.ToPtr("x")}},
	})
	if got := resp.Choices[0].Message.Role; got != "assistant" {
		t.Errorf("Message.Role = %q, want assistant", got)
	}
}

// Kills deleting `result.Usage = convertAnthropicUsage(resp.Usage)`: the existing
// suite only ever asserted Usage on a response that had none (expecting nil), so
// dropping usage propagation entirely stayed green. Billing depends on this.
func TestConvertToLLMResponsePropagatesUsage(t *testing.T) {
	resp := convertToLLMResponse(&anthropicModel.Message{
		Content: []anthropicModel.MessageContentBlock{{Type: "text", Text: lo.ToPtr("x")}},
		Usage:   &anthropicModel.Usage{InputTokens: 7, OutputTokens: 5},
	})
	if resp.Usage == nil {
		t.Fatal("Usage = nil, want propagated")
	}
	if resp.Usage.PromptTokens != 7 || resp.Usage.CompletionTokens != 5 || resp.Usage.TotalTokens != 12 {
		t.Errorf("Usage = prompt %d/completion %d/total %d, want 7/5/12",
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
	}
}

// Same propagation, through the full TransformResponse path.
func TestTransformResponseCarriesUsage(t *testing.T) {
	o := &MessageOutbound{}
	got, err := o.TransformResponse(context.Background(), &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"id":"m","role":"assistant","content":[{"type":"text","text":"h"}],"usage":{"input_tokens":1,"output_tokens":2}}`)),
		Header:     http.Header{},
	})
	if err != nil {
		t.Fatalf("TransformResponse err = %v", err)
	}
	if got.Usage == nil || got.Usage.TotalTokens != 3 {
		t.Fatalf("Usage = %+v, want TotalTokens=3", got.Usage)
	}
}

// Kills `if block.Name != nil` (dropping the ID emptiness check): a tool_use
// block with no ID must be skipped, not emitted as an ID-less tool call.
func TestConvertToLLMResponseSkipsToolUseWithoutID(t *testing.T) {
	resp := convertToLLMResponse(&anthropicModel.Message{Content: []anthropicModel.MessageContentBlock{
		{Type: "tool_use", ID: "", Name: lo.ToPtr("fn")},
	}})
	if got := resp.Choices[0].Message.ToolCalls; len(got) != 0 {
		t.Errorf("ToolCalls = %+v, want none", got)
	}
}

// Kills dropping CacheReadInputTokens from the message_start usage guard: a
// cache-heavy first event can report 0 input and 0 output with only cache_read
// set, and that usage must still be emitted rather than swallowed.
func TestTransformStreamMessageStartCacheOnlyUsage(t *testing.T) {
	o := &MessageOutbound{}
	got, err := o.TransformStream(context.Background(),
		[]byte(`{"type":"message_start","message":{"id":"i","model":"m","usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":4}}}`))
	if err != nil {
		t.Fatalf("TransformStream err = %v", err)
	}
	if got.Usage == nil {
		t.Fatal("Usage = nil, want cache-read-only usage emitted")
	}
	if got.Usage.TotalTokens != 4 {
		t.Errorf("Usage.TotalTokens = %d, want 4", got.Usage.TotalTokens)
	}
	if got.Usage.PromptTokensDetails == nil || got.Usage.PromptTokensDetails.CachedTokens != 4 {
		t.Errorf("PromptTokensDetails = %+v, want CachedTokens=4", got.Usage.PromptTokensDetails)
	}
}

// The three error branches the WO-003 receipt called unreachable. Each is
// reachable without touching messages.go, and together they take the package
// from 99.0% to 100%.

// A NaN float is not representable in JSON, so json.Marshal fails.
func TestTransformRequestMarshalFailure(t *testing.T) {
	o := &MessageOutbound{}
	_, err := o.TransformRequest(context.Background(),
		&model.InternalLLMRequest{Model: "m", Temperature: lo.ToPtr(math.NaN())},
		"https://api.anthropic.com/v1", "k")
	if err == nil || !strings.Contains(err.Error(), "failed to marshal anthropic request") {
		t.Errorf("err = %v, want to contain failed to marshal anthropic request", err)
	}
}

// http.NewRequestWithContext rejects a nil context.
func TestTransformRequestNewRequestFailure(t *testing.T) {
	o := &MessageOutbound{}
	//nolint:staticcheck // deliberately nil to exercise the error branch
	_, err := o.TransformRequest(nil, &model.InternalLLMRequest{Model: "m"},
		"https://api.anthropic.com/v1", "k")
	if err == nil || !strings.Contains(err.Error(), "failed to create request") {
		t.Errorf("err = %v, want to contain failed to create request", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

// A body whose Read fails exercises the io.ReadAll error branch.
func TestTransformResponseReadBodyFailure(t *testing.T) {
	o := &MessageOutbound{}
	_, err := o.TransformResponse(context.Background(), &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(failingReader{}),
		Header:     http.Header{},
	})
	if err == nil || !strings.Contains(err.Error(), "failed to read response body") {
		t.Errorf("err = %v, want to contain failed to read response body", err)
	}
}
