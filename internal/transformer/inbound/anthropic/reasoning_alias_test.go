package anthropic

import (
	"context"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/transformer/model"
)

// 上游可能把推理文本放在 `reasoning` 而不是 `reasoning_content`
// （OpenRouter / Ollama cloud），`model.Message.GetReasoningContent()` 就是为此存在的。
// 这两条路径曾经裸判 ReasoningContent，于是这种拼法的推理文本对 /v1/messages
// 客户端整条丢失 —— 而 /v1/chat/completions 客户端不受影响，所以拿主端点测测不出来。
func TestTransformResponseEmitsThinkingForTheReasoningSpelling(t *testing.T) {
	reasoning := "step by step"
	content := "final answer"

	out, err := (&MessagesInbound{}).TransformResponse(context.Background(), &model.InternalLLMResponse{
		ID:    "resp-1",
		Model: "claude-test",
		Choices: []model.Choice{{
			Index: 0,
			Message: &model.Message{
				Role:      "assistant",
				Content:   model.MessageContent{Content: &content},
				Reasoning: &reasoning, // ← 只给 `reasoning`，不给 `reasoning_content`
			},
		}},
	})
	if err != nil {
		t.Fatalf("TransformResponse() error = %v", err)
	}

	body := string(out)
	if !strings.Contains(body, `"thinking"`) {
		t.Fatalf("response carries no thinking block; the `reasoning` spelling was dropped:\n%s", body)
	}
	if !strings.Contains(body, reasoning) {
		t.Fatalf("response does not contain the reasoning text %q:\n%s", reasoning, body)
	}
}

func TestTransformStreamEmitsThinkingDeltaForTheReasoningSpelling(t *testing.T) {
	reasoning := "thinking out loud"

	out, err := (&MessagesInbound{}).TransformStream(context.Background(), &model.InternalLLMResponse{
		ID:    "resp-1",
		Model: "claude-test",
		Choices: []model.Choice{{
			Index: 0,
			Delta: &model.Message{
				Role:      "assistant",
				Reasoning: &reasoning, // ← 同上
			},
		}},
	})
	if err != nil {
		t.Fatalf("TransformStream() error = %v", err)
	}

	body := string(out)
	if !strings.Contains(body, reasoning) {
		t.Fatalf("stream events do not carry the reasoning text %q:\n%s", reasoning, body)
	}
}
