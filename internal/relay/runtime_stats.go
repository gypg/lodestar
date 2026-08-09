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

func init() {
	go sessionMetricsWorker()
}

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

// sessionMetricsWorker periodically pushes session/sticky counts to the shared telemetry store
// so that ops can read them without an import cycle.
func sessionMetricsWorker() {
	ticker := time.NewTicker(conf.SSEHeartbeatInterval)
	defer ticker.Stop()
	for range ticker.C {
		telemetry.Global().SetActiveSessions(int64(ActiveSessionCount()))
		telemetry.Global().SetStickyBoundSessions(int64(balancer.StickyCount()))
	}
}
