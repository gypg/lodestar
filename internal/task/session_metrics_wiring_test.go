package task

import (
	"context"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/relay"
)

// TestInitStartsSessionMetricsWorker guards the call site, not the function.
//
// Background: CI's race detector caught a real data race on c7037c5 —
// `go sessionMetricsWorker()` was launched from relay's runtime_stats.go init(),
// reading shard maps that stream_session.go's later init() was still writing
// (same-package init() order is filename order). The fix moved the launch to an
// explicit relay.StartSessionMetricsWorker() called from Init().
//
// The failure mode this test exists to catch: someone deletes the
// relay.StartSessionMetricsWorker() line from Init(). Nothing else would notice.
// The worker's only observable effect is a telemetry gauge refreshed every
// SSEHeartbeatInterval (15s), so no behavioural test waits on it — the dashboard
// would just silently report active_sessions = 0 forever. Asserting inside
// package relay would only prove the function works when called, leaving the call
// site deletable, which is exactly the gap here.
func TestInitStartsSessionMetricsWorker(t *testing.T) {
	// Init() is not idempotent across tests in this package (Register warns and
	// skips on duplicate names), so this test owns the process-wide task registry.
	resetTaskRegistryForTest(t)

	if err := db.InitDB("sqlite", "file:task_session_metrics_wiring?mode=memory&cache=shared", false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}

	// Precondition: the worker must NOT already be running. If it were, this test
	// would pass no matter what Init() does — the exact false-green shape to avoid.
	relay.ResetSessionMetricsWorkerForTest()
	if relay.SessionMetricsWorkerStarted() {
		t.Fatal("session metrics worker already started before Init(): the assertion below would be vacuous")
	}

	Init()

	if !relay.SessionMetricsWorkerStarted() {
		t.Fatal("relay.SessionMetricsWorkerStarted() = false after task.Init(): " +
			"Init must start the session metrics worker explicitly. It must not be " +
			"moved back into relay's init() — that reintroduces the shard-map data race.")
	}

	// The dashboard reads these gauges through ops.TelemetrySummaryGet. Confirm the
	// counter the worker publishes is callable and safe now that init has finished,
	// i.e. the shard maps really are allocated.
	if got := relay.ActiveSessionCount(); got != 0 {
		t.Fatalf("relay.ActiveSessionCount() = %d with no live sessions, want 0", got)
	}
}

// resetTaskRegistryForTest clears the package-level task registry so Init() starts
// from a clean slate, and restores it afterwards.
func resetTaskRegistryForTest(t *testing.T) {
	t.Helper()

	tasksMu.Lock()
	saved := tasks
	tasks = make(map[string]*taskEntry)
	tasksMu.Unlock()

	t.Cleanup(func() {
		tasksMu.Lock()
		tasks = saved
		tasksMu.Unlock()
	})
}
