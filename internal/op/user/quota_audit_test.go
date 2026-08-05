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

func TestDeductQuota_concurrent(t *testing.T) {
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
			if err := DeductQuota(u.ID, 0.3, ctx); err == nil {
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

	// Balance must never go negative.
	if rem < 0 {
		t.Errorf("balance went negative: %.17g", rem)
	}
	// Account conservation: rem + used == 1.0 (within float tolerance).
	if math.Abs(rem+used-1.0) > 1e-9 {
		t.Errorf("account not conserved: rem=%.17g used=%.17g sum=%.17g (want 1.0)", rem, used, rem+used)
	}
	// Exactly (successCount * 0.3) should have been charged.
	expectedUsed := float64(successCount) * 0.3
	if math.Abs(used-expectedUsed) > 1e-9 {
		t.Errorf("used=%.17g, want successCount(%d)*0.3=%.17g", used, successCount, expectedUsed)
	}
	// At most floor(1.0/0.3)=3 requests can succeed.
	if successCount > 3 {
		t.Errorf("too many successes: %d (max 3 at 0.3 each from 1.0)", successCount)
	}
	// At least 1 should succeed (balance starts positive).
	if successCount < 1 {
		t.Errorf("no successes at all (balance=1.0, charge=0.3)")
	}
}

// ---------------------------------------------------------------------------
// WO-009 §2.1 — Account conservation: rem + used == original after each op
// ---------------------------------------------------------------------------

func TestDeductQuota_accountConservation(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "cons1", Password: "x", Quota: 5.0}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	for i, charge := range []float64{0.5, 1.2, 0.8} {
		if err := DeductQuota(u.ID, charge, ctx); err != nil {
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

func TestDeductQuota_microAccumulation(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "micro1", Password: "x", Quota: 100.0}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	const N = 10_000
	const amount = 0.0001
	for i := 0; i < N; i++ {
		if err := DeductQuota(u.ID, amount, ctx); err != nil {
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
// WO-009 §2.4 — Insufficient balance: balance unchanged on failed deduction
// ---------------------------------------------------------------------------

func TestDeductQuota_balanceUnchangedOnInsufficient(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "insuf1", Password: "x", Quota: 0.5}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	err := DeductQuota(u.ID, 1.0, ctx)
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("want ErrInsufficientBalance, got %v", err)
	}
	rem, used, err := GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rem != 0.5 {
		t.Errorf("balance changed: want 0.5, got %.17g", rem)
	}
	if used != 0 {
		t.Errorf("used_quota changed: want 0, got %.17g", used)
	}
}

// ---------------------------------------------------------------------------
// WO-009 §2.2 — NaN / Inf guards on DeductQuota
// ---------------------------------------------------------------------------

func TestDeductQuota_nanRejected(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "nan1", Password: "x", Quota: 10.0}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	// NaN amount: DeductQuota must not credit or deduct anything.
	_ = DeductQuota(u.ID, math.NaN(), ctx)
	rem, used, err := GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rem != 10.0 {
		t.Errorf("NaN deduction changed balance: rem=%.17g", rem)
	}
	if used != 0 {
		t.Errorf("NaN deduction changed used_quota: used=%.17g", used)
	}
}

func TestDeductQuota_negativeAmountNoOp(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "neg1", Password: "x", Quota: 10.0}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	// Negative amount: should be no-op (amount <= 0 guard).
	if err := DeductQuota(u.ID, -5.0, ctx); err != nil {
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
