package relay

import (
	"math"
	"testing"

	transmodel "github.com/gypg/lodestar/internal/transformer/model"
)

// WO-045 阻断 1（上游 octopus #243）：负 cost 的真正来源不是负字段，而是
// 「cached > prompt 时 prompt-cached 为负」的减法结果。必须用**有价模型**
// （gpt-4o：Input=2.5, Output=10, CacheRead=1.25，presets.go:58），否则
// GetLLMPrice 返回 nil、cost 行整个不执行，测试测的是寂寞。
// 断言全部用死值（不是 >=0），防止「恒返回 0」的变异存活。

// cached 报得比 prompt 大 → uncached 段钳零，InputCost = 100×1.25×1e-6。
func TestSetInternalResponseNegativeCachedOverflowDoesNotFlipCostSign(t *testing.T) {
	m := NewRelayMetrics(1, "gpt-4o", "chat", "chat", "127.0.0.1", nil)

	m.SetInternalResponse(&transmodel.InternalLLMResponse{
		Choices: []transmodel.Choice{},
		Usage: &transmodel.Usage{
			PromptTokens:     10,
			CompletionTokens: 0,
			PromptTokensDetails: &transmodel.PromptTokensDetails{
				CachedTokens: 100,
			},
		},
	}, "gpt-4o")

	// 修复后：uncached = clamp(10-100)=0 → InputCost = (100×1.25 + 0×2.5)×1e-6 = 0.000125
	// 修复前（裸减法）：(100×1.25 + (10-100)×2.5)×1e-6 = -0.0001
	if m.Stats.InputCost < 0 || m.Stats.OutputCost < 0 {
		t.Fatalf("negative cost survived: input=%.9f output=%.9f — uncached subtraction must clamp", m.Stats.InputCost, m.Stats.OutputCost)
	}
	if math.Abs(m.Stats.InputCost-0.000125) > 1e-12 {
		t.Fatalf("InputCost = %.12f, want 0.000125 (100×CacheRead, uncached clamped to 0)", m.Stats.InputCost)
	}
	if m.Stats.OutputCost != 0 {
		t.Fatalf("OutputCost = %.9f, want 0", m.Stats.OutputCost)
	}
}

// 大规模污染（CC 探针第二路）：prompt=1, cached=1e9，修复前 InputCost ≈ -70。
func TestSetInternalResponseMassiveCachedOverflowStaysNonNegative(t *testing.T) {
	m := NewRelayMetrics(1, "gpt-4o", "chat", "chat", "127.0.0.1", nil)

	m.SetInternalResponse(&transmodel.InternalLLMResponse{
		Choices: []transmodel.Choice{},
		Usage: &transmodel.Usage{
			PromptTokens:     1,
			CompletionTokens: 0,
			PromptTokensDetails: &transmodel.PromptTokensDetails{
				CachedTokens: 1e9,
			},
		},
	}, "gpt-4o")

	if m.Stats.InputCost < 0 {
		t.Fatalf("InputCost = %.6f, want >= 0", m.Stats.InputCost)
	}
	// 死值：uncached=0 → InputCost = 1e9×1.25×1e-6 = 1250
	if math.Abs(m.Stats.InputCost-1250) > 1e-6 {
		t.Fatalf("InputCost = %.6f, want 1250 (1e9×CacheRead×1e-6)", m.Stats.InputCost)
	}
}

// 对照组：正常分布（cached ⊂ prompt）cost 逐分对得上公式；全零 usage cost 为 0。
func TestSetInternalResponseNormalSplitMatchesFormula(t *testing.T) {
	m := NewRelayMetrics(1, "gpt-4o", "chat", "chat", "127.0.0.1", nil)
	m.SetInternalResponse(&transmodel.InternalLLMResponse{
		Choices: []transmodel.Choice{},
		Usage: &transmodel.Usage{
			PromptTokens:     100,
			CompletionTokens: 50,
			PromptTokensDetails: &transmodel.PromptTokensDetails{
				CachedTokens: 10,
			},
		},
	}, "gpt-4o")
	// InputCost = (10×1.25 + 90×2.5)×1e-6 = 237.5e-6；OutputCost = 50×10×1e-6 = 500e-6
	if math.Abs(m.Stats.InputCost-0.0002375) > 1e-12 {
		t.Fatalf("InputCost = %.12f, want 0.0002375", m.Stats.InputCost)
	}
	if math.Abs(m.Stats.OutputCost-0.0005) > 1e-12 {
		t.Fatalf("OutputCost = %.12f, want 0.0005", m.Stats.OutputCost)
	}

	z := NewRelayMetrics(1, "gpt-4o", "chat", "chat", "127.0.0.1", nil)
	z.SetInternalResponse(&transmodel.InternalLLMResponse{
		Choices: []transmodel.Choice{},
		Usage:   &transmodel.Usage{},
	}, "gpt-4o")
	if z.Stats.InputCost != 0 || z.Stats.OutputCost != 0 {
		t.Fatalf("zero usage produced cost: %.9f/%.9f, want 0/0", z.Stats.InputCost, z.Stats.OutputCost)
	}
}

// 既有守卫保留：负字段直报（四路钳制），token 口径归零。改用有价模型，
// cost 行真实执行。
func TestSetInternalResponseClampsNegativeUsage(t *testing.T) {
	m := NewRelayMetrics(1, "gpt-4o", "chat", "chat", "127.0.0.1", nil)

	m.SetInternalResponse(&transmodel.InternalLLMResponse{
		Choices: []transmodel.Choice{},
		Usage: &transmodel.Usage{
			PromptTokens:     -1000000,
			CompletionTokens: -500,
		},
	}, "gpt-4o")

	if m.Stats.InputToken != 0 || m.Stats.OutputToken != 0 {
		t.Fatalf("negative usage leaked into stats: input=%d output=%d, want 0/0", m.Stats.InputToken, m.Stats.OutputToken)
	}
	if m.Stats.InputCost != 0 || m.Stats.OutputCost != 0 {
		t.Fatalf("negative cost computed: input=%.9f output=%.9f, want 0", m.Stats.InputCost, m.Stats.OutputCost)
	}
}

// 混合形态：正 prompt + 负 cached（部分字段被污染）不得让 cost 变负。
func TestSetInternalResponseClampsNegativeCachedTokens(t *testing.T) {
	m := NewRelayMetrics(1, "gpt-4o", "chat", "chat", "127.0.0.1", nil)

	m.SetInternalResponse(&transmodel.InternalLLMResponse{
		Choices: []transmodel.Choice{},
		Usage: &transmodel.Usage{
			PromptTokens:     100,
			CompletionTokens: 10,
			PromptTokensDetails: &transmodel.PromptTokensDetails{
				CachedTokens: -99999,
			},
		},
	}, "gpt-4o")

	// 修复后：cached 钳 0 → uncached = 100 → InputCost = 100×2.5×1e-6 = 0.00025
	if m.Stats.InputCost < 0 || m.Stats.OutputCost < 0 {
		t.Fatalf("negative cost from polluted cached tokens: input=%.9f output=%.9f", m.Stats.InputCost, m.Stats.OutputCost)
	}
	if math.Abs(m.Stats.InputCost-0.00025) > 1e-12 {
		t.Fatalf("InputCost = %.12f, want 0.00025 (cached clamped 0, uncached=100×Input)", m.Stats.InputCost)
	}
}
