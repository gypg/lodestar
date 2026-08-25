package billing

import (
	"context"
	"testing"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/user"
)

// Subscription-pool billing: a purchased plan grants a USD pool, and a request
// must spend that pool before it touches the buyer's wallet. Before the pool was
// wired, UserSubscription.AmountUsed was never incremented and the relay billed
// only User.Quota, so buying a plan spent money and granted nothing.

// grantPool inserts an active subscription pool for the user.
func grantPool(t *testing.T, userID uint, total, used float64) *model.UserSubscription {
	t.Helper()
	now := time.Now().Unix()
	sub := &model.UserSubscription{
		UserID: userID, PlanID: 1,
		AmountTotal: total, AmountUsed: used,
		StartsAt: now - 60, ExpiresAt: now + 3600,
		Status: model.SubStatusActive, Source: "order", CreatedAt: now,
	}
	if err := db.GetDB().Create(sub).Error; err != nil {
		t.Fatalf("grant pool: %v", err)
	}
	return sub
}

func poolUsed(t *testing.T, id int) float64 {
	t.Helper()
	var got model.UserSubscription
	if err := db.GetDB().First(&got, id).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	return got.AmountUsed
}

// TestChargeKey_poolCoversCost_walletUntouched is the whole point of selling a
// subscription: the buyer consumes what they already paid for, and their wallet
// balance does not move.
func TestChargeKey_poolCoversCost_walletUntouched(t *testing.T) {
	uid, keyID := initBillingTestDB(t, 10.0)
	ctx := context.Background()
	sub := grantPool(t, uid, 5.0, 0)

	ChargeKey(keyID, 2.0, ctx)

	if got := poolUsed(t, sub.ID); got != 2.0 {
		t.Errorf("pool amount_used = %v, want 2.0 (the pool must fund the request)", got)
	}
	rem, used, err := user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rem != 10.0 {
		t.Errorf("wallet balance = %v, want 10.0 (pool-funded request must not touch the wallet)", rem)
	}
	if used != 0 {
		t.Errorf("wallet used_quota = %v, want 0 (pool spend belongs to amount_used)", used)
	}
}

// TestChargeKey_poolPartiallyCovers_remainderHitsWallet pins the split. Getting
// this wrong in either direction is a money bug: charging the full cost to both
// double-bills, charging neither serves for free.
func TestChargeKey_poolPartiallyCovers_remainderHitsWallet(t *testing.T) {
	uid, keyID := initBillingTestDB(t, 10.0)
	ctx := context.Background()
	// Pool has 1.5 left of 4.0.
	sub := grantPool(t, uid, 4.0, 2.5)

	ChargeKey(keyID, 2.0, ctx) // 1.5 from pool, 0.5 from wallet

	if got := poolUsed(t, sub.ID); got != 4.0 {
		t.Errorf("pool amount_used = %v, want 4.0 (pool exhausted, never overdrawn)", got)
	}
	rem, used, err := user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rem != 9.5 {
		t.Errorf("wallet balance = %v, want 9.5 (only the 0.5 remainder)", rem)
	}
	if used != 0.5 {
		t.Errorf("wallet used_quota = %v, want 0.5", used)
	}
}

// TestChargeKey_exhaustedPool_fullCostHitsWallet: once the pool is spent the
// subscriber pays from their wallet like anyone else.
func TestChargeKey_exhaustedPool_fullCostHitsWallet(t *testing.T) {
	uid, keyID := initBillingTestDB(t, 10.0)
	ctx := context.Background()
	sub := grantPool(t, uid, 3.0, 3.0)

	ChargeKey(keyID, 2.0, ctx)

	if got := poolUsed(t, sub.ID); got != 3.0 {
		t.Errorf("pool amount_used = %v, want 3.0 (unchanged)", got)
	}
	rem, used, err := user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rem != 8.0 {
		t.Errorf("wallet balance = %v, want 8.0", rem)
	}
	if used != 2.0 {
		t.Errorf("wallet used_quota = %v, want 2.0", used)
	}
}

