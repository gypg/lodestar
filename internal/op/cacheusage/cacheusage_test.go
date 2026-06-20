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
