package group

import (
	"strings"
	"testing"
	"time"

	"github.com/gypg/lodestar/internal/model"
)

// TestCreateAutoGroupCandidateDuplicateNameLeavesConnectionUsable exercises the
// in-transaction error early-return of createAutoGroupCandidate (unique-name
// violation). With SQLite running on a single pooled connection and
// _txlock=immediate, a leaked (never rolled back) transaction keeps the write
// lock and blocks/fails every subsequent write on that connection — the next
// write can only succeed if the failed transaction was rolled back.
//
// Ported from internal/op/auto_group_tx_leak_test.go when the duplicate
// package-op copy of the auto-group implementation was removed. That copy was
// the only one under test; this live implementation had no coverage of the
// rollback path at all.
func TestCreateAutoGroupCandidateDuplicateNameLeavesConnectionUsable(t *testing.T) {
	ctx := setupGroupTestDB(t)
	groupMap.Clear()
	t.Cleanup(func() {
		groupCache.Clear()
		groupMap.Clear()
	})

	candidate := model.CandidateGroup{
		EndpointType: model.EndpointTypeChat,
		Canonical:    "dup-candidate-name",
		Refs: []model.ChannelModelRef{
			{ChannelID: 1, RawModel: "gpt-4o"},
		},
	}

	if err := createAutoGroupCandidate(candidate, ctx); err != nil {
		t.Fatalf("first createAutoGroupCandidate: %v", err)
	}

	// Same canonical name again -> unique constraint violation inside the tx.
	if err := createAutoGroupCandidate(candidate, ctx); err == nil {
		t.Fatal("second createAutoGroupCandidate with duplicate name expected error, got nil")
	}

	// The rolled-back transaction must not block the next write on the shared
	// connection. Guarded by a timer because a leaked transaction deadlocks the
	// BEGIN on the sole connection (busy_timeout cannot break a self-wait).
	errCh := make(chan error, 1)
	go func() {
		errCh <- createAutoGroupCandidate(model.CandidateGroup{
			EndpointType: model.EndpointTypeChat,
			Canonical:    "after-failed-candidate",
			Refs: []model.ChannelModelRef{
				{ChannelID: 1, RawModel: "gpt-4o-mini"},
			},
		}, ctx)
	}()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("createAutoGroupCandidate after failed one failed (leaked transaction?): %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("createAutoGroupCandidate after failed one blocked >15s — leaked transaction is holding the SQLite write lock")
	}

	groups, err := GroupList(ctx)
	if err != nil {
		t.Fatalf("GroupList: %v", err)
	}
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.Name)
	}
	if !containsFold(names, "dup-candidate-name") || !containsFold(names, "after-failed-candidate") {
		t.Errorf("expected both groups present, got %v", names)
	}
}

func containsFold(values []string, target string) bool {
	for _, v := range values {
		if strings.EqualFold(v, target) {
			return true
		}
	}
	return false
}
