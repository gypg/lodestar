package relay

import (
	"testing"

	"github.com/gypg/lodestar/internal/transformer/model"
)

// TestIsEmptyOutputResponse_Fake200Guard 验证假 200 守卫逻辑
func TestIsEmptyOutputResponse_Fake200Guard(t *testing.T) {
	tests := []struct {
		name     string
		resp     *model.InternalLLMResponse
		expected bool
	}{
		{
			name:     "nil response",
			resp:     nil,
			expected: false,
		},
		{
			name: "fake 200: both EmbeddingData and Choices empty",
			resp: &model.InternalLLMResponse{
				ID:            "fake-id",
				EmbeddingData: nil,
				Choices:       nil,
			},
			expected: true, // 假 200，必须返回 true
		},
		{
			name: "fake 200: both EmbeddingData and Choices empty slices",
			resp: &model.InternalLLMResponse{
				ID:            "fake-id",
				EmbeddingData: []model.EmbeddingObject{},
				Choices:       []model.Choice{},
			},
			expected: true, // 假 200，必须返回 true
		},
		{
			name: "valid embedding: EmbeddingData present, Choices empty",
			resp: &model.InternalLLMResponse{
				ID: "embedding-id",
				EmbeddingData: []model.EmbeddingObject{
					{
						Index: 0,
						Embedding: model.Embedding{
							FloatArray: []float64{0.1, 0.2, 0.3},
						},
					},
				},
				Choices: nil,
			},
			expected: false, // 合法 embedding，不应标记为空
		},
		{
			name: "valid LLM: Choices present with content",
			resp: &model.InternalLLMResponse{
				ID: "llm-id",
				Choices: []model.Choice{
					{
						Index: 0,
						Message: &model.Message{
							Content: model.MessageContent{
								Content: strPtr("Hello, world!"),
							},
						},
					},
				},
				EmbeddingData: nil,
				Usage: &model.Usage{
					CompletionTokens: 10,
				},
			},
			expected: false, // 有内容，不应标记为空
		},
		{
			name: "empty output: Choices present but all empty",
			resp: &model.InternalLLMResponse{
				ID: "empty-output-id",
				Choices: []model.Choice{
					{
						Index: 0,
						Message: &model.Message{
							Content: model.MessageContent{
								Content: strPtr(""),
							},
						},
					},
				},
				EmbeddingData: nil,
				Usage: &model.Usage{
					CompletionTokens: 0,
				},
			},
			expected: true, // 空输出，应标记为空
		},
		{
			name: "Choices with CompletionTokens > 0",
			resp: &model.InternalLLMResponse{
				ID: "tokens-id",
				Choices: []model.Choice{
					{
						Index: 0,
						Message: &model.Message{
							Content: model.MessageContent{
								Content: strPtr(""),
							},
						},
					},
				},
				Usage: &model.Usage{
					CompletionTokens: 5,
				},
			},
			expected: false, // 有 CompletionTokens，不应标记为空
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isEmptyOutputResponse(tt.resp)
			if got != tt.expected {
				t.Errorf("isEmptyOutputResponse() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestIsEmptyOutputResponse_Fake200Mutation 变异测试：验证假 200 守卫不能被移除
func TestIsEmptyOutputResponse_Fake200Mutation(t *testing.T) {
	// M1: 移除假 200 守卫 -> 必须导致此测试失败
	fake200 := &model.InternalLLMResponse{
		ID:            "fake-200",
		EmbeddingData: []model.EmbeddingObject{},
		Choices:       []model.Choice{},
	}
	if !isEmptyOutputResponse(fake200) {
		t.Error("M1: fake 200 guard removed - must return true for both-empty response")
	}

	// M2: 豁免逻辑被破坏 -> 必须导致此测试失败
	validEmbedding := &model.InternalLLMResponse{
		ID: "valid-embedding",
		EmbeddingData: []model.EmbeddingObject{
			{
				Index: 0,
				Embedding: model.Embedding{
					FloatArray: []float64{0.1, 0.2},
				},
			},
		},
		Choices: nil,
	}
	if isEmptyOutputResponse(validEmbedding) {
		t.Error("M2: embedding exemption broken - must return false for valid embedding")
	}

	// M3: 条件反转 (len == 0 改成 len > 0) -> 必须导致此测试失败
	fake200Both := &model.InternalLLMResponse{
		EmbeddingData: nil,
		Choices:       nil,
	}
	if !isEmptyOutputResponse(fake200Both) {
		t.Error("M3: condition inverted - must return true when both nil")
	}
}

func strPtr(s string) *string {
	return &s
}
