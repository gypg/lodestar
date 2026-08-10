package relay

import (
	"runtime"
	"sync/atomic"
	"time"

	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/relay/balancer"
	"github.com/gypg/lodestar/internal/utils/telemetry"
)

var processStartedAt = time.Now()

var inflightRequests int64

// 运行时趋势数据由 internal/utils/telemetry 那一套提供（Store.trendSnapshots，
// 读写都在 Store.mu 内），经 ops.TelemetrySummaryGet 出到仪表盘。
// 本文件曾并存第二套 trend 实现（trendWorker + TrendSnapshots + 三个计数器），
// 读端零调用方、写端每 30 秒空转一次 runtime.ReadMemStats，且对
// trendSnapshots/trendSnapshotIdx 既无锁也无 atomic —— 已整套删除（R-8）。
// 若以后要恢复，请直接扩展 telemetry.Store，不要再起第二份状态。

// sessionMetricsStarted guards StartSessionMetricsWorker so repeated calls (tests,
// a future second call site) cannot start two tickers writing the same gauges.
var sessionMetricsStarted atomic.Bool

// StartSessionMetricsWorker launches the background worker that publishes session
// gauges to the telemetry store. Safe to call more than once; only the first call
// starts a goroutine.
//
// This must NOT be started from init(). It used to be, and the race detector
// caught why: `go sessionMetricsWorker()` lived in this file's init(), while the
// shard maps the worker reads through ActiveSessionCount are built by a *later*
// init() in stream_session.go. Go runs a package's init() functions in filename
// order, so runtime_stats.go ran before stream_session.go and the worker was
// already reading relayStreamSessions.shards while that map assignment was still
// in flight — a write with no happens-before edge to the read.
//
// Starting the goroutine from an explicit call keeps it ordered after all package
// init(), matching how every other background worker here is wired
// (relaylog.StartFlushWorker, telemetry.StartBackground, db.StartSerialWriter).
func StartSessionMetricsWorker() {
	if !sessionMetricsStarted.CompareAndSwap(false, true) {
		return
	}
	go sessionMetricsWorker()
}

// SessionMetricsWorkerStarted reports whether the session metrics worker has been
// started. Exported so the startup wiring can be asserted from another package:
// the worker's only observable effect is a telemetry gauge refreshed every
// SSEHeartbeatInterval, which is far too slow for a test to wait on.
func SessionMetricsWorkerStarted() bool { return sessionMetricsStarted.Load() }

// ResetSessionMetricsWorkerForTest clears the started flag so a test can assert
// that a call site actually starts the worker. It does not stop an already-running
// worker; the worker is a bare ticker with no side effects beyond refreshing two
// gauges, so a leftover one is harmless.
func ResetSessionMetricsWorkerForTest() { sessionMetricsStarted.Store(false) }

func InflightCount() int64 {
	return atomic.LoadInt64(&inflightRequests)
}

func InflightInc() int64 {
	telemetry.Global().ActiveConnectionsInc()
	return atomic.AddInt64(&inflightRequests, 1)
}

func InflightDec() int64 {
	telemetry.Global().ActiveConnectionsDec()
	return atomic.AddInt64(&inflightRequests, -1)
}

func UptimeSeconds() int64 {
	return int64(time.Since(processStartedAt).Seconds())
}

func ProcessMemoryMB() int64 {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return int64(mem.Alloc / (1024 * 1024))
}

// sessionMetricsWorkerLaunches counts how many worker instances actually began
// running. The started flag alone cannot express "exactly one worker is live", so
// asserting on the flag would let a broken guard that still sets it pass; this
// counter makes the real side effect (a running ticker) observable.
var sessionMetricsWorkerLaunches atomic.Int64

// SessionMetricsWorkerLaunches reports how many session metrics workers have
// started. Exported for tests asserting the single-instance guarantee.
func SessionMetricsWorkerLaunches() int64 { return sessionMetricsWorkerLaunches.Load() }

// sessionMetricsWorker periodically pushes session/sticky counts to the shared telemetry store
// so that ops can read them without an import cycle.
func sessionMetricsWorker() {
	sessionMetricsWorkerLaunches.Add(1)
	ticker := time.NewTicker(conf.SSEHeartbeatInterval)
	defer ticker.Stop()
	for range ticker.C {
		telemetry.Global().SetActiveSessions(int64(ActiveSessionCount()))
		telemetry.Global().SetStickyBoundSessions(int64(balancer.StickyCount()))
	}
}
