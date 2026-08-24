package openai

import (
	"context"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/transformer/model"
)

// 与 inbound/anthropic 的同名测试同理：上游可能用 `reasoning` 而非 `reasoning_content`
// （OpenRouter / Ollama cloud）。Responses 格式的这两条路径曾经裸判 ReasoningContent，
// 于是这种拼法的推理文本对 /v1/responses 客户端整条丢失。
func TestResponsesTransformStreamEmitsReasoningForTheAliasSpelling(t *testing.T) {
	reasoning := "deliberating"

	out, err := (&ResponseInbound{}).TransformStream(context.Background(), &model.InternalLLMResponse{
		ID:    "resp-1",
		Model: "gpt-test",
		Choices: []model.Choice{{
			Index: 0,
			Delta: &model.Message{
				Role:      "assistant",
				Reasoning: &reasoning, // ← 只给 `reasoning`
			},
		}},
	})
	if err != nil {
		t.Fatalf("TransformStream() error = %v", err)
	}

	if body := string(out); !strings.Contains(body, reasoning) {
		t.Fatalf("stream events do not carry the reasoning text %q:\n%s", reasoning, body)
	}
}

func TestConvertToResponsesAPIResponseEmitsReasoningForTheAliasSpelling(t *testing.T) {
	reasoning := "because of X"
	content := "answer"

	got := convertToResponsesAPIResponse(&model.InternalLLMResponse{
		ID:    "resp-1",
		Model: "gpt-test",
		Choices: []model.Choice{{
			Index: 0,
			Message: &model.Message{
				Role:      "assistant",
				Content:   model.MessageContent{Content: &content},
				Reasoning: &reasoning, // ← 只给 `reasoning`
			},
		}},
	})
	if got == nil {
		t.Fatal("convertToResponsesAPIResponse() = nil")
	}

	found := false
	for _, item := range got.Output {
		if item.Type != "reasoning" {
			continue
		}
		for _, summary := range item.Summary {
			if summary.Text == reasoning {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("no reasoning output item carries %q; the alias spelling was dropped: %+v", reasoning, got.Output)
	}
}
