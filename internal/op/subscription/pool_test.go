package subscription

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

// activeSub inserts an active subscription with the given pool and returns it.
func activeSub(t *testing.T, userID uint, total, used float64) *model.UserSubscription {
	t.Helper()
	now := time.Now().Unix()
	sub := &model.UserSubscription{
		UserID:      userID,
		PlanID:      1,
		AmountTotal: total,
		AmountUsed:  used,
		StartsAt:    now - 60,
		ExpiresAt:   now + 3600,
		Status:      model.SubStatusActive,
		Source:      "order",
		CreatedAt:   now,
	}
	if err := db.GetDB().Create(sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	return sub
}

func reloadSub(t *testing.T, id int) model.UserSubscription {
	t.Helper()
	var got model.UserSubscription
	if err := db.GetDB().First(&got, id).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	return got
}

// TestDrawFromPool_partialAndExhaustion pins the core contract: a draw never
// exceeds what the pool has left, and reports exactly what it took so the
// caller can bill the remainder to the wallet.
func TestDrawFromPool_partialAndExhaustion(t *testing.T) {
	initSubTestDB(t)
	ctx := context.Background()

	uid := uint(7)
	sub := activeSub(t, uid, 10, 0)

	drawn, err := DrawFromPool(uid, 4, ctx)
	if err != nil {
		t.Fatalf("DrawFromPool: %v", err)
	}
	if drawn != 4 {
		t.Fatalf("first draw = %v, want 4 (pool covers it fully)", drawn)
	}
	if got := reloadSub(t, sub.ID).AmountUsed; got != 4 {
		t.Fatalf("amount_used = %v, want 4", got)
	}

	// Ask for more than the 6 that remain: must cap at 6, not overdraw.
	drawn, err = DrawFromPool(uid, 10, ctx)
	if err != nil {
		t.Fatalf("DrawFromPool: %v", err)
	}
	if drawn != 6 {
		t.Fatalf("capped draw = %v, want 6 (only 6 left of 10)", drawn)
	}
	if got := reloadSub(t, sub.ID).AmountUsed; got != 10 {
		t.Fatalf("amount_used = %v, want 10 (pool exactly exhausted)", got)
	}

	// Exhausted pool must draw nothing rather than going over AmountTotal.
	drawn, err = DrawFromPool(uid, 5, ctx)
	if err != nil {
		t.Fatalf("DrawFromPool: %v", err)
	}
	if drawn != 0 {
		t.Fatalf("draw from exhausted pool = %v, want 0", drawn)
	}
	if got := reloadSub(t, sub.ID).AmountUsed; got != 10 {
		t.Fatalf("amount_used = %v, want 10 (must never exceed amount_total)", got)
	}
}

// TestDrawFromPool_noSubscription is the path every non-subscriber takes: no
// pool, no error, nothing drawn — the caller then bills the wallet in full.
func TestDrawFromPool_noSubscription(t *testing.T) {
	initSubTestDB(t)
	drawn, err := DrawFromPool(42, 3, context.Background())
	if err != nil {
		t.Fatalf("DrawFromPool with no subscription must not error, got %v", err)
	}
	if drawn != 0 {
		t.Fatalf("drawn = %v, want 0", drawn)
	}
}

// TestDrawFromPool_ignoresExpiredAndCancelled makes sure a pool stops paying
// the moment the subscription is no longer active. Without this an expired
// subscriber would keep consuming a pool they no longer own.
func TestDrawFromPool_ignoresExpiredAndCancelled(t *testing.T) {
	initSubTestDB(t)
	ctx := context.Background()
	now := time.Now().Unix()

	cases := []struct {
		name   string
		status string
		expiry int64
	}{
		{"expired by time", model.SubStatusActive, now - 1},
		{"marked expired", model.SubStatusExpired, now + 3600},
		{"cancelled", model.SubStatusCancelled, now + 3600},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uid := uint(100 + i)
			sub := &model.UserSubscription{
				UserID: uid, PlanID: 1, AmountTotal: 50, AmountUsed: 0,
				StartsAt: now - 60, ExpiresAt: tc.expiry, Status: tc.status,
				Source: "order", CreatedAt: now,
			}
			if err := db.GetDB().Create(sub).Error; err != nil {
				t.Fatal(err)
			}
			drawn, err := DrawFromPool(uid, 5, ctx)
			if err != nil {
				t.Fatalf("DrawFromPool: %v", err)
			}
			if drawn != 0 {
				t.Fatalf("drawn = %v, want 0 (%s subscription must not fund usage)", drawn, tc.name)
			}
			if got := reloadSub(t, sub.ID).AmountUsed; got != 0 {
				t.Fatalf("amount_used = %v, want 0", got)
			}
		})
	}
}

