package billing

import (
	"context"
	"math"
	"testing"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/user"
)

// TestOverdraftIsRecordedAsDebtAndThenBlocked 钉死透支的处置方式。
//
// 之前的行为：闸门 middleware/auth.go:171 -> HasBalanceForKey 只要求 remaining > 0，
// 不比较"够不够这次的钱"；扣费走全有全无的 `WHERE quota >= amount`，不足时 0 行受影响、
// 只记一条 warning。于是"请求成本 > 剩余余额"时服务已交付（上游的钱是我们付的）、
// 余额分毫不动、used_quota 也不涨 —— 下次闸门照样放行，循环没有出口。
// 实测：$0.50 余额跑 5 次 $1 请求，实收 $0.00。
//
// 触发条件是"剩余 < 单次成本"，那是每个预付费账户的**终态**，不是边角。
//
// 现在的行为：用量结算记欠款（余额可为负），闸门随即拦住下一次请求。
// 单次事故的透支上限从"无限"降到"一次请求的成本"。
func TestOverdraftIsRecordedAsDebtAndThenBlocked(t *testing.T) {
	const startingBalance = 0.5
	const costPerRequest = 1.0

	uid, kid := initBillingTestDB(t, startingBalance)
	ctx := context.Background()

	// 第一次请求：余额为正，闸门放行；成本超过余额，记成欠款。
	if !HasBalanceForKey(kid, ctx) {
		t.Fatal("request 1: gate blocked a user with positive balance")
	}
	ChargeKey(kid, costPerRequest, ctx)

	rem, used, err := user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(rem-(startingBalance-costPerRequest)) > 1e-9 {
		t.Fatalf("balance = %.17g, want %.17g (the charge must be recorded as debt, not dropped)",
			rem, startingBalance-costPerRequest)
	}
	if math.Abs(used-costPerRequest) > 1e-9 {
		t.Fatalf("used_quota = %.17g, want %.17g (revenue reporting must see the spend)", used, costPerRequest)
	}

	// 第二次请求：余额已为负，闸门必须拦住 —— 这是循环的出口。
	if HasBalanceForKey(kid, ctx) {
		t.Fatal("request 2: gate passed on a negative balance — the overdraft loop is still open")
	}

	// 充值必须能清掉欠款并恢复服务。WO-017 起入账走漏斗。
	if err := user.MutateQuota(nil, uid, 2.0, user.LedgerEntry{Kind: model.LedgerKindTopupEpay}, ctx); err != nil {
		t.Fatal(err)
	}
	rem, _, err = user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(rem-1.5) > 1e-9 {
		t.Fatalf("balance after $2.00 top-up = %.17g, want 1.5 (debt must net off)", rem)
	}
	if !HasBalanceForKey(kid, ctx) {
		t.Fatal("gate still blocked after the top-up cleared the debt")
	}
}

// TestChargeKeyRejectsNonFiniteCost 钉死 NaN/±Inf 不得进入余额列。
//
// 旧的 `WHERE quota >= amount` 顺带挡住了 NaN —— SQL 里任何与 NaN 的比较都为假，
// 所以 0 行受影响。去掉那个守卫之后这层保护就没了，而 `quota - NaN` 会把余额写成 NaN：
// 之后 remaining > 0 恒为假（用户永久被锁），且再多充值也算不回来。必须显式拒绝。
//
// 上游成本这条路没有任何 NaN 防线（只有 billingexpr 表达式路径有，
// internal/pkg/billingexpr/run.go:111），所以这里是最后一道。
func TestChargeKeyRejectsNonFiniteCost(t *testing.T) {
	for _, tt := range []struct {
		name string
		cost float64
	}{
		{name: "NaN", cost: math.NaN()},
		{name: "+Inf", cost: math.Inf(1)},
		{name: "-Inf", cost: math.Inf(-1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			uid, kid := initBillingTestDB(t, 10.0)
			ctx := context.Background()

			ChargeKey(kid, tt.cost, ctx)

			rem, used, err := user.GetQuota(uid, ctx)
			if err != nil {
				t.Fatal(err)
			}
			if math.IsNaN(rem) || math.IsInf(rem, 0) {
				t.Fatalf("balance is now %v — a non-finite charge poisoned the column", rem)
			}
			if math.Abs(rem-10.0) > 1e-9 {
				t.Fatalf("balance = %.17g, want 10 unchanged", rem)
			}
			if math.Abs(used) > 1e-9 {
				t.Fatalf("used_quota = %.17g, want 0", used)
			}
			// 余额没被毒化，用户仍可正常请求。
			if !HasBalanceForKey(kid, ctx) {
				t.Fatal("gate blocked the user after a rejected non-finite charge")
			}
		})
	}
}
