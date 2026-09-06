package relay

/*
WO-040 ①：静默免单守卫。

缺陷链（CC 已实读核实）：上游返回 200 且带完整内容（Choices 非空）但没有 usage
对象时，SetInternalResponse 旧实现对 `resp.Usage == nil` 直接 return —— Stats 全 0；
isUnbillableFake200Response 抓不到它（那要求零载荷，IsFake200 对非空 Choices 为
false）；chargeable 保持 true，但 ChargeKey 对 cost<=0 提前返回（billing.go:102-104）。
⇒ 完整成功交付的答案被免费送出，且全程零告警。

本文件钉死三件事：
  1. 非零载荷 + Usage==nil 必须标记 usageMissing 并触发观测钩子（接线断言：
     生产 Handler 路径触达守卫，见 server 包外的 relay_usage_missing_wiring
     部分 / Save 前标记断言）；
  2. 零载荷 + Usage==nil 是真假 200，**不得**误标（它由既有 fake200 守卫按失败
     入账，不扣费也不交付）；
  3. usageMissing 的请求 Save(true) 后：免单决策显式化（余额不动、ChargeKey 仍
     被路由到 —— cost=0 的调用必须发生，不能绕开计费漏斗）。
*/

import (
	"testing"

	"github.com/gypg/lodestar/internal/op/billing"
	transmodel "github.com/gypg/lodestar/internal/transformer/model"
)

func deliverableRespWithoutUsage() *transmodel.InternalLLMResponse {
	return &transmodel.InternalLLMResponse{
		Choices: []transmodel.Choice{{
			Index:   0,
			Message: &transmodel.Message{Content: transmodel.MessageContent{Content: strPtr("a full answer the client received")}},
		}},
		// Usage: nil —— 缺陷场景的核心。
	}
}

// TestSetInternalResponseMarksUsageMissingOnDeliverable 守卫本体：有交付内容而
// usage 缺失时，usageMissing 必须置位、观测钩子必须触发、Stats 必须保持零值
// （不许估算，也不许静默当成 0 而无任何信号）。
func TestSetInternalResponseMarksUsageMissingOnDeliverable(t *testing.T) {
	observed := 0
	prev := UsageMissingObserver
	UsageMissingObserver = func(apiKeyID int, requestModel string) {
		observed++
		if apiKeyID != 424242 {
			t.Errorf("observer apiKeyID = %d, want 424242", apiKeyID)
		}
		if requestModel != "wo040-usage-missing" {
			t.Errorf("observer requestModel = %q, want wo040-usage-missing", requestModel)
		}
	}
	t.Cleanup(func() { UsageMissingObserver = prev })

	m := NewRelayMetrics(424242, "wo040-usage-missing", "chat", "chat", "127.0.0.1", nil)
	m.SetInternalResponse(deliverableRespWithoutUsage(), "wo040-usage-missing")

	if !m.usageMissing {
		t.Fatal("usageMissing = false, want true — a delivered response without usage must be explicitly marked")
	}
	if observed != 1 {
		t.Fatalf("usageMissingObserver fired %d times, want exactly 1", observed)
	}
	if m.Stats.InputToken != 0 || m.Stats.OutputToken != 0 || m.Stats.InputCost != 0 || m.Stats.OutputCost != 0 {
		t.Fatalf("stats must stay zero when usage is missing, got %+v", m.Stats)
	}
}

// TestSetInternalResponseSilentOnZeroPayload 反向守卫：零载荷 + 无 usage 是真假
// 200，归既有 fake200 守卫按失败入账——不得被本守卫误标为 usageMissing（否则
// 一次"未交付也不扣费"的失败会多出一条误导性的漏单告警）。
func TestSetInternalResponseSilentOnZeroPayload(t *testing.T) {
	observed := 0
	prev := UsageMissingObserver
	UsageMissingObserver = func(int, string) { observed++ }
	t.Cleanup(func() { UsageMissingObserver = prev })

	m := NewRelayMetrics(424243, "wo040-zero-payload", "chat", "chat", "127.0.0.1", nil)
	m.SetInternalResponse(&transmodel.InternalLLMResponse{}, "wo040-zero-payload")

	if m.usageMissing {
		t.Fatal("usageMissing = true for a zero-payload response, want false — fake 200s belong to the existing guard")
	}
	if observed != 0 {
		t.Fatalf("usageMissingObserver fired %d times for zero payload, want 0", observed)
	}
}

