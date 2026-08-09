package openai

import (
	"encoding/json"
	"testing"

	"github.com/gypg/lodestar/internal/transformer/model"
)

// R-7: extended effort levels must reach OpenAI-compatible upstreams verbatim.
// Before the fix, xhigh/max were rewritten to "high" and minimal was dropped to "",
// so a caller paying for the top tier silently received a lower one.
// These expectations are asserted through the real call sites
// (ConvertToResponsesRequest and SanitizeRequestForOpenAICompat), not the helper alone.
func TestConvertToResponsesRequestPreservesExtendedEffort(t *testing.T) {
	cases := []struct {
		name   string
		effort string
		want   string // "" means the reasoning block must be absent entirely
	}{
		{"minimal", "minimal", "minimal"},
		{"low", "low", "low"},
		{"medium", "medium", "medium"},
		{"high", "high", "high"},
		{"xhigh", "xhigh", "xhigh"},
		{"max", "max", "max"},
		{"paddedMixedCaseXHigh", "  XHigh  ", "xhigh"},
		{"paddedMixedCaseMax", "\tMAX\n", "max"},
		{"none", "none", ""},
		{"empty", "", ""},
		{"bogus", "turbo", ""},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertToResponsesRequest(&model.InternalLLMRequest{
				Model:           "gpt-5",
				ReasoningEffort: tt.effort,
			})

			if tt.want == "" {
				if got.Reasoning != nil {
					t.Fatalf("Reasoning = %+v, want nil for effort %q", got.Reasoning, tt.effort)
				}
				return
			}

			if got.Reasoning == nil {
				t.Fatalf("Reasoning = nil, want effort %q", tt.want)
			}
			if got.Reasoning.Effort != tt.want {
				t.Fatalf("Reasoning.Effort = %q, want %q", got.Reasoning.Effort, tt.want)
			}
		})
	}
}

// R-7 regression guard: xhigh/max must not collapse onto high. If a future change
// reintroduces the downgrade, these two requests become indistinguishable on the wire.
func TestConvertToResponsesRequestExtendedEffortDistinctFromHigh(t *testing.T) {
	marshal := func(effort string) string {
		b, err := json.Marshal(ConvertToResponsesRequest(&model.InternalLLMRequest{
			Model:           "gpt-5",
			ReasoningEffort: effort,
		}))
		if err != nil {
			t.Fatalf("json.Marshal(%q) err = %v", effort, err)
		}
		return string(b)
	}

	high := marshal("high")
	for _, effort := range []string{"xhigh", "max", "minimal"} {
		if got := marshal(effort); got == high {
			t.Fatalf("effort %q serialized identically to high (%s); the downgrade is back", effort, got)
		}
	}
}

// R-7: the chat.go sanitize path shares the same normalizer. A non-DeepSeek/Mimo
// target must keep xhigh/max on the request instead of rewriting them to high.
func TestSanitizeRequestForOpenAICompatPreservesExtendedEffort(t *testing.T) {
	cases := []struct {
		name   string
		effort string
		want   string
	}{
		{"xhigh", "xhigh", "xhigh"},
		{"max", "max", "max"},
		{"minimal", "minimal", "minimal"},
		{"high", "high", "high"},
		{"none", "none", ""},
		{"bogus", "turbo", ""},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			req := &model.InternalLLMRequest{Model: "gpt-5", ReasoningEffort: tt.effort}
			SanitizeRequestForOpenAICompat(req, "https://api.openai.com/v1", false)
			if req.ReasoningEffort != tt.want {
				t.Fatalf("ReasoningEffort = %q, want %q", req.ReasoningEffort, tt.want)
			}
		})
	}
}
