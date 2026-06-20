package relay

import (
	"testing"

	"github.com/gypg/lodestar/internal/transformer/model"
)

// TestExtractRequestText_StringContent 验证从纯字符串 content 提取文本。
func TestExtractRequestText_StringContent(t *testing.T) {
	s := "hello"
	req := &model.InternalLLMRequest{
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: &s}},
		},
	}
	if got := extractRequestText(req); got != "hello\n" {
		t.Errorf("string content: got %q, want %q", got, "hello\n")
	}
}

// TestExtractRequestText_MultipleContent 验证多部分内容只取 text 类型，跳过图片等。
func TestExtractRequestText_MultipleContent(t *testing.T) {
	t1, t2 := "part1", "part2"
	req := &model.InternalLLMRequest{
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{MultipleContent: []model.MessageContentPart{
				{Type: "text", Text: &t1},
				{Type: "image_url"},
				{Type: "text", Text: &t2},
			}}},
		},
	}
	if got := extractRequestText(req); got != "part1\npart2\n" {
		t.Errorf("multiple content: got %q, want %q", got, "part1\npart2\n")
	}
}

func TestExtractRequestText_MultiMessage(t *testing.T) {
	a, b := "first", "second"
	req := &model.InternalLLMRequest{
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: &a}},
			{Role: "assistant", Content: model.MessageContent{Content: &b}},
		},
	}
	got := extractRequestText(req)
	if got != "first\nsecond\n" {
		t.Errorf("multi-message: got %q, want %q", got, "first\nsecond\n")
	}
}

func TestExtractRequestText_NilAndEmpty(t *testing.T) {
	if got := extractRequestText(nil); got != "" {
		t.Errorf("nil req: got %q, want empty", got)
	}
	if got := extractRequestText(&model.InternalLLMRequest{}); got != "" {
		t.Errorf("empty req: got %q, want empty", got)
	}
}
