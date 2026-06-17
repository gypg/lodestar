package tokenizer

import (
	"strings"

	"github.com/tiktoken-go/tokenizer/codec"
)

// modelCodec maps model name prefixes to the appropriate tokenizer codec.
// Models are matched by prefix to cover variants (e.g., "gpt-4o" matches "gpt-4o-2024-08-06").
func codecForModel(model string) *codec.Codec {
	lower := strings.ToLower(strings.TrimSpace(model))

	// o200k_base — GPT-4o, o1, o3, o4, and their variants
	if strings.HasPrefix(lower, "gpt-4o") || strings.HasPrefix(lower, "o1") ||
		strings.HasPrefix(lower, "o3") || strings.HasPrefix(lower, "o4") ||
		strings.HasPrefix(lower, "chatgpt-4o") {
		return codec.NewO200kBase()
	}

	// cl100k_base — GPT-4, GPT-3.5-turbo, text-embedding-ada-002
	if strings.HasPrefix(lower, "gpt-4") || strings.HasPrefix(lower, "gpt-3.5") ||
		strings.HasPrefix(lower, "text-embedding-ada") || strings.HasPrefix(lower, "text-embedding-3") {
		return codec.NewCl100kBase()
	}

	// p50k_edit — text-davinci-edit-001, code-davinci-edit-001
	if strings.HasPrefix(lower, "text-davinci-edit") || strings.HasPrefix(lower, "code-davinci-edit") {
		return codec.NewP50kEdit()
	}

	// r50k_base — original GPT-3 davinci models
	if strings.HasPrefix(lower, "davinci") || strings.HasPrefix(lower, "curie") ||
		strings.HasPrefix(lower, "babbage") || strings.HasPrefix(lower, "ada") ||
		strings.HasPrefix(lower, "text-similarity") || strings.HasPrefix(lower, "text-search") ||
		strings.HasPrefix(lower, "code-search") {
		return codec.NewR50kBase()
	}

	// p50k_base — code-cushman, code-davinci (Codex models)
	if strings.HasPrefix(lower, "code-cushman") || strings.HasPrefix(lower, "code-davinci") {
		return codec.NewP50kBase()
	}

	// Default: o200k_base for unknown models (most modern models use this encoding)
	return codec.NewO200kBase()
}

func CountTokens(content, model string) int {
	enc := codecForModel(model)
	tc, err := enc.Count(content)
	if err != nil {
		return 0
	}
	return tc
}