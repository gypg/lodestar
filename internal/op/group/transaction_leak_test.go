package group

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

// TestGroupUpdateEmptyNameRejected verifies that the "group name is required"
// validation survives the transaction-leak fix (WO-007). Before the fix this
// early-return path skipped tx.Rollback(); the fix must not alter the error.
func TestGroupUpdateEmptyNameRejected(t *testing.T) {
	ctx := setupGroupTestDB(t)
	seed := &model.Group{
		Name:         "leak-check-group",
		EndpointType: model.EndpointTypeChat,
	}
	if err := GroupCreate(seed, ctx); err != nil {
		t.Fatalf("seed GroupCreate: %v", err)
	}

	emptyName := "   "
	_, err := GroupUpdate(&model.GroupUpdateRequest{ID: seed.ID, Name: &emptyName}, ctx)
	if err == nil {
		t.Fatal("GroupUpdate() with blank name expected error, got nil")
	}
	if !strings.Contains(err.Error(), "group name is required") {
		t.Errorf("GroupUpdate() error = %q, want it to contain %q", err.Error(), "group name is required")
	}

	// The failed update must not have changed the stored name.
	got, err := GroupGet(seed.ID, ctx)
	if err != nil {
		t.Fatalf("GroupGet after failed update: %v", err)
	}
	if got.Name != "leak-check-group" {
		t.Errorf("group name = %q after rejected update, want unchanged %q", got.Name, "leak-check-group")
	}
}

// TestGroupUpdateEmptyNameThenSuccessIsUnblocked is the functional regression
// behind the leak: SQLite runs the group package on a single pooled connection
// with _txlock=immediate, so a leaked (never rolled back) transaction keeps
// holding the write lock and the NEXT write on that connection blocks. With
// busy_timeout=5000ms it then fails with "database table is locked". This test
// performs exactly one write after the empty-name rejection; it can only pass
// if the leaked transaction was rolled back.
func TestGroupUpdateEmptyNameThenSuccessIsUnblocked(t *testing.T) {
	ctx := setupGroupTestDB(t)
	seed := &model.Group{
		Name:         "leak-probe-group",
		EndpointType: model.EndpointTypeChat,
	}
	if err := GroupCreate(seed, ctx); err != nil {
		t.Fatalf("seed GroupCreate: %v", err)
	}

	emptyName := ""
	if _, err := GroupUpdate(&model.GroupUpdateRequest{ID: seed.ID, Name: &emptyName}, ctx); err == nil {
		t.Fatal("GroupUpdate() with empty name expected error, got nil")
	}

	// Second write on the same single connection: hangs/fails if the previous
	// transaction leaked. Wrapped in a goroutine because a leaked transaction
	// deadlocks the BEGIN on the shared connection (busy_timeout cannot break a
	// self-wait), so without this guard a regression would hang the whole run.
	newName := "leak-probe-group-renamed"
	errCh := make(chan error, 1)
	go func() {
		_, err := GroupUpdate(&model.GroupUpdateRequest{ID: seed.ID, Name: &newName}, ctx)
		errCh <- err
	}()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("GroupUpdate() after rejected update failed (leaked transaction?): %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("GroupUpdate() after rejected update blocked >15s — leaked transaction is holding the SQLite write lock")
	}

	got, err := GroupGet(seed.ID, ctx)
	if err != nil {
		t.Fatalf("GroupGet: %v", err)
	}
	if got.Name != newName {
		t.Errorf("group name = %q, want %q", got.Name, newName)
	}
}

// TestGroupUpdateSuccessPath verifies a legitimate update still commits:
// scalar fields and item adds must all land in the DB and the cache.
func TestGroupUpdateSuccessPath(t *testing.T) {
	ctx := setupGroupTestDB(t)
	seed := &model.Group{
		Name:         "update-me",
		EndpointType: model.EndpointTypeChat,
	}
	if err := GroupCreate(seed, ctx); err != nil {
		t.Fatalf("seed GroupCreate: %v", err)
	}

	newName := "updated-name"
	newRegex := "(?i)^updated-name$"
	_, err := GroupUpdate(&model.GroupUpdateRequest{
		ID:         seed.ID,
		Name:       &newName,
		MatchRegex: &newRegex,
		ItemsToAdd: []model.GroupItemAddRequest{
			{ChannelID: 101, ModelName: "gpt-4o"},
			{ChannelID: 101, ModelName: "gpt-4o-mini"},
		},
	}, ctx)
	if err != nil {
		t.Fatalf("GroupUpdate() success path returned error: %v", err)
	}

	got, err := GroupGet(seed.ID, ctx)
	if err != nil {
		t.Fatalf("GroupGet: %v", err)
	}
	if got.Name != newName {
		t.Errorf("name = %q, want %q", got.Name, newName)
	}
	if got.MatchRegex != newRegex {
		t.Errorf("match_regex = %q, want %q", got.MatchRegex, newRegex)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items = %d, want 2: %+v", len(got.Items), got.Items)
	}

	// DB row count must match the cache view (transaction really committed).
	var dbItems []model.GroupItem
	if err := testQueryGroupItems(ctx, seed.ID, &dbItems); err != nil {
		t.Fatalf("query group items: %v", err)
	}
	if len(dbItems) != 2 {
		t.Errorf("db items = %d, want 2", len(dbItems))
	}
}

// testQueryGroupItems reads group items directly from the DB, bypassing caches.
func testQueryGroupItems(ctx context.Context, groupID int, out *[]model.GroupItem) error {
	return db.GetDB().WithContext(ctx).Where("group_id = ?", groupID).Find(out).Error
}
