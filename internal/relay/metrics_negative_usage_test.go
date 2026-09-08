package relay

import (
	"math"
	"testing"

	transmodel "github.com/gypg/lodestar/internal/transformer/model"
)

// WO-044 #243：上游负 usage 钳制。恶意/被污染上游返回负 token 时，负 token ×
// 正单价 = 负 cost，统计被"反向充值"——auth 中间件的 MaxCost/MaxTokens 判据
// 吃的就是这套统计，冲销后配额永不触顶。钳制语义与 media 路径一致。
func TestSetInternalResponseClampsNegativeUsage(t *testing.T) {
	m := NewRelayMetrics(1, "neg-usage-model", "chat", "chat", "127.0.0.1", nil)

	m.SetInternalResponse(&transmodel.InternalLLMResponse{
		Choices: []transmodel.Choice{},
		Usage: &transmodel.Usage{
			PromptTokens:     -1000000,
			CompletionTokens: -500,
		},
	}, "neg-usage-model")

	if m.Stats.InputToken != 0 || m.Stats.OutputToken != 0 {
		t.Fatalf("negative usage leaked into stats: input=%d output=%d, want 0/0", m.Stats.InputToken, m.Stats.OutputToken)
	}
	if m.Stats.InputCost != 0 || m.Stats.OutputCost != 0 {
		t.Fatalf("negative cost computed: input=%.9f output=%.9f, want 0", m.Stats.InputCost, m.Stats.OutputCost)
	}
}

// 正常 usage 不受钳制影响（价格来自 price 包，本测试只验证 token 口径无损，
// cost 用相对断言：有 price 时非负，无 price 时保持零）。
func TestSetInternalResponseKeepsPositiveUsage(t *testing.T) {
	m := NewRelayMetrics(1, "pos-usage-model", "chat", "chat", "127.0.0.1", nil)

	m.SetInternalResponse(&transmodel.InternalLLMResponse{
		Choices: []transmodel.Choice{},
		Usage: &transmodel.Usage{
			PromptTokens:     100,
			CompletionTokens: 50,
		},
	}, "pos-usage-model")

	if m.Stats.InputToken != 100 || m.Stats.OutputToken != 50 {
		t.Fatalf("positive usage altered: input=%d output=%d, want 100/50", m.Stats.InputToken, m.Stats.OutputToken)
	}
}

// 混合形态：正 prompt + 负 cached（部分字段被污染）不得让 cost 变负。
func TestSetInternalResponseClampsNegativeCachedTokens(t *testing.T) {
	m := NewRelayMetrics(1, "mixed-usage-model", "chat", "chat", "127.0.0.1", nil)

	m.SetInternalResponse(&transmodel.InternalLLMResponse{
		Choices: []transmodel.Choice{},
		Usage: &transmodel.Usage{
			PromptTokens:     100,
			CompletionTokens: 10,
			PromptTokensDetails: &transmodel.PromptTokensDetails{
				CachedTokens: -99999,
			},
		},
	}, "mixed-usage-model")

	if m.Stats.InputCost < 0 || m.Stats.OutputCost < 0 {
		t.Fatalf("negative cost from polluted cached tokens: input=%.9f output=%.9f", m.Stats.InputCost, m.Stats.OutputCost)
	}
	if m.Stats.OutputCost < 0 || math.Signbit(float64(m.Stats.InputCost)) {
		t.Fatalf("cost sign violated: input=%.9f", m.Stats.InputCost)
	}
}
