package relay

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/relay/balancer"
	transmodel "github.com/lingyuins/octopus/internal/transformer/model"
)

func TestFinalChannelFallsBackToSkippedAttempt(t *testing.T) {
	attempts := []model.ChannelAttempt{
		{
			ChannelID:   56,
			ChannelName: "test-channel",
			Status:      model.AttemptCircuitBreak,
			Msg:         "circuit breaker tripped",
		},
	}

	channelID, channelName := finalChannel(attempts)
	if channelID != 56 || channelName != "test-channel" {
		t.Fatalf("finalChannel() = (%d, %q), want (56, %q)", channelID, channelName, "test-channel")
	}
}

func TestFinalChannelPrefersLastForwardedFailure(t *testing.T) {
	attempts := []model.ChannelAttempt{
		{
			ChannelID:   11,
			ChannelName: "failed-channel",
			Status:      model.AttemptFailed,
		},
		{
			ChannelID:   56,
			ChannelName: "skipped-channel",
			Status:      model.AttemptCircuitBreak,
		},
	}

	channelID, channelName := finalChannel(attempts)
	if channelID != 11 || channelName != "failed-channel" {
		t.Fatalf("finalChannel() = (%d, %q), want (11, %q)", channelID, channelName, "failed-channel")
	}
}

func TestRouteIteratorAttemptsCarrySuccessfulChannel(t *testing.T) {
	iter := &balancer.Iterator{}
	span := iter.StartAttempt(23, 7, "mimo-channel", "Mimo-v2.5-pro-codeplan")
	span.End(model.AttemptSuccess, 200, "")

	channelID, channelName := finalChannel(iter.Attempts())
	if channelID != 23 || channelName != "mimo-channel" {
		t.Fatalf("finalChannel(iter.Attempts()) = (%d, %q), want (%d, %q)", channelID, channelName, 23, "mimo-channel")
	}
}

func TestInflightRelayResultCarriesAttemptsSnapshot(t *testing.T) {
	attempts := []model.ChannelAttempt{{ChannelID: 23, ChannelName: "mimo-channel", Status: model.AttemptSuccess}}
	result := newInflightRelayResult(nil, "mimo-v2.5", attempts, "", "")
	attempts[0].ChannelName = "mutated"

	channelID, channelName := finalChannel(result.attempts)
	if channelID != 23 || channelName != "mimo-channel" {
		t.Fatalf("finalChannel(result.attempts) = (%d, %q), want (%d, %q)", channelID, channelName, 23, "mimo-channel")
	}
}

func TestTruncateRelayLogStringPreservesUTF8(t *testing.T) {
	value := strings.Repeat("界", relayLogTextFieldMaxBytes)
	got := truncateRelayLogString(value, relayLogTextFieldMaxBytes)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateRelayLogString() returned invalid UTF-8")
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("truncateRelayLogString() = %q, want truncation marker", got)
	}
}

func TestFilterRequestForLogTruncatesLargeTextFields(t *testing.T) {
	longText := strings.Repeat("x", relayLogTextFieldMaxBytes+100)
	longReasoning := strings.Repeat("r", relayLogTextFieldMaxBytes+100)
	longArgs := strings.Repeat("a", relayLogTextFieldMaxBytes+100)
	longPart := strings.Repeat("p", relayLogTextFieldMaxBytes+100)
	longEmbedding := strings.Repeat("e", relayLogTextFieldMaxBytes+100)
	longToolDescription := strings.Repeat("d", relayLogTextFieldMaxBytes+100)
	longToolParams := json.RawMessage(`{"type":"object","description":"` + strings.Repeat("s", relayLogJSONFieldMaxBytes+100) + `"}`)
	stream := true
	req := &transmodel.InternalLLMRequest{
		Model:  "gpt-4o",
		Stream: &stream,
		Messages: []transmodel.Message{{
			Role:             "assistant",
			Content:          transmodel.MessageContent{Content: &longText, MultipleContent: []transmodel.MessageContentPart{{Type: "text", Text: &longPart}}},
			ReasoningContent: &longReasoning,
			ToolCalls: []transmodel.ToolCall{{
				ID:       "call-1",
				Type:     "function",
				Function: transmodel.FunctionCall{Name: "search", Arguments: longArgs},
			}},
		}},
		EmbeddingInput: &transmodel.EmbeddingInput{Single: &longEmbedding, Multiple: []string{longEmbedding}},
		Tools: []transmodel.Tool{{
			Type: "function",
			Function: transmodel.Function{
				Name:        "search",
				Description: longToolDescription,
				Parameters:  longToolParams,
			},
		}},
		ExtraBody:  json.RawMessage(`{"large":"body"}`),
		RawRequest: []byte(`{"raw":"request"}`),
	}

	filtered := (&RelayMetrics{}).filterRequestForLog(req)

	if got := *filtered.Messages[0].Content.Content; len(got) <= relayLogTextFieldMaxBytes || !strings.Contains(got, "truncated") {
		t.Fatalf("message content was not truncated: len=%d value=%q", len(got), got)
	}
	if got := *filtered.Messages[0].ReasoningContent; len(got) <= relayLogTextFieldMaxBytes || !strings.Contains(got, "truncated") {
		t.Fatalf("reasoning content was not truncated: len=%d value=%q", len(got), got)
	}
	if got := filtered.Messages[0].ToolCalls[0].Function.Arguments; len(got) <= relayLogTextFieldMaxBytes || !strings.Contains(got, "truncated") {
		t.Fatalf("tool call arguments were not truncated: len=%d value=%q", len(got), got)
	}
	if got := *filtered.Messages[0].Content.MultipleContent[0].Text; len(got) <= relayLogTextFieldMaxBytes || !strings.Contains(got, "truncated") {
		t.Fatalf("content part text was not truncated: len=%d value=%q", len(got), got)
	}
	if got := *filtered.EmbeddingInput.Single; len(got) <= relayLogTextFieldMaxBytes || !strings.Contains(got, "truncated") {
		t.Fatalf("embedding single was not truncated: len=%d value=%q", len(got), got)
	}
	if got := filtered.EmbeddingInput.Multiple[0]; len(got) <= relayLogTextFieldMaxBytes || !strings.Contains(got, "truncated") {
		t.Fatalf("embedding multiple was not truncated: len=%d value=%q", len(got), got)
	}
	if got := filtered.Tools[0].Function.Description; len(got) <= relayLogTextFieldMaxBytes || !strings.Contains(got, "truncated") {
		t.Fatalf("tool description was not truncated: len=%d value=%q", len(got), got)
	}
	if len(filtered.Tools[0].Function.Parameters) > relayLogJSONFieldMaxBytes+128 {
		t.Fatalf("tool parameters were not bounded: len=%d", len(filtered.Tools[0].Function.Parameters))
	}
	if len(filtered.ExtraBody) != 0 || len(filtered.RawRequest) != 0 {
		t.Fatalf("log filter should clear ExtraBody and RawRequest")
	}
	if *req.Messages[0].Content.Content != longText || req.Messages[0].ToolCalls[0].Function.Arguments != longArgs {
		t.Fatalf("filterRequestForLog mutated original request")
	}
	if req.EmbeddingInput.Multiple[0] != longEmbedding {
		t.Fatalf("filterRequestForLog mutated original embedding input")
	}
}