// TestMissingUsageSaveCompletesThroughBillingFunnel 钉住免单/按次两种决策的形状：
// usageMissing 的请求 Save(true) 后计费漏斗必须仍然被走到（CallRecorder 观察到
// 调用），即"usage 缺失"是计费路径上的显式输入，而不是绕开计费的旁路。
//   - 无表达式模型（生产站形态）：上游成本 0 → ChargeKey 对 0 免单，余额不动；
//   - 表达式（按次）模型：表达式对 0/0 输入的固定费照收 —— usage 缺失不得成为
//     跳过表达式计费的理由（那会是反向缺陷）。
func TestMissingUsageSaveCompletesThroughBillingFunnel(t *testing.T) {
	t.Run("no-expression model is unbilled", func(t *testing.T) {
		uid, kid := initFake200CommercialDB(t, 10.0, "wo040-missing-usage-model", "")

		var billedCostSum float64
		var calls int
		prevRecorder := billing.CallRecorder
		billing.CallRecorder = func(apiKeyID int, _ string, _, _ int, cost float64) {
			if apiKeyID == kid {
				calls++
				billedCostSum += cost
			}
		}
		t.Cleanup(func() { billing.CallRecorder = prevRecorder })

		m := NewRelayMetrics(kid, "wo040-missing-usage-model", "chat", "chat", "127.0.0.1", nil)
		m.SetInternalResponse(deliverableRespWithoutUsage(), "wo040-missing-usage-model")
		m.Save(true, nil, nil)

		if !m.usageMissing {
			t.Fatal("usageMissing lost before Save completed — the flag must survive until billing")
		}
		if calls == 0 {
			t.Fatal("ChargeKey was never reached — the unbilled decision must route through the billing funnel, not bypass it")
		}
		if billedCostSum != 0 {
			t.Fatalf("billed cost sum = %f, want 0 — a tokenless upstream cost must not invent a price", billedCostSum)
		}
		if rem := fake200Quota(t, uid); rem < 9.999999 || rem > 10.000001 {
			t.Fatalf("balance after missing-usage Save = %.9f, want ~10.0 (unbilled, explicitly decided)", rem)
		}
	})

	t.Run("expression model still charges its flat fee", func(t *testing.T) {
		uid, kid := initFake200CommercialDB(t, 10.0, "wo040-missing-usage-expr", "5")

		var billedCostSum float64
		prevRecorder := billing.CallRecorder
		billing.CallRecorder = func(apiKeyID int, _ string, _, _ int, cost float64) {
			if apiKeyID == kid {
				billedCostSum += cost
			}
		}
		t.Cleanup(func() { billing.CallRecorder = prevRecorder })

		m := NewRelayMetrics(kid, "wo040-missing-usage-expr", "chat", "chat", "127.0.0.1", nil)
		m.SetInternalResponse(deliverableRespWithoutUsage(), "wo040-missing-usage-expr")
		m.Save(true, nil, nil)

		if billedCostSum < 5.0-1e-9 || billedCostSum > 5.0+1e-9 {
			t.Fatalf("billed cost sum = %f, want 5.0 — usage-missing must not skip per-call expression billing", billedCostSum)
		}
		if rem := fake200Quota(t, uid); rem < 5.0-1e-9 || rem > 5.0+1e-9 {
			t.Fatalf("balance = %.9f, want ~5.0 — the flat per-call fee still applies without usage", rem)
		}
	})
}
