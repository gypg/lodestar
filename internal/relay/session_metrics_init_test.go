package relay

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// launchesAfterPackageInit is the session metrics worker launch count observed
// after all of package relay's init() functions have run but before any test body
// executes. TestMain is the only place that can sample this reliably: a
// package-level var initialiser would be evaluated *before* init() runs, and an
// ordinary test could be preceded by another test that starts the worker.
var launchesAfterPackageInit int64

func TestMain(m *testing.M) {
	// The counter is incremented by the worker's own goroutine, so sampling it the
	// instant init() returns reads 0 even when a worker *was* launched — the
	// goroutine simply has not been scheduled yet. A mutation that reintroduced the
	// bug indirectly (init() → helper → `go`) survived exactly this way. Give any
	// init-launched goroutine a window to execute its first statement before
	// sampling; one atomic add needs far less than this.
	time.Sleep(300 * time.Millisecond)
	launchesAfterPackageInit = SessionMetricsWorkerLaunches()
	os.Exit(m.Run())
}

// TestNoWorkerRunningAfterPackageInit is the authoritative guard against the data
// race, and unlike the source scan below it cannot be evaded.
//
// The scan only recognises a `go` statement written directly inside an init()
// body. A mutation that moved the launch one level out — init() calling a helper
// whose body holds the `go` statement — survived the scan while reintroducing the
// exact race. This test observes the effect instead of the syntax, so any launch
// mechanism (direct, via helper, via variable initialiser) trips it.
func TestNoWorkerRunningAfterPackageInit(t *testing.T) {
	if launchesAfterPackageInit != 0 {
		t.Fatalf("session metrics workers launched during package init = %d, want 0.\n"+
			"The worker reads relayStreamSessions.shards, which is populated by "+
			"stream_session.go's init(). Same-package init() order is filename order, so a "+
			"worker started from any init() can read those maps while they are still being "+
			"written — the data race CI caught on c7037c5. Start it from task.Init via "+
			"StartSessionMetricsWorker() instead.", launchesAfterPackageInit)
	}
}

// TestNoGoroutineLaunchedFromPackageInit is the guard for the data race CI caught
// on c7037c5:
//
//	WARNING: DATA RACE
//	Read at ... by goroutine 14:
//	  relay.ActiveSessionCount()   stream_session.go:644
//	  relay.sessionMetricsWorker() runtime_stats.go:58
//	Previous write at ... by main goroutine:
//	  relay.init.3()               stream_session.go:137
//
// `go sessionMetricsWorker()` sat in runtime_stats.go's init(); the shard maps it
// reads are built by stream_session.go's init(). Go runs a package's init()
// functions in filename order, so the worker was reading
// relayStreamSessions.shards while that assignment was still in flight.
//
// A test asserting only "StartSessionMetricsWorker exists and works" would not
// notice someone re-adding `go worker()` to an init() — the package would race
// again while every behavioural test stayed green. So this asserts on the source:
// no init() in this package may launch a goroutine. The check is deliberately
// scoped to package relay, where the ordering hazard lives.
func TestNoGoroutineLaunchedFromPackageInit(t *testing.T) {
	fset := token.NewFileSet()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	var scanned int
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != "init" || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if goStmt, ok := n.(*ast.GoStmt); ok {
					t.Errorf("%s:%d: init() launches a goroutine (%s). "+
						"Background workers in this package must be started explicitly after "+
						"package init, because same-package init() order is filename order and a "+
						"goroutine started in one init() can read state a later init() is still "+
						"writing. Use an exported Start... func called from task.Init instead.",
						name, fset.Position(goStmt.Pos()).Line, exprText(goStmt.Call.Fun))
				}
				return true
			})
		}
	}

	// Guard the guard: a glob that silently matched nothing would pass forever.
	// The package has far more than a handful of files; 10 is a safe floor.
	if scanned < 10 {
		t.Fatalf("scanned only %d non-test .go files in package relay, expected >= 10: the source scan is not looking at the package", scanned)
	}
}

func exprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name + "()"
	case *ast.SelectorExpr:
		return exprText(v.X) + "." + v.Sel.Name + "()"
	case *ast.FuncLit:
		return "func literal"
	default:
		return "goroutine"
	}
}

// TestStartSessionMetricsWorkerIsIdempotent pins the CompareAndSwap guard by
// counting workers that actually started, not by reading the started flag.
//
// An earlier version of this test asserted only the flag, and a mutation that kept
// `sessionMetricsStarted.Store(true)` while deleting the CAS gate survived it: two
// tickers were running, both writing the same gauges, and nothing failed. Counting
// launches is what gives the test teeth.
func TestStartSessionMetricsWorkerIsIdempotent(t *testing.T) {
	t.Cleanup(func() { sessionMetricsStarted.Store(false) })

	sessionMetricsStarted.Store(false)
	before := SessionMetricsWorkerLaunches()

	// The worker only ticks every conf.SSEHeartbeatInterval and never touches the
	// DB, so starting it here is cheap and does not need to be joined.
	StartSessionMetricsWorker()
	if !sessionMetricsStarted.Load() {
		t.Fatal("sessionMetricsStarted = false after StartSessionMetricsWorker(), want true: the started flag must be set")
	}
	waitForWorkerLaunches(t, before+1)

	// Second and third calls must be no-ops: the CAS fails, so no further worker is
	// spawned. Exact expected value — "at least one" would not catch a double start.
	StartSessionMetricsWorker()
	StartSessionMetricsWorker()

	// Give any wrongly-spawned goroutine time to run its first statement, otherwise
	// this could pass simply because the extra worker had not been scheduled yet.
	time.Sleep(200 * time.Millisecond)

	if got := SessionMetricsWorkerLaunches(); got != before+1 {
		t.Fatalf("session metrics workers launched = %d, want exactly %d: repeated "+
			"StartSessionMetricsWorker calls must not start a second ticker writing the same gauges",
			got-before, 1)
	}
}

// waitForWorkerLaunches waits for the launch counter to reach want, since the
// worker increments it on its own goroutine.
func waitForWorkerLaunches(t *testing.T, want int64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if SessionMetricsWorkerLaunches() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("session metrics worker launches = %d after 3s, want %d: the worker goroutine never started",
		SessionMetricsWorkerLaunches(), want)
}

// TestActiveSessionCountShardMapsInitialised pins the invariant the race broke:
// every shard map must be non-nil once package init has finished, which is what
// makes ActiveSessionCount safe for the worker to call.
func TestActiveSessionCountShardMapsInitialised(t *testing.T) {
	for i := range relayStreamSessions.shards {
		if relayStreamSessions.shards[i].byKey == nil {
			t.Fatalf("relayStreamSessions.shards[%d].byKey = nil after package init, want an allocated map", i)
		}
	}
	if got := len(relayStreamSessions.shards); got != streamStoreShardCount {
		t.Fatalf("shard count = %d, want %d", got, streamStoreShardCount)
	}
	if got := ActiveSessionCount(); got != 0 {
		t.Fatalf("ActiveSessionCount() = %d on a store with no sessions, want 0", got)
	}
}
