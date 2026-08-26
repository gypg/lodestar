package task

import (
	"context"
	"testing"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/setting"
)

// TestInitRegistersSubscriptionExpireTask guards the call site, not the function.
//
// subscription.ExpireDueSubscriptions shipped with a doc comment saying "intended
// for periodic background invocation" and zero callers — not even a test. Nothing
// ever flipped a due subscription's status, so every expired row stayed "active"
// forever and both subscription lists rendered it as a green 活跃 badge
// (web/src/components/modules/subscription/index.tsx:625).
//
// Asserting only that ExpireSubscriptionsTask works when called would leave the
// Register line deletable, which is precisely how the function ended up orphaned
// the first time.
func TestInitRegistersSubscriptionExpireTask(t *testing.T) {
	// Init() is not idempotent across tests in this package (Register warns and
	// skips on duplicate names), so this test owns the process-wide task registry.
	resetTaskRegistryForTest(t)

	if err := db.InitDB("sqlite", "file:task_sub_expire_wiring?mode=memory&cache=shared", false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}

	// Precondition: the registry must be empty of this task, otherwise the
	// assertion below would pass no matter what Init() does.
	if _, exists := lookupTaskForTest(TaskSubExpire); exists {
		t.Fatalf("task %q already registered before Init(): the assertion below would be vacuous", TaskSubExpire)
	}

	Init()

	entry, exists := lookupTaskForTest(TaskSubExpire)
	if !exists {
		t.Fatalf("task %q not registered after task.Init(): expired subscriptions would keep "+
			"status=active forever and render as an active badge in both subscription lists", TaskSubExpire)
	}
	if entry.interval != time.Hour {
		t.Fatalf("task %q interval = %v, want %v (one hour is the finest whole subscription "+
			"duration tier, model.SubDurationHour)", TaskSubExpire, entry.interval, time.Hour)
	}
	if !entry.runOnStart {
		t.Fatalf("task %q runOnStart = false, want true: without it a restart lets a batch of "+
			"already-expired subscriptions keep showing as active for another full interval", TaskSubExpire)
	}
	if entry.fn == nil {
		t.Fatalf("task %q registered with a nil fn", TaskSubExpire)
	}
}

// TestExpireSubscriptionsTaskFlipsOnlyDueRows is the behavioural half: the task
// must expire what is past expires_at and leave everything else alone.
//
// The "leave alone" side matters more than the flip side. This task writes to the
// same status column the quota pool reads (internal/op/subscription/pool.go:57),
// so an over-broad WHERE would expire live paid subscriptions and stop funding
// requests customers already paid for.
func TestExpireSubscriptionsTaskFlipsOnlyDueRows(t *testing.T) {
	if err := db.InitDB("sqlite", "file:task_sub_expire_behaviour?mode=memory&cache=shared", false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().Unix()
	rows := []model.UserSubscription{
		// 0: due — must flip.
		{UserID: 1, PlanID: 1, AmountTotal: 10, ExpiresAt: now - 60, Status: model.SubStatusActive},
		// 1: due exactly at the boundary (expires_at == now) — must flip; the
		// WHERE uses <=, and the pool's gate is expires_at > now, so a row where
		// expires_at == now is already unfunded and must not read as active.
		{UserID: 2, PlanID: 1, AmountTotal: 10, ExpiresAt: now, Status: model.SubStatusActive},
		// 2: still live — must NOT flip.
		{UserID: 3, PlanID: 1, AmountTotal: 10, ExpiresAt: now + 3600, Status: model.SubStatusActive},
		// 3: never-expiring (expires_at == 0) — must NOT flip. Guarded by
		// `expires_at > 0`; without it a lifetime grant would be killed instantly.
		{UserID: 4, PlanID: 1, AmountTotal: 10, ExpiresAt: 0, Status: model.SubStatusActive},
		// 4: already cancelled and past due — must stay cancelled, not become
		// expired, or an admin's cancellation would be overwritten.
		{UserID: 5, PlanID: 1, AmountTotal: 10, ExpiresAt: now - 60, Status: model.SubStatusCancelled},
	}
	for i := range rows {
		if err := db.GetDB().Create(&rows[i]).Error; err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}

	ExpireSubscriptionsTask()
	waitForSerialWriteDrain(t)

	want := []string{
		model.SubStatusExpired,
		model.SubStatusExpired,
		model.SubStatusActive,
		model.SubStatusActive,
		model.SubStatusCancelled,
	}
	for i, wantStatus := range want {
		var got model.UserSubscription
		if err := db.GetDB().First(&got, rows[i].ID).Error; err != nil {
			t.Fatalf("reload row %d (id=%d): %v", i, rows[i].ID, err)
		}
		if got.Status != wantStatus {
			t.Errorf("row %d (user=%d expires_at=%d seeded=%q): status = %q, want %q",
				i, rows[i].UserID, rows[i].ExpiresAt, rows[i].Status, got.Status, wantStatus)
		}
	}
}

// lookupTaskForTest reads one entry out of the package-level registry.
func lookupTaskForTest(name string) (*taskEntry, bool) {
	tasksMu.RLock()
	defer tasksMu.RUnlock()
	entry, exists := tasks[name]
	return entry, exists
}

// waitForSerialWriteDrain blocks until every write enqueued before it has run.
//
// Why this is needed: on SQLite the task hands its UPDATE to db.EnqueueWrite,
// which returns as soon as the job is on the channel. Asserting straight after
// the call therefore races the writer goroutine — it happened to pass locally,
// which is exactly the kind of green that turns red in CI at 3am.
//
// The barrier relies on two properties of internal/db/writer.go: the queue is a
// channel and there is exactly one consumer goroutine. So a sentinel enqueued
// after the real job cannot run before it. When no serial writer is running,
// EnqueueWrite executes inline and the sentinel closes immediately.
func waitForSerialWriteDrain(t *testing.T) {
	t.Helper()

	drained := make(chan struct{})
	db.EnqueueWrite(db.WriteJob{Name: "test_drain_barrier", Fn: func(context.Context) error {
		close(drained)
		return nil
	}})

	select {
	case <-drained:
	case <-time.After(10 * time.Second):
		t.Fatal("serial DB write queue did not drain within 10s")
	}
}
