package telemetry

import (
	"sync"
	"testing"
)

// R-8: internal/relay/runtime_stats.go used to carry a second trend implementation
// whose ring buffer and index were read and written from different goroutines with
// no lock and no atomic. Its reader had zero call sites, so the whole thing was
// deleted rather than locked, leaving telemetry.Store as the only trend source.
//
// These tests guard what survived. They drive captureTrendPoint (the same function
// the background ticker calls) rather than reaching into s.trendSnapshots, so they
// break if the ticker's wiring or the eviction bound regresses.

func TestCaptureTrendPointRecordsDeltasNotTotals(t *testing.T) {
	store := NewStore()

	// Tick 1: 3 requests, 1 of them failed.
	store.RecordRequest(100, true)
	store.RecordRequest(100, true)
	store.RecordRequest(100, false)
	lastReq, lastFail := store.captureTrendPoint(0, 0)

	if lastReq != 3 || lastFail != 1 {
		t.Fatalf("returned counters = (%d, %d), want (3, 1)", lastReq, lastFail)
	}

	// Tick 2: 2 more requests, both failed. The point must carry the *delta*
	// (2 and 2), not the running totals (5 and 3) — a snapshot that reported
	// totals would make the dashboard trend monotonically increasing forever.
	store.RecordRequest(100, false)
	store.RecordRequest(100, false)
	lastReq, lastFail = store.captureTrendPoint(lastReq, lastFail)

	if lastReq != 5 || lastFail != 3 {
		t.Fatalf("returned counters = (%d, %d), want (5, 3)", lastReq, lastFail)
	}

	trend := store.Snapshot().TrendSnapshots
	if len(trend) != 2 {
		t.Fatalf("len(trend) = %d, want 2", len(trend))
	}
	if trend[0].RequestDelta != 3 || trend[0].FailedDelta != 1 {
		t.Errorf("trend[0] = (req %d, fail %d), want (3, 1)", trend[0].RequestDelta, trend[0].FailedDelta)
	}
	if trend[1].RequestDelta != 2 || trend[1].FailedDelta != 2 {
		t.Errorf("trend[1] = (req %d, fail %d), want (2, 2)", trend[1].RequestDelta, trend[1].FailedDelta)
	}
}

// A tick with no traffic must record a zero point, not be skipped: gaps in the
// series would be drawn as if the window never existed.
func TestCaptureTrendPointRecordsIdleTick(t *testing.T) {
	store := NewStore()

	lastReq, lastFail := store.captureTrendPoint(0, 0)
	if lastReq != 0 || lastFail != 0 {
		t.Fatalf("returned counters = (%d, %d), want (0, 0)", lastReq, lastFail)
	}

	trend := store.Snapshot().TrendSnapshots
	if len(trend) != 1 {
		t.Fatalf("len(trend) = %d, want 1 (idle ticks must still be recorded)", len(trend))
	}
	if trend[0].RequestDelta != 0 || trend[0].AvgLatencyMs != 0 {
		t.Errorf("idle point = %+v, want zero deltas", trend[0])
	}
}

// The ring must stay bounded at maxTrendSnapshots and keep the *newest* points.
// An off-by-one or a missing evict would grow unbounded for the process lifetime.
func TestCaptureTrendPointEvictsOldestBeyondBound(t *testing.T) {
	store := NewStore()

	const extra = 5
	var lastReq, lastFail int64
	for i := 0; i < maxTrendSnapshots+extra; i++ {
		// One request per tick makes each point's delta identifiable: the Nth
		// tick has RequestDelta 1, and AvgLatencyMs encodes the tick number,
		// so we can tell which points were evicted.
		store.RecordRequest(int64(i+1)*1000, true)
		lastReq, lastFail = store.captureTrendPoint(lastReq, lastFail)
	}

	trend := store.Snapshot().TrendSnapshots
	if len(trend) != maxTrendSnapshots {
		t.Fatalf("len(trend) = %d, want %d", len(trend), maxTrendSnapshots)
	}

	// Every retained point should be a single-request delta.
	for i, p := range trend {
		if p.RequestDelta != 1 {
			t.Fatalf("trend[%d].RequestDelta = %d, want 1", i, p.RequestDelta)
		}
	}

	// AvgLatencyMs is cumulative-average over all requests so far, hence strictly
	// increasing here. The retained window must be the newest one: its first point
	// must be *later* than the first `extra` ticks that were evicted.
	if trend[0].AvgLatencyMs >= trend[len(trend)-1].AvgLatencyMs {
		t.Fatalf("trend not ordered oldest→newest: first=%v last=%v",
			trend[0].AvgLatencyMs, trend[len(trend)-1].AvgLatencyMs)
	}
	// After 65 ticks with latencies 1000..65000, the cumulative average at the
	// oldest *retained* tick (tick 6, 1-indexed) is mean(1000..6000) = 3500.
	// Seeing 1000 here would mean the oldest points were kept instead of evicted.
	if trend[0].AvgLatencyMs != 3500 {
		t.Errorf("oldest retained AvgLatencyMs = %v, want 3500 (evicted the wrong end?)", trend[0].AvgLatencyMs)
	}
}

