package subscription

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

func setupWindowTestDB(t *testing.T) context.Context {
	t.Helper()
	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", testName)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return context.Background()
}

func seedWindowSubscription(t *testing.T, ctx context.Context, userID uint, plan model.SubscriptionPlan) *model.UserSubscription {
	t.Helper()
	if err := db.GetDB().WithContext(ctx).Create(&plan).Error; err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	now := time.Now().Unix()
	sub := model.UserSubscription{
		UserID:      userID,
		PlanID:      plan.ID,
		AmountTotal: plan.QuotaAmount,
		StartsAt:    now,
		ExpiresAt:   now + 86400*30,
		Status:      model.SubStatusActive,
	}
	if err := db.GetDB().WithContext(ctx).Create(&sub).Error; err != nil {
		t.Fatalf("seed sub: %v", err)
	}
	return &sub
}

// The admission rule for windows: spend inside the window caps the pool draw,
// and a window is OPTIONAL per plan. A plan with 0/0 window limits must behave
// exactly as before — any test that breaks this leg breaks every existing plan.
func TestWindowHeadroomDisabledPlanUnchanged(t *testing.T) {
	ctx := setupWindowTestDB(t)
	plan := model.SubscriptionPlan{
		Name: "legacy", Price: 10, QuotaAmount: 30,
		Window4hLimit: 0, WindowWeeklyLimit: 0, Enabled: true, EnabledSet: true,
	}
	sub := seedWindowSubscription(t, ctx, 7, plan)

	used, resetAt, err := DrawWindowed(ctx, sub, 5)
	if err != nil {
		t.Fatalf("DrawWindowed: %v", err)
	}
	if used != 5 {
		t.Fatalf("legacy plan: used = %v, want 5 (no window caps)", used)
	}
	if resetAt != 0 {
		t.Fatalf("legacy plan: resetAt = %v, want 0 (no window => no reset)", resetAt)
	}
}

// 4h window: draws succeed until the limit, then the window refuses and
// reports the reset time. The refused remainder must NOT silently become 0 —
// the caller needs to know the draw was partial.
func TestWindow4hLimitsAndRefuses(t *testing.T) {
	ctx := setupWindowTestDB(t)
	plan := model.SubscriptionPlan{
		Name: "w4h", Price: 10, QuotaAmount: 100,
		Window4hLimit: 5, WindowWeeklyLimit: 0, Enabled: true, EnabledSet: true,
	}
	sub := seedWindowSubscription(t, ctx, 8, plan)

	if used, _, err := DrawWindowed(ctx, sub, 3); err != nil || used != 3 {
		t.Fatalf("draw 1: used=%v err=%v, want 3/nil", used, err)
	}
	used, resetAt, err := DrawWindowed(ctx, sub, 3)
	if err != nil {
		t.Fatalf("draw 2: %v", err)
	}
	if used != 2 {
		t.Fatalf("draw 2: used = %v, want 2 (window caps at 5)", used)
	}
	used, resetAt, err = DrawWindowed(ctx, sub, 1)
	if err != nil {
		t.Fatalf("draw 3: %v", err)
	}
	if used != 0 {
		t.Fatalf("draw 3: used = %v, want 0 (window exhausted)", used)
	}
	if resetAt <= time.Now().Unix() {
		t.Fatalf("resetAt = %v, want a future 4h-boundary timestamp", resetAt)
	}
	if resetAt > time.Now().Add(4*time.Hour+time.Minute).Unix() {
		t.Fatalf("resetAt = %v, want within one 4h window from now", resetAt)
	}
}

// Window rollover: after the period elapses, used resets and the anchor
// advances. An idle subscription may skip several periods at once.
func TestWindow4hRollsOver(t *testing.T) {
	ctx := setupWindowTestDB(t)
	plan := model.SubscriptionPlan{
		Name: "w4h-roll", Price: 10, QuotaAmount: 100,
		Window4hLimit: 5, Enabled: true, EnabledSet: true,
	}
	sub := seedWindowSubscription(t, ctx, 9, plan)

	if _, _, err := DrawWindowed(ctx, sub, 5); err != nil {
		t.Fatalf("fill window: %v", err)
	}

	// Force the anchor into the past: one full period + slack.
	stale := time.Now().Add(-5 * time.Hour).Unix()
	if err := db.GetDB().Model(&model.SubscriptionWindow{}).
		Where("subscription_id = ? AND kind = ?", sub.ID, model.SubWindow4h).
		Update("window_start", stale).Error; err != nil {
		t.Fatalf("age window: %v", err)
	}

	used, _, err := DrawWindowed(ctx, sub, 4)
	if err != nil {
		t.Fatalf("post-rollover draw: %v", err)
	}
	if used != 4 {
		t.Fatalf("post-rollover used = %v, want 4 (window reset)", used)
	}
}

// Weekly window composes with the 4h window: both caps apply, the tighter
// one wins, and each tracks its own period.
func TestWeeklyWindowComposes(t *testing.T) {
	ctx := setupWindowTestDB(t)
	plan := model.SubscriptionPlan{
		Name: "weekly", Price: 10, QuotaAmount: 1000,
		// 4h limit is loose so the weekly cap (80) is the binding one.
		Window4hLimit: 500, WindowWeeklyLimit: 80, Enabled: true, EnabledSet: true,
	}
	sub := seedWindowSubscription(t, ctx, 10, plan)

	// The weekly budget (80) runs out across draws while the 4h window stays
	// loose; the composition rule is "the tighter window wins".
	total := 0.0
	for i := 0; i < 10; i++ {
		used, _, err := DrawWindowed(ctx, sub, 10)
		if err != nil {
			t.Fatalf("draw %d: %v", i+1, err)
		}
		total += used
	}
	if total != 80 {
		t.Fatalf("total drawn = %v, want 80 (weekly cap)", total)
	}
}
