package model

import "testing"

// strPtr 复用 embedding_test.go 里已有的那个。

// 这三个分支正是三份副本漂移掉的东西：openai-chat 那份有，
// openai-responses 与 anthropic-messages 那两份没有。抽成一份共享实现后
// 三个 inbound adapter 都拿到它们，这里钉住的就是这个不变量。
func TestAggregateStreamChunks_AccumulatesTheBranchesThatUsedToBeDropped(t *testing.T) {
	chunks := []*InternalLLMResponse{
		{
			ID:    "resp-1",
			Model: "some-model",
			Choices: []Choice{{
				Index: 0,
				Delta: &Message{
					Role:    "assistant",
					Content: MessageContent{Content: strPtr("hello ")},
				},
			}},
		},
		{
			Choices: []Choice{{
				Index: 0,
				Delta: &Message{
					// 多模态输出（图片/音频）——曾被两份副本丢掉
					Content: MessageContent{MultipleContent: []MessageContentPart{{Type: "image_url"}}},
				},
			}},
		},
		{
			Choices: []Choice{{
				Index: 0,
				Delta: &Message{
					// Gemini 经 OpenAI 兼容端点做图片生成——曾被两份副本丢掉
					Images: []MessageContentPart{{Type: "image_url"}},
				},
			}},
		},
		{
			Choices: []Choice{{
				Index: 0,
				Delta: &Message{
					// ★ 用 `reasoning` 而不是 `reasoning_content`（OpenRouter / Ollama cloud 的拼法）。
					// 两份副本只测 ReasoningContent != nil，所以这种拼法整条丢失。
					Reasoning: strPtr("because "),
				},
			}},
		},
		{
			Choices: []Choice{{
				Index: 0,
				Delta: &Message{
					Content:          MessageContent{Content: strPtr("world")},
					ReasoningContent: strPtr("and also"),
				},
			}},
		},
	}

	got := AggregateStreamChunks(chunks)
	if got == nil {
		t.Fatal("AggregateStreamChunks() = nil, want an aggregate")
	}
	if len(got.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(got.Choices))
	}

	msg := got.Choices[0].Message
	if msg == nil {
		t.Fatal("Choices[0].Message = nil")
	}
	if msg.Content.Content == nil || *msg.Content.Content != "hello world" {
		t.Fatalf("text content = %v, want %q", msg.Content.Content, "hello world")
	}
	// 两个来源（Content.MultipleContent 与 Images）都必须落进 MultipleContent。
	if len(msg.Content.MultipleContent) != 2 {
		t.Fatalf("len(MultipleContent) = %d, want 2 (one from Content.MultipleContent, one from Images)", len(msg.Content.MultipleContent))
	}
	// `reasoning` 与 `reasoning_content` 都要被累加，且顺序按 chunk 顺序。
	if msg.ReasoningContent == nil || *msg.ReasoningContent != "because and also" {
		t.Fatalf("reasoning = %v, want %q — the `reasoning` spelling must count too", msg.ReasoningContent, "because and also")
	}
	if msg.Role != "assistant" {
		t.Fatalf("Role = %q, want assistant", msg.Role)
	}
}

func TestAggregateStreamChunks_EmptyInputYieldsNil(t *testing.T) {
	if got := AggregateStreamChunks(nil); got != nil {
		t.Fatalf("AggregateStreamChunks(nil) = %+v, want nil", got)
	}
	if got := AggregateStreamChunks([]*InternalLLMResponse{}); got != nil {
		t.Fatalf("AggregateStreamChunks(empty) = %+v, want nil", got)
	}
}

func TestAggregateStreamChunks_CarriesLateIDModelAndUsage(t *testing.T) {
	chunks := []*InternalLLMResponse{
		{Choices: []Choice{{Index: 0, Delta: &Message{Content: MessageContent{Content: strPtr("a")}}}}},
		// 有些上游只在后续 chunk 才给 ID / Model。
		{ID: "late-id", Model: "late-model"},
		// usage 取最后一个带它的 chunk。
		{Usage: &Usage{PromptTokens: 3, CompletionTokens: 4}},
		{Usage: &Usage{PromptTokens: 5, CompletionTokens: 6}},
	}

	got := AggregateStreamChunks(chunks)
	if got.ID != "late-id" {
		t.Fatalf("ID = %q, want late-id", got.ID)
	}
	if got.Model != "late-model" {
		t.Fatalf("Model = %q, want late-model", got.Model)
	}
	if got.Usage == nil || got.Usage.PromptTokens != 5 || got.Usage.CompletionTokens != 6 {
		t.Fatalf("Usage = %+v, want the last chunk's {5,6}", got.Usage)
	}
	if got.Object != "chat.completion" {
		t.Fatalf("Object = %q, want chat.completion", got.Object)
	}
}

func TestAggregateStreamChunks_MergesToolCallsByIndex(t *testing.T) {
	chunks := []*InternalLLMResponse{
		{Choices: []Choice{{Index: 0, Delta: &Message{ToolCalls: []ToolCall{{
			Index: 0, ID: "call-1", Type: "function",
			Function: FunctionCall{Name: "get_", Arguments: `{"a`},
		}}}}}},
		{Choices: []Choice{{Index: 0, Delta: &Message{ToolCalls: []ToolCall{{
			Index:    0,
			Function: FunctionCall{Name: "weather", Arguments: `":1}`},
		}}}}}},
		{Choices: []Choice{{Index: 0, Delta: &Message{ToolCalls: []ToolCall{{
			Index: 1, ID: "call-2", Function: FunctionCall{Name: "other"},
		}}}}}},
	}

	got := AggregateStreamChunks(chunks)
	calls := got.Choices[0].Message.ToolCalls
	if len(calls) != 2 {
		t.Fatalf("len(ToolCalls) = %d, want 2", len(calls))
	}
	if calls[0].Function.Name != "get_weather" {
		t.Fatalf("ToolCalls[0].Function.Name = %q, want get_weather", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"a":1}` {
		t.Fatalf("ToolCalls[0].Function.Arguments = %q, want %q", calls[0].Function.Arguments, `{"a":1}`)
	}
	// ID 与 Type 是覆盖而非累加，第二个 delta 没带就保留原值。
	if calls[0].ID != "call-1" || calls[0].Type != "function" {
		t.Fatalf("ToolCalls[0] lost its ID/Type: %+v", calls[0])
	}
}