// TestDrawFromPool_zeroTotalDrawsNothing covers a plan that grants no pool: it
// must fall through to the wallet, never be treated as unlimited.
func TestDrawFromPool_zeroTotalDrawsNothing(t *testing.T) {
	initSubTestDB(t)
	ctx := context.Background()
	uid := uint(9)
	sub := activeSub(t, uid, 0, 0)

	drawn, err := DrawFromPool(uid, 5, ctx)
	if err != nil {
		t.Fatalf("DrawFromPool: %v", err)
	}
	if drawn != 0 {
		t.Fatalf("drawn = %v, want 0 (amount_total 0 is no pool, NOT unlimited)", drawn)
	}
	if got := reloadSub(t, sub.ID).AmountUsed; got != 0 {
		t.Fatalf("amount_used = %v, want 0", got)
	}
}

// TestDrawFromPool_nonFiniteRejected mirrors the wallet's guard. A NaN draw
// would poison amount_used exactly like it poisons quota: every later
// comparison turns false and the pool can never be repaired.
func TestDrawFromPool_nonFiniteRejected(t *testing.T) {
	initSubTestDB(t)
	ctx := context.Background()
	uid := uint(11)
	sub := activeSub(t, uid, 10, 0)

	for _, amount := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := DrawFromPool(uid, amount, ctx); err == nil {
			t.Fatalf("DrawFromPool(%v) error = nil, want rejection", amount)
		}
	}
	if got := reloadSub(t, sub.ID).AmountUsed; got != 0 {
		t.Fatalf("amount_used = %v, want 0 (a rejected draw must not touch the pool)", got)
	}
}

// TestDrawFromPool_concurrentNeverOverdraws is the oversell guard. Ten
// concurrent draws of 2 against a pool of 10 must hand out exactly 10 in
// total — a read-then-write implementation hands out more.
func TestDrawFromPool_concurrentNeverOverdraws(t *testing.T) {
	initSubTestDB(t)
	ctx := context.Background()
	uid := uint(13)
	sub := activeSub(t, uid, 10, 0)

	const workers = 10
	const each = 2.0

	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0.0
	errs := 0

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			drawn, err := DrawFromPool(uid, each, ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs++
				return
			}
			total += drawn
		}()
	}
	wg.Wait()

	if errs != 0 {
		t.Fatalf("%d draws errored; concurrent draws must serialize, not fail", errs)
	}
	if total != 10 {
		t.Fatalf("total drawn = %v, want exactly 10 (pool size); over means oversell, under means lost capacity", total)
	}
	if got := reloadSub(t, sub.ID).AmountUsed; got != 10 {
		t.Fatalf("amount_used = %v, want 10", got)
	}
}

// TestPoolRemaining reports what the request gate needs: a subscriber whose
// wallet is empty but whose pool has room must still be allowed through.
func TestPoolRemaining(t *testing.T) {
	initSubTestDB(t)
	ctx := context.Background()

	if got, err := PoolRemaining(1, ctx); err != nil || got != 0 {
		t.Fatalf("PoolRemaining(no subscription) = %v, %v; want 0, nil", got, err)
	}

	uid := uint(21)
	activeSub(t, uid, 10, 7.5)
	got, err := PoolRemaining(uid, ctx)
	if err != nil {
		t.Fatalf("PoolRemaining: %v", err)
	}
	if got != 2.5 {
		t.Fatalf("PoolRemaining = %v, want 2.5", got)
	}

	uid2 := uint(22)
	activeSub(t, uid2, 10, 10)
	if got, err := PoolRemaining(uid2, ctx); err != nil || got != 0 {
		t.Fatalf("PoolRemaining(exhausted) = %v, %v; want 0, nil", got, err)
	}
}
