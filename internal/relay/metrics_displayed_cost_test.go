package relay

/*
WO-040 ③：显示的成本 = 扣走的钱。

ChargeKeyWithExpr 在模型配了计费表达式时用表达式结果覆盖上游成本扣款，但
Save 的展示/记录三处（relay complete 日志、relayLog.Cost、per-key 累计统计
→ wallet getUsage）全部记录上游定价。配了表达式的模型，账单数字与真实扣款
对不上。

media 路径（media_relay.go）没有这个 bug：先算 mediaCost，relayLog.Cost /
stats.InputCost / 日志 / ChargeKey（直接收 mediaCost，不走 WithExpr 重算）
全部用同一个数。本文件把 chat 路径对齐到该形状。

⛔ 只钉"记录/展示哪个数"——表达式本身的定价语义不在范围。
*/

import (
	"math"
	"testing"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/relaylog"
	transmodel "github.com/gypg/lodestar/internal/transformer/model"
)

// TestDisplayedCostMatchesExpressionCharge：表达式固定费 $5 的模型、上游返回
// 正常 usage。本站无该模型的上游定价（GetLLMPrice=nil）→ 上游成本 0，实扣为
// 表达式 $5。修复前：余额扣 5（对），但 relayLog.Cost=0（错，显示 0 实扣 5），
// per-key stats 成本=0（错）。
func TestDisplayedCostMatchesExpressionCharge(t *testing.T) {
	uid, kid := initFake200CommercialDB(t, 10.0, "wo040-display-expr-model", "5")

	m := NewRelayMetrics(kid, "wo040-display-expr-model", "chat", "chat", "127.0.0.1", nil)
	m.SetInternalResponse(&transmodel.InternalLLMResponse{
		Choices: []transmodel.Choice{{
			Index:   0,
			Message: &transmodel.Message{Content: transmodel.MessageContent{Content: strPtr("answer")}},
		}},
		Usage: &transmodel.Usage{PromptTokens: 100, CompletionTokens: 20},
	}, "wo040-display-expr-model")
	m.Save(true, nil, nil)

	if rem := fake200Quota(t, uid); math.Abs(rem-5.0) > 1e-9 {
		t.Fatalf("balance = %.9f, want 5.0 (expression flat fee)", rem)
	}

	if m.Stats.InputCost+m.Stats.OutputCost < 5.0-1e-9 || m.Stats.InputCost+m.Stats.OutputCost > 5.0+1e-9 {
		t.Fatalf("displayed stats cost = %f, want the charged 5.0", m.Stats.InputCost+m.Stats.OutputCost)
	}

	// relayLog.Cost 必须等于实扣（RelayLogAdd 进内存缓存，落库是异步的）。
	cache, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	var logRow *model.RelayLog
	for i := range cache {
		if cache[i].RequestAPIKeyID == kid {
			logRow = &cache[i]
		}
	}
	lock.Unlock()
	if logRow == nil {
		t.Fatal("relay log entry missing from the in-memory cache")
	}
	if math.Abs(logRow.Cost-5.0) > 1e-9 {
		t.Fatalf("relayLog.Cost = %f, want 5.0 (the charged figure, not upstream pricing)", logRow.Cost)
	}
	if logRow.InputTokens != 100 || logRow.OutputTokens != 20 {
		t.Fatalf("relayLog tokens = %d/%d, want 100/20 (usage itself is still upstream-reported)", logRow.InputTokens, logRow.OutputTokens)
	}
}

// TestDisplayedCostUnchangedWithoutExpression 对照组：无表达式模型保持上游
// 定价口径（本站无定价 → 0 → 免单，既有行为）。
func TestDisplayedCostUnchangedWithoutExpression(t *testing.T) {
	uid, kid := initFake200CommercialDB(t, 10.0, "wo040-display-noexpr-model", "")

	m := NewRelayMetrics(kid, "wo040-display-noexpr-model", "chat", "chat", "127.0.0.1", nil)
	m.SetInternalResponse(&transmodel.InternalLLMResponse{
		Choices: []transmodel.Choice{{
			Index:   0,
			Message: &transmodel.Message{Content: transmodel.MessageContent{Content: strPtr("answer")}},
		}},
		Usage: &transmodel.Usage{PromptTokens: 100, CompletionTokens: 20},
	}, "wo040-display-noexpr-model")
	m.Save(true, nil, nil)

	if rem := fake200Quota(t, uid); math.Abs(rem-10.0) > 1e-9 {
		t.Fatalf("balance = %.9f, want 10.0 (no expression, no upstream price → unbilled)", rem)
	}
	if m.Stats.InputCost+m.Stats.OutputCost != 0 {
		t.Fatalf("displayed cost = %f, want 0 (upstream pricing has no entry for this model)", m.Stats.InputCost+m.Stats.OutputCost)
	}
}