// Snapshot must hand out a copy: if it returned the live slice, a caller iterating
// the trend while the ticker appended would race, which is exactly the class of bug
// R-8 removed from the relay-side copy.
func TestSnapshotTrendIsCopy(t *testing.T) {
	store := NewStore()
	store.RecordRequest(100, true)
	store.captureTrendPoint(0, 0)

	trend := store.Snapshot().TrendSnapshots
	if len(trend) != 1 {
		t.Fatalf("len(trend) = %d, want 1", len(trend))
	}
	trend[0].RequestDelta = 999

	again := store.Snapshot().TrendSnapshots
	if again[0].RequestDelta != 1 {
		t.Fatalf("mutating the returned trend changed store state: RequestDelta = %d, want 1", again[0].RequestDelta)
	}
}

// Production has exactly one trend writer (the StartBackground ticker, guarded by
// s.started), so writer-vs-writer is unreachable through the real wiring. This test
// nonetheless drives concurrent writers, because that is the only topology in which
// a missing write lock is observable *without* the race detector — which is
// unavailable here (needs cgo+gcc; CI does not run -race either).
//
// Measured: with the s.mu.Lock() in captureTrendPoint removed, this loses appends
// (worst case 26 of 60 over 200 attempts on an 8-core amd64 box). With the lock in
// place it is exact. A single-writer version of this test does NOT catch the missing
// lock — I verified that by mutation, so don't "simplify" this to one writer.
func TestCaptureTrendPointConcurrentWritersDoNotLoseAppends(t *testing.T) {
	// Keep the total at exactly the bound so no eviction can mask a lost append.
	const writers = 30
	const each = 2

	// The loss is a scheduling race, so a single attempt can pass by luck.
	// Repeat enough that a missing lock is caught reliably.
	for attempt := 0; attempt < 50; attempt++ {
		store := NewStore()
		var wg sync.WaitGroup
		start := make(chan struct{})

		for w := 0; w < writers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for i := 0; i < each; i++ {
					store.captureTrendPoint(0, 0)
				}
			}()
		}

		// A concurrent reader, matching the real reader/writer split
		// (ops.TelemetrySummaryGet on an HTTP goroutine vs the ticker).
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 500; i++ {
				_ = store.Snapshot()
			}
		}()

		close(start)
		wg.Wait()

		if got := len(store.Snapshot().TrendSnapshots); got != writers*each {
			t.Fatalf("attempt %d: retained %d points, want %d (lost %d appends — write lock missing?)",
				attempt, got, writers*each, writers*each-got)
		}
	}
}

// Points observed by a reader running alongside the writer must never be partially
// written. Single writer here, matching production.
func TestSnapshotNeverObservesPartialPoint(t *testing.T) {
	store := NewStore()

	const ticks = 40 // under maxTrendSnapshots, so nothing is evicted
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		var lastReq, lastFail int64
		for i := 0; i < ticks; i++ {
			store.RecordRequest(1000, true)
			lastReq, lastFail = store.captureTrendPoint(lastReq, lastFail)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			for _, p := range store.Snapshot().TrendSnapshots {
				// Each tick records exactly one request, so any other delta
				// means a torn or duplicated write became visible.
				if p.RequestDelta != 1 {
					t.Errorf("observed inconsistent trend point: %+v", p)
					return
				}
			}
		}
	}()

	wg.Wait()

	if got := len(store.Snapshot().TrendSnapshots); got != ticks {
		t.Fatalf("len(trend) = %d, want %d", got, ticks)
	}
}
