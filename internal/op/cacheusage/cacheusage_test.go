package cacheusage

import "testing"

// TestParseLodestarSemanticCacheHit 验证 lodestar.semantic_cache.hit 键能正确解析。
// 历史问题：OctopusCompat 字段曾与 Lodestar 字段共用 json tag "lodestar"，
// Go encoding/json 对同级重复 tag 会丢弃两个字段，导致该键永远解析不到。
func TestParseLodestarSemanticCacheHit(t *testing.T) {
	payload := `{
		"lodestar": {"semantic_cache": {"hit": true}},
		"usage": {"input_tokens": 100}
	}`
	signals, ok := ParseProviderPromptCacheUsageSignals(payload)
	if !ok {
		t.Fatalf("expected ok=true, got ok=false")
	}
	if !signals.SemanticCacheHit {
		t.Errorf("expected SemanticCacheHit=true, got false (lodestar key not parsed — duplicate json tag regression?)")
	}
	if signals.PromptTokens != 100 {
		t.Errorf("expected PromptTokens=100, got %d", signals.PromptTokens)
	}
}

func TestParseCachedTokens(t *testing.T) {
	payload := `{
		"usage": {
			"input_tokens": 200,
			"input_tokens_details": {"cached_tokens": 150}
		}
	}`
	signals, ok := ParseProviderPromptCacheUsageSignals(payload)
	if !ok {
		t.Fatalf("expected ok=true, got ok=false")
	}
	if signals.CachedTokens != 150 {
		t.Errorf("expected CachedTokens=150, got %d", signals.CachedTokens)
	}
}

// TestParseAnthropicUsageIsKeyPresenceNotValue 钉死：cache_creation_input_tokens
// 这个键**在不在**才是 Anthropic 用量语义的标记，它的**值**不是。
//
// Usage.AnthropicUsage 与 Usage.CacheCreationInputTokens 都是 json:"-"，
// relay/metrics.go 只在 AnthropicUsage 为真时才把这个键字符串拼进日志，
// 所以「键存在」等价于「prompt_tokens 不含缓存读」。暖前缀纯命中（只读、
// 不写新缓存）时该值恰好是 0，用「值 > 0」反推会把 Anthropic 误判成
// OpenAI 语义，缓存读 token 就再也加不回分母。
func TestParseAnthropicUsageIsKeyPresenceNotValue(t *testing.T) {
	warmReadOnly := `{"usage":{"prompt_tokens":10,"prompt_tokens_details":{"cached_tokens":50000},"cache_creation_input_tokens":0}}`
	signals, ok := ParseProviderPromptCacheUsageSignals(warmReadOnly)
	if !ok {
		t.Fatalf("expected ok=true, got ok=false")
	}
	if !signals.AnthropicUsage {
		t.Errorf("expected AnthropicUsage=true: the key is present, value 0 means a read-only hit not an absent key")
	}
	if signals.CacheCreationInputTokens != 0 {
		t.Errorf("expected CacheCreationInputTokens=0, got %d", signals.CacheCreationInputTokens)
	}
	if signals.CachedTokens != 50000 {
		t.Errorf("expected CachedTokens=50000, got %d", signals.CachedTokens)
	}

	openAIStyle := `{"usage":{"prompt_tokens":10,"prompt_tokens_details":{"cached_tokens":50000}}}`
	signals, ok = ParseProviderPromptCacheUsageSignals(openAIStyle)
	if !ok {
		t.Fatalf("expected ok=true, got ok=false")
	}
	if signals.AnthropicUsage {
		t.Errorf("expected AnthropicUsage=false when the cache_creation_input_tokens key is absent")
	}
}

func TestParseEmptyInvalid(t *testing.T) {
	if _, ok := ParseProviderPromptCacheUsageSignals(""); ok {
		t.Errorf("expected ok=false for empty input")
	}
	if _, ok := ParseProviderPromptCacheUsageSignals("not json"); ok {
		t.Errorf("expected ok=false for invalid json")
	}
	if _, ok := ParseProviderPromptCacheUsageSignals(`{"lodestar": {}}`); ok {
		t.Errorf("expected ok=false for payload without usage")
	}
}