// TestChargeKey_expiredPool_fullCostHitsWallet: an expired subscription must
// stop funding usage, otherwise a lapsed subscriber keeps consuming for free.
func TestChargeKey_expiredPool_fullCostHitsWallet(t *testing.T) {
	uid, keyID := initBillingTestDB(t, 10.0)
	ctx := context.Background()
	now := time.Now().Unix()
	sub := &model.UserSubscription{
		UserID: uid, PlanID: 1, AmountTotal: 50, AmountUsed: 0,
		StartsAt: now - 7200, ExpiresAt: now - 1,
		Status: model.SubStatusActive, Source: "order", CreatedAt: now,
	}
	if err := db.GetDB().Create(sub).Error; err != nil {
		t.Fatal(err)
	}

	ChargeKey(keyID, 2.0, ctx)

	if got := poolUsed(t, sub.ID); got != 0 {
		t.Errorf("expired pool amount_used = %v, want 0", got)
	}
	rem, _, err := user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rem != 8.0 {
		t.Errorf("wallet balance = %v, want 8.0 (expired pool must not pay)", rem)
	}
}

// TestHasBalanceForKey_emptyWalletButPoolLeft_allows is the gate half of the
// same promise. Refusing here would sell a pool and then not honour it — the
// customer paid, has quota left, and would still get 402.
func TestHasBalanceForKey_emptyWalletButPoolLeft_allows(t *testing.T) {
	uid, keyID := initBillingTestDB(t, 0)
	ctx := context.Background()
	grantPool(t, uid, 5.0, 1.0)

	if !HasBalanceForKey(keyID, ctx) {
		t.Fatal("HasBalanceForKey = false, want true (wallet empty but 4.0 of pool remains)")
	}
}

// TestHasBalanceForKey_negativeWalletButPoolLeft_allows: a wallet in debt must
// not block the pool either — the debt and the pool are separate ledgers.
func TestHasBalanceForKey_negativeWalletButPoolLeft_allows(t *testing.T) {
	uid, keyID := initBillingTestDB(t, -3.0)
	ctx := context.Background()
	grantPool(t, uid, 5.0, 0)

	if !HasBalanceForKey(keyID, ctx) {
		t.Fatal("HasBalanceForKey = false, want true (pool has 5.0 regardless of wallet debt)")
	}
}

// TestHasBalanceForKey_emptyWalletAndExhaustedPool_refuses closes the loop: with
// neither wallet nor pool, the request must be refused. If this ever returns
// true the unlimited-overdraft hole is back.
func TestHasBalanceForKey_emptyWalletAndExhaustedPool_refuses(t *testing.T) {
	uid, keyID := initBillingTestDB(t, 0)
	ctx := context.Background()
	grantPool(t, uid, 5.0, 5.0)

	if HasBalanceForKey(keyID, ctx) {
		t.Fatal("HasBalanceForKey = true, want false (no wallet, no pool left)")
	}
}

// TestHasBalanceForKey_poolLookupError_refuses pins a deliberate asymmetry that
// is easy to "clean up" into a bug.
//
// Every other error in HasBalanceForKey fails OPEN, so a transient infra fault
// never breaks the relay. The pool lookup fails CLOSED, because failing open
// there means "cannot verify a pool ⇒ serve for free", which is the
// unlimited-overdraft hole re-entered through an error path. It costs nothing
// legitimate: we only reach the pool check once the wallet is known to be empty.
//
// The error is produced by dropping the table the lookup needs, which is the
// closest reachable stand-in for a broken/migrating DB.
func TestHasBalanceForKey_poolLookupError_refuses(t *testing.T) {
	_, keyID := initBillingTestDB(t, 0)
	ctx := context.Background()

	if err := db.GetDB().Migrator().DropTable(&model.UserSubscription{}); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	if HasBalanceForKey(keyID, ctx) {
		t.Fatal("HasBalanceForKey = true, want false (empty wallet + unverifiable pool must not be served)")
	}
}
