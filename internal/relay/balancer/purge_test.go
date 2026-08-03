package balancer

import (
	"testing"
	"time"
)

func TestPurgeIdleStatsRemovesStaleEntries(t *testing.T) {
	globalAutoStats.Range(func(k, _ any) bool { globalAutoStats.Delete(k); return true })

	// active entry
	RecordAutoSuccess(1, "model-active")
	// stale entry: record then backdate its window timestamp
	RecordAutoSuccess(2, "model-stale")
	if v, ok := globalAutoStats.Load(statsKey(2, "model-stale")); ok {
		cs := v.(*ChannelStats)
		cs.mu.Lock()
		for i := range cs.window {
			cs.window[i].Timestamp = time.Now().Add(-2 * time.Hour)
		}
		cs.mu.Unlock()
	}

	removed := PurgeIdleStats(time.Hour)
	if removed < 1 {
		t.Fatalf("PurgeIdleStats removed %d, want >= 1", removed)
	}
	if _, ok := globalAutoStats.Load(statsKey(2, "model-stale")); ok {
		t.Fatal("stale stats entry should have been purged")
	}
	if _, ok := globalAutoStats.Load(statsKey(1, "model-active")); !ok {
		t.Fatal("active stats entry should remain")
	}
}

func TestPurgeIdleSessionsRemovesStaleEntries(t *testing.T) {
	globalSession.Range(func(k, _ any) bool { globalSession.Delete(k); return true })

	SetSticky(1, "fresh", 10, 20)
	globalSession.Store(sessionKey(2, "stale"), &SessionEntry{
		ChannelID:    1,
		ChannelKeyID: 1,
		Timestamp:    time.Now().Add(-2 * time.Hour),
	})

	removed := PurgeIdleSessions(time.Hour)
	if removed != 1 {
		t.Fatalf("PurgeIdleSessions removed %d, want 1", removed)
	}
	if _, ok := globalSession.Load(sessionKey(2, "stale")); ok {
		t.Fatal("stale sticky entry should have been purged")
	}
}

func TestPurgeIdleEntriesRemovesClosedBreakers(t *testing.T) {
	globalBreaker.Range(func(k, _ any) bool { globalBreaker.Delete(k); return true })

	// A closed entry with no failures should be purged.
	getOrCreateEntry(circuitKey(9, 9, "idle"))

	removed := PurgeIdleEntries(time.Hour)
	if removed < 1 {
		t.Fatalf("PurgeIdleEntries removed %d, want >= 1", removed)
	}
}
