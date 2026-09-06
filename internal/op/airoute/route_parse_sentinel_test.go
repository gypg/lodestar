package airoute

import (
	"errors"
	"strings"
	"testing"
)

// TestNormalizeAIMessageContentEmptyUsesSentinel pins that empty model output
// surfaces as errAIRouteEmptyResult: route_http maps that sentinel to a
// retryable aiRouteCallError, which is what lets a single-service local-mode
// pool retry a transient empty answer instead of failing the whole batch.
func TestNormalizeAIMessageContentEmptyUsesSentinel(t *testing.T) {
	for name, input := range map[string]any{
		"empty string":        "   ",
		"empty content parts": []any{map[string]any{"text": "  "}},
		"unrecognized shape":  42,
	} {
		_, err := normalizeAIMessageContent(input)
		if err == nil {
			t.Fatalf("%s: want an error for empty output", name)
		}
		if !errors.Is(err, errAIRouteEmptyResult) {
			t.Fatalf("%s: error %v should wrap errAIRouteEmptyResult", name, err)
		}
		if !strings.Contains(err.Error(), "AI返回结果为空") {
			t.Fatalf("%s: error %q should carry the operator-facing message", name, err.Error())
		}
	}

	content, err := normalizeAIMessageContent("ok")
	if err != nil || content != "ok" {
		t.Fatalf("non-empty string should pass through, got %q err=%v", content, err)
	}
}
