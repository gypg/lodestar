package anthropic

import (
	"testing"

	"github.com/gypg/lodestar/internal/transformer/model"
	"github.com/samber/lo"
)

// budgetFromConvert drives the real call site and returns the emitted budget_tokens.
// The second result is false when no thinking block was emitted at all.
func budgetFromConvert(t *testing.T, req *model.InternalLLMRequest) (int64, bool) {
	t.Helper()

	got := convertToAnthropicRequest(req)
	if got.Thinking == nil || got.Thinking.BudgetTokens == nil {
		return 0, false
	}
	return *got.Thinking.BudgetTokens, true
}

// R-7: xhigh/max used to fall through getThinkingBudget's default and emit 8192 —
// *below* high's 32768, so asking for more reasoning bought less of it.
// max_tokens is set generously here so the clamp does not mask the mapping.
func TestConvertToAnthropicRequestExtendedEffortBudget(t *testing.T) {
	const roomyMaxTokens = 200000

	cases := []struct {
		name   string
		effort string
		want   int64
	}{
		{"low", "low", 1024},
		{"medium", "medium", 8192},
		{"high", "high", 32768},
		{"xhigh", "xhigh", 49152},
		{"max", "max", 65536},
		{"paddedMixedCaseXHigh", "  XHigh  ", 49152},
		{"paddedMixedCaseMax", "\tMAX\n", 65536},
		{"bogusFallsBackToDefault", "turbo", 8192},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := budgetFromConvert(t, &model.InternalLLMRequest{
				Model:           "claude-sonnet-4-5",
				ReasoningEffort: tt.effort,
				MaxTokens:       lo.ToPtr(int64(roomyMaxTokens)),
			})
			if !ok {
				t.Fatalf("thinking block absent for effort %q", tt.effort)
			}
			if got != tt.want {
				t.Fatalf("budget_tokens(%q) = %d, want %d", tt.effort, got, tt.want)
			}
		})
	}
}

// R-7 regression guard: the extended levels must stay strictly above high and must not
// collapse back onto the 8192 default, which is what made the downgrade invisible.
func TestConvertToAnthropicRequestExtendedEffortExceedsHigh(t *testing.T) {
	const roomyMaxTokens = 200000

	budget := func(effort string) int64 {
		got, ok := budgetFromConvert(t, &model.InternalLLMRequest{
			Model:           "claude-sonnet-4-5",
			ReasoningEffort: effort,
			MaxTokens:       lo.ToPtr(int64(roomyMaxTokens)),
		})
		if !ok {
			t.Fatalf("thinking block absent for effort %q", effort)
		}
		return got
	}

	high := budget("high")
	for _, effort := range []string{"xhigh", "max"} {
		got := budget(effort)
		if got == 8192 {
			t.Fatalf("effort %q fell back to the 8192 default; the reverse downgrade is back", effort)
		}
		if got <= high {
			t.Fatalf("budget_tokens(%q) = %d, want > high (%d)", effort, got, high)
		}
	}
}

// Anthropic rejects budget_tokens >= max_tokens with a 400 (thinking tokens count
// toward max_tokens, and we never send the interleaved-thinking beta header that
// would waive the rule). Raising the xhigh/max budgets is only safe because of this
// clamp — without it the fix would trade a silent downgrade for a hard failure.
func TestConvertToAnthropicRequestClampsThinkingBudget(t *testing.T) {
	cases := []struct {
		name      string
		effort    string
		maxTokens int64
		budget    *int64
		want      int64
	}{
		{"maxEffortClampedUnderSmallCeiling", "max", 16000, nil, 16000 - thinkingResponseReserve},
		{"highEffortClampedUnderDefaultCeiling", "high", 8192, nil, 8192 - thinkingResponseReserve},
		{"roomyCeilingLeavesBudgetIntact", "max", 200000, nil, 65536},
		{"explicitOversizedBudgetClamped", "low", 4096, lo.ToPtr(int64(99999)), 4096 - thinkingResponseReserve},
		{"explicitBudgetUnderCeilingKept", "low", 200000, lo.ToPtr(int64(4242)), 4242},
		{"tinyCeilingFloorsAtMinimum", "max", 1200, nil, minThinkingBudget},
		{"belowMinimumCeilingStaysUnderMaxTokens", "max", 700, nil, 699},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := budgetFromConvert(t, &model.InternalLLMRequest{
				Model:           "claude-sonnet-4-5",
				ReasoningEffort: tt.effort,
				ReasoningBudget: tt.budget,
				MaxTokens:       lo.ToPtr(tt.maxTokens),
			})
			if !ok {
				t.Fatalf("thinking block absent")
			}
			if got != tt.want {
				t.Fatalf("budget_tokens = %d, want %d", got, tt.want)
			}
		})
	}
}

// The strict budget_tokens < max_tokens invariant must hold for every effort level at
// every ceiling, including the pathological ones. A violation here is a 400 in prod.
func TestConvertToAnthropicRequestBudgetAlwaysBelowMaxTokens(t *testing.T) {
	efforts := []string{"low", "medium", "high", "xhigh", "max", "", "turbo"}
	ceilings := []int64{1, 2, 512, 1024, 1025, 4096, 8192, 16000, 64000, 200000}

	for _, effort := range efforts {
		for _, ceiling := range ceilings {
			got, ok := budgetFromConvert(t, &model.InternalLLMRequest{
				Model:           "claude-sonnet-4-5",
				ReasoningEffort: effort,
				MaxTokens:       lo.ToPtr(ceiling),
			})
			if effort == "" {
				if ok {
					t.Fatalf("effort %q emitted a thinking block, want none", effort)
				}
				continue
			}
			if !ok {
				t.Fatalf("thinking block absent for effort %q ceiling %d", effort, ceiling)
			}
			if got >= ceiling {
				t.Fatalf("budget_tokens = %d >= max_tokens = %d (effort %q): Anthropic rejects this with a 400",
					got, ceiling, effort)
			}
		}
	}
}

// Adaptive thinking carries effort in output_config and must never emit budget_tokens,
// so the clamp must not leak into that branch. xhigh/max have to survive verbatim.
func TestConvertToAnthropicRequestAdaptivePassesExtendedEffort(t *testing.T) {
	for _, effort := range []string{"high", "xhigh", "max"} {
		t.Run(effort, func(t *testing.T) {
			got := convertToAnthropicRequest(&model.InternalLLMRequest{
				Model:            "claude-opus-4-6",
				ReasoningEffort:  effort,
				AdaptiveThinking: true,
			})
			if got.Thinking == nil || got.Thinking.Type != "adaptive" {
				t.Fatalf("Thinking = %+v, want adaptive", got.Thinking)
			}
			if got.Thinking.BudgetTokens != nil {
				t.Fatalf("BudgetTokens = %d, want nil in adaptive mode", *got.Thinking.BudgetTokens)
			}
			if got.OutputConfig == nil || got.OutputConfig.Effort != effort {
				t.Fatalf("OutputConfig = %+v, want effort %q", got.OutputConfig, effort)
			}
		})
	}
}
