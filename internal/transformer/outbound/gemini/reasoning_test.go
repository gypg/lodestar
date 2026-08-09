package gemini

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/gypg/lodestar/internal/transformer/model"
)

// thinkingBudgetFromTransformRequest drives the real outbound call site and reads the
// thinking budget back off the serialized wire body, so the assertion covers
// convertLLMToGeminiRequest wiring rather than reasoningToThinkingBudget in isolation.
func thinkingBudgetFromTransformRequest(t *testing.T, effort string) (int32, bool) {
	t.Helper()

	outbound := &MessagesOutbound{}
	httpReq, err := outbound.TransformRequest(
		context.Background(),
		&model.InternalLLMRequest{Model: "gemini-3-pro", ReasoningEffort: effort},
		"https://generativelanguage.googleapis.com/v1beta",
		"test-key",
	)
	if err != nil {
		t.Fatalf("TransformRequest(effort=%q) err = %v", effort, err)
	}

	body, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("read body err = %v", err)
	}

	var parsed struct {
		GenerationConfig *struct {
			ThinkingConfig *struct {
				ThinkingBudget *int32 `json:"thinkingBudget"`
			} `json:"thinkingConfig"`
		} `json:"generationConfig"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal body %s err = %v", string(body), err)
	}

	if parsed.GenerationConfig == nil ||
		parsed.GenerationConfig.ThinkingConfig == nil ||
		parsed.GenerationConfig.ThinkingConfig.ThinkingBudget == nil {
		return 0, false
	}
	return *parsed.GenerationConfig.ThinkingConfig.ThinkingBudget, true
}

// R-7: xhigh/max previously fell through to the -1 "dynamic thinking" default, which
// lets Gemini pick any budget it likes — the caller's top-tier request was discarded.
// The mapping must now be strictly increasing across low < medium < high < xhigh < max.
func TestTransformRequestThinkingBudgetExtendedEffort(t *testing.T) {
	cases := []struct {
		name   string
		effort string
		want   int32
	}{
		{"low", "low", 1024},
		{"medium", "medium", 4096},
		{"high", "high", 24576},
		{"xhigh", "xhigh", 49152},
		{"max", "max", 65536},
		{"paddedMixedCaseXHigh", "  XHigh  ", 49152},
		{"paddedMixedCaseMax", "\tMAX\n", 65536},
		{"bogusFallsBackToDynamic", "turbo", -1},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := thinkingBudgetFromTransformRequest(t, tt.effort)
			if !ok {
				t.Fatalf("thinkingBudget absent for effort %q, want %d", tt.effort, tt.want)
			}
			if got != tt.want {
				t.Fatalf("thinkingBudget(%q) = %d, want %d", tt.effort, got, tt.want)
			}
		})
	}
}

// R-7 regression guard: the extended levels must be strictly above high and must never
// return to the -1 dynamic sentinel, which is what made the downgrade silent.
func TestTransformRequestExtendedEffortExceedsHigh(t *testing.T) {
	high, ok := thinkingBudgetFromTransformRequest(t, "high")
	if !ok {
		t.Fatal("thinkingBudget absent for high")
	}

	for _, effort := range []string{"xhigh", "max"} {
		got, ok := thinkingBudgetFromTransformRequest(t, effort)
		if !ok {
			t.Fatalf("thinkingBudget absent for effort %q", effort)
		}
		if got == -1 {
			t.Fatalf("effort %q mapped back to dynamic thinking (-1); the silent downgrade is back", effort)
		}
		if got <= high {
			t.Fatalf("thinkingBudget(%q) = %d, want > high (%d)", effort, got, high)
		}
	}
}

// R-7: an empty effort must leave thinkingConfig off the request entirely rather than
// pinning a budget the caller never asked for.
func TestTransformRequestNoEffortOmitsThinkingConfig(t *testing.T) {
	if _, ok := thinkingBudgetFromTransformRequest(t, ""); ok {
		t.Fatal("thinkingBudget present for empty effort, want absent")
	}
}
