package group

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

// setupGroupTestDB initialises an in-memory SQLite DB and clears the group
// cache, mirroring the pattern used by purge_test.go.
func setupGroupTestDB(t *testing.T) context.Context {
	t.Helper()
	ctx := context.Background()
	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", testName)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	groupCache.Clear()
	t.Cleanup(func() { groupCache.Clear() })
	return ctx
}

func TestGroupCreateRejectsInvalidCondition(t *testing.T) {
	ctx := setupGroupTestDB(t)
	group := &model.Group{
		Name:         "test-group",
		EndpointType: "*",
		Condition:    `[{"key":"api_key_nmae","op":"equals","value":"x"}]`,
	}
	if err := GroupCreate(group, ctx); err == nil {
		t.Fatal("GroupCreate() expected error for invalid condition, got nil")
	}
}

func TestGroupCreateAcceptsEmptyCondition(t *testing.T) {
	ctx := setupGroupTestDB(t)
	group := &model.Group{
		Name:         "empty-cond-group",
		EndpointType: "*",
		Condition:    "",
	}
	if err := GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate() with empty condition error = %v", err)
	}
}

func TestGroupUpdateRejectsInvalidCondition(t *testing.T) {
	ctx := setupGroupTestDB(t)
	// Seed a group so GroupUpdate finds it in the cache.
	seed := &model.Group{
		Name:         "upd-group",
		EndpointType: "*",
	}
	if err := GroupCreate(seed, ctx); err != nil {
		t.Fatalf("seed GroupCreate: %v", err)
	}

	bad := `[{"key":"model","op":"contais","value":"x"}]`
	req := &model.GroupUpdateRequest{ID: seed.ID, Condition: &bad}
	if _, err := GroupUpdate(req, ctx); err == nil {
		t.Fatal("GroupUpdate() expected error for invalid condition, got nil")
	}
}

func TestGroupUpdateAcceptsValidCondition(t *testing.T) {
	ctx := setupGroupTestDB(t)
	seed := &model.Group{
		Name:         "upd-valid-group",
		EndpointType: "*",
	}
	if err := GroupCreate(seed, ctx); err != nil {
		t.Fatalf("seed GroupCreate: %v", err)
	}

	good := `[{"key":"model","op":"contains","value":"gpt-4"}]`
	req := &model.GroupUpdateRequest{ID: seed.ID, Condition: &good}
	if _, err := GroupUpdate(req, ctx); err != nil {
		t.Fatalf("GroupUpdate() with valid condition error = %v", err)
	}
}
