package user

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

// ---------------------------------------------------------------------------
// WO-009 §2.1 — Concurrency: 10 goroutines compete on balance=1.0, deduct 0.3
// ---------------------------------------------------------------------------

func TestSettleUsage_concurrent(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "conc1", Password: "x", Quota: 1.0}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := SettleUsage(u.ID, 0.3, ctx); err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	rem, used, err := GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}

	// 每一笔已交付的用量都必须落账 —— 结算不再因余额不足而丢弃。
	// 这条测试从前断言"最多 3 笔成功、余额不得为负"，那正是无限白嫖的来源：
	// 被丢弃的 7 笔上游成本是我们付的，却既不扣余额也不涨 used_quota。
	if successCount != 10 {
		t.Errorf("successCount = %d, want 10 (no delivered charge may be dropped)", successCount)
	}
	// 无丢失更新：10 次并发各减 0.3，靠单条原子 UPDATE（gorm.Expr）而非读-改-写。
	// 少于 10 笔即说明有更新被覆盖。
	if math.Abs(used-3.0) > 1e-9 {
		t.Errorf("used = %.17g, want 3.0 (10 concurrent × 0.3, no lost updates)", used)
	}
	if math.Abs(rem-(-2.0)) > 1e-9 {
		t.Errorf("rem = %.17g, want -2.0 (1.0 balance minus 3.0 settled = debt)", rem)
	}
	// 账目守恒：rem + used == 起始余额。丢弃行为会破坏这一条。
	if math.Abs(rem+used-1.0) > 1e-9 {
		t.Errorf("account not conserved: rem=%.17g used=%.17g sum=%.17g (want 1.0)", rem, used, rem+used)
	}
}

// ---------------------------------------------------------------------------
// WO-009 §2.1 — Account conservation: rem + used == original after each op
// ---------------------------------------------------------------------------

func TestSettleUsage_accountConservation(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "cons1", Password: "x", Quota: 5.0}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	for i, charge := range []float64{0.5, 1.2, 0.8} {
		if err := SettleUsage(u.ID, charge, ctx); err != nil {
			t.Fatalf("step %d: unexpected error: %v", i, err)
		}
		rem, used, err := GetQuota(u.ID, ctx)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(rem+used-5.0) > 1e-9 {
			t.Errorf("step %d: rem+used=%.17g, want 5.0", i, rem+used)
		}
	}
}

// ---------------------------------------------------------------------------
// WO-009 §2.2 — Float accumulation: 10 000 × 0.0001 deductions ≈ 1.0 (±1e-9)
// ---------------------------------------------------------------------------

func TestSettleUsage_microAccumulation(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "micro1", Password: "x", Quota: 100.0}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	const N = 10_000
	const amount = 0.0001
	for i := 0; i < N; i++ {
		if err := SettleUsage(u.ID, amount, ctx); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	_, used, err := GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	// used_quota should be ≈ 1.0 (10000 × 0.0001).
	if math.Abs(used-1.0) > 1e-9 {
		t.Errorf("used=%.17g, want ≈1.0 (drift %.3g)", used, used-1.0)
	}
}

// ---------------------------------------------------------------------------
// WO-009 §2.4 — 已作废并反转。原契约「余额不足则整笔丢弃、余额不变」是无限白嫖的
// 直接来源：上游成本已付，却既不扣余额也不涨 used_quota，于是闸门
// （billing.HasBalanceForKey，remaining > 0）继续放行。
// 新契约见 quota_test.go:TestSettleUsage_overspendBecomesDebt（欠款入账 + 充值净掉）
// 与 billing/overdraft_test.go:TestOverdraftIsRecordedAsDebtAndThenBlocked（闸门收口）。
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// WO-009 §2.2 — NaN / Inf guards on SettleUsage
// ---------------------------------------------------------------------------

func TestSettleUsage_nanRejected(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "nan1", Password: "x", Quota: 10.0}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	// NaN amount: SettleUsage must reject it outright, not clamp it. `quota - NaN`
	// would poison the column permanently — every later `remaining > 0` is false so
	// the account locks out, and no top-up can arithmetically repair it. This used
	// to be caught only as a side effect of the removed `WHERE quota >= amount`
	// guard (SQL comparisons against NaN are always false); the check is explicit now.
	if err := SettleUsage(u.ID, math.NaN(), ctx); !errors.Is(err, ErrNonFiniteAmount) {
		t.Errorf("SettleUsage(NaN) error = %v, want ErrNonFiniteAmount", err)
	}
	for _, inf := range []float64{math.Inf(1), math.Inf(-1)} {
		if err := SettleUsage(u.ID, inf, ctx); !errors.Is(err, ErrNonFiniteAmount) {
			t.Errorf("SettleUsage(%v) error = %v, want ErrNonFiniteAmount", inf, err)
		}
	}
	rem, used, err := GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if math.IsNaN(rem) || math.IsInf(rem, 0) {
		t.Fatalf("balance is %v — a non-finite amount poisoned the column", rem)
	}
	if rem != 10.0 {
		t.Errorf("non-finite settle changed balance: rem=%.17g", rem)
	}
	if used != 0 {
		t.Errorf("non-finite settle changed used_quota: used=%.17g", used)
	}
}

func TestSettleUsage_negativeAmountNoOp(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "neg1", Password: "x", Quota: 10.0}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	// Negative amount: should be no-op (amount <= 0 guard).
	if err := SettleUsage(u.ID, -5.0, ctx); err != nil {
		t.Errorf("want nil for negative amount, got %v", err)
	}
	rem, used, _ := GetQuota(u.ID, ctx)
	if rem != 10.0 || used != 0 {
		t.Errorf("negative amount modified balance: rem=%.17g used=%.17g", rem, used)
	}
}

// ---------------------------------------------------------------------------
// WO-009-续 §2.1 — AddQuota: credits balance; AddQuota(0) is a no-op
// ---------------------------------------------------------------------------

func TestAddQuota_creditsBalance(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "add1", Password: "x", Quota: 5.0}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	if err := AddQuota(u.ID, 5.0, ctx); err != nil {
		t.Fatal(err)
	}
	rem, _, err := GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(rem-10.0) > 1e-9 {
		t.Errorf("want remaining 10.0 after adding 5.0 to 5.0, got %.17g", rem)
	}
}

func TestAddQuota_zeroNoOp(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "add2", Password: "x", Quota: 5.0}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	if err := AddQuota(u.ID, 0, ctx); err != nil {
		t.Fatal(err)
	}
	rem, used, err := GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rem != 5.0 || used != 0 {
		t.Errorf("AddQuota(0) should be no-op: rem=%.17g used=%.17g", rem, used)
	}
}

// ---------------------------------------------------------------------------
// WO-009-续 §2.1 — SetQuota: overwrites balance
// ---------------------------------------------------------------------------

func TestSetQuota_overwritesBalance(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "set1", Password: "x", Quota: 5.0, UsedQuota: 3.0}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	if err := SetQuota(u.ID, 99.0, ctx); err != nil {
		t.Fatal(err)
	}
	rem, _, err := GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rem != 99.0 {
		t.Errorf("SetQuota should overwrite to 99.0, got %.17g", rem)
	}
}
