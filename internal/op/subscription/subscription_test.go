package subscription

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

// initSubTestDB sets up an in-memory SQLite DB with the subscription + user
// tables and returns a fresh context.
func initSubTestDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()),
	)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(
		&model.User{},
		&model.SubscriptionPlan{},
		&model.SubscriptionOrder{},
		&model.UserSubscription{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}

// newUser creates a user with the given balance and returns its ID.
func newUser(t *testing.T, quota float64) uint {
	t.Helper()
	u := model.User{Username: fmt.Sprintf("u-%d", int(quota*1000)), Password: "x", Quota: quota}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// newPlan creates an enabled plan with the given price and returns its ID.
// newPlan creates a sellable plan at the given price. QuotaAmount must be
// positive or PurchaseWithBalance refuses it up front (ErrPlanGrantsNoQuota),
// and these tests are about the balance deduction, not that guard — the guard
// has its own coverage in plan_grants_quota_test.go.
func newPlan(t *testing.T, price float64) int {
	t.Helper()
	p := model.SubscriptionPlan{
		Name:         "plan",
		Price:        price,
		QuotaAmount:  25,
		DurationType: model.SubDurationDay,
		DurationDays: 1,
		Enabled:      true,
	}
	if err := db.GetDB().Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func TestPurchaseWithBalance_deductsPrice(t *testing.T) {
	initSubTestDB(t)
	ctx := context.Background()
	uid := newUser(t, 10.0)
	pid := newPlan(t, 3.0)

	if err := PurchaseWithBalance(uid, pid, ctx); err != nil {
		t.Fatalf("purchase failed: %v", err)
	}
	var u model.User
	if err := db.GetDB().First(&u, uid).Error; err != nil {
		t.Fatal(err)
	}
	if math.Abs(u.Quota-7.0) > 1e-9 {
		t.Errorf("balance after purchase = %.17g, want 7.0", u.Quota)
	}
}

func TestPurchaseWithBalance_insufficientRejected(t *testing.T) {
	initSubTestDB(t)
	ctx := context.Background()
	uid := newUser(t, 2.0)
	pid := newPlan(t, 3.0)

	err := PurchaseWithBalance(uid, pid, ctx)
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("want ErrInsufficientBalance, got %v", err)
	}
	// Balance unchanged.
	var u model.User
	if err := db.GetDB().First(&u, uid).Error; err != nil {
		t.Fatal(err)
	}
	if math.Abs(u.Quota-2.0) > 1e-9 {
		t.Errorf("insufficient purchase changed balance: %.17g", u.Quota)
	}
	// No order or subscription created.
	var orders int64
	db.GetDB().Model(&model.SubscriptionOrder{}).Count(&orders)
	if orders != 0 {
		t.Errorf("insufficient purchase created %d orders, want 0", orders)
	}
}

func TestPurchaseWithBalance_zeroPrice_free(t *testing.T) {
	initSubTestDB(t)
	ctx := context.Background()
	uid := newUser(t, 0.0)
	pid := newPlan(t, 0.0)

	if err := PurchaseWithBalance(uid, pid, ctx); err != nil {
		t.Fatalf("free plan purchase failed: %v", err)
	}
	var u model.User
	if err := db.GetDB().First(&u, uid).Error; err != nil {
		t.Fatal(err)
	}
	if math.Abs(u.Quota-0.0) > 1e-9 {
		t.Errorf("free plan changed balance: %.17g", u.Quota)
	}
}

// Two concurrent purchases of price 8 on a balance of 10: exactly one must
// succeed and the balance must land at 2 (never negative). This is the WO-009
// BUG-002 regression test — without the WHERE guard, both succeed and the
// balance becomes -6.
func TestPurchaseWithBalance_concurrentNoOversell(t *testing.T) {
	initSubTestDB(t)
	ctx := context.Background()
	uid := newUser(t, 10.0)
	pid := newPlan(t, 8.0)

	var wg sync.WaitGroup
	var success atomic.Int32
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := PurchaseWithBalance(uid, pid, ctx); err == nil {
				success.Add(1)
			}
		}()
	}
	wg.Wait()

	var u model.User
	if err := db.GetDB().First(&u, uid).Error; err != nil {
		t.Fatal(err)
	}
	if success.Load() != 1 {
		t.Errorf("success count = %d, want exactly 1", success.Load())
	}
	if math.Abs(u.Quota-2.0) > 1e-9 {
		t.Errorf("balance = %.17g, want 2.0 (10 - 8)", u.Quota)
	}
	if u.Quota < 0 {
		t.Errorf("balance went negative: %.17g", u.Quota)
	}
}
