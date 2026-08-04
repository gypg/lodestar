package apikey

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

// setupAPIKeyTestDB initialises an isolated in-memory SQLite DB and clears the
// key caches, mirroring the pattern used by op/group tests.
func setupAPIKeyTestDB(t *testing.T) context.Context {
	t.Helper()
	ctx := context.Background()
	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", testName)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	keyCache.Clear()
	keyIDMap.Clear()
	t.Cleanup(func() {
		keyCache.Clear()
		keyIDMap.Clear()
	})
	return ctx
}

// TestDeleteMissingRowReturnsOriginalError exercises the in-transaction early
// return (RowsAffected == 0): the error message must be unchanged and the
// transaction must be rolled back so subsequent writes are not blocked.
func TestDeleteMissingRowReturnsOriginalError(t *testing.T) {
	ctx := setupAPIKeyTestDB(t)

	// Key exists in the cache but not in the DB -> RowsAffected == 0 path.
	keyCache.Set(999, model.APIKey{ID: 999, Name: "ghost", APIKey: "sk-ghost"})
	keyIDMap.Set("sk-ghost", 999)

	_, err := Get(999, ctx)
	if err != nil {
		t.Fatalf("seed Get: %v", err)
	}

	if err := Delete(999, ctx); err == nil {
		t.Fatal("Delete() for missing DB row expected error, got nil")
	} else if !strings.Contains(err.Error(), "API key not found") {
		t.Errorf("Delete() error = %q, want it to contain %q", err.Error(), "API key not found")
	}

	// The rolled-back transaction must not block the next write on the shared
	// single SQLite connection. Guarded by a timer: a leaked transaction
	// deadlocks the BEGIN on the sole connection (busy_timeout cannot break a
	// self-wait), so without this guard a regression would hang the whole run.
	errCh := make(chan error, 1)
	go func() {
		errCh <- Create(&model.APIKey{Name: "after-failed-delete", APIKey: "sk-after"}, ctx)
	}()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Create after failed Delete failed (leaked transaction?): %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Create after failed Delete blocked >15s — leaked transaction is holding the SQLite write lock")
	}
}

// TestDeleteSuccessPath verifies a legitimate deletion still commits and
// cleans up caches.
func TestDeleteSuccessPath(t *testing.T) {
	ctx := setupAPIKeyTestDB(t)

	key := &model.APIKey{Name: "to-delete", APIKey: "sk-delete-me"}
	if err := Create(key, ctx); err != nil {
		t.Fatalf("seed Create: %v", err)
	}

	if err := Delete(key.ID, ctx); err != nil {
		t.Fatalf("Delete() success path returned error: %v", err)
	}

	if _, err := Get(key.ID, ctx); err == nil {
		t.Error("Get() after Delete expected error, got nil")
	}

	var count int64
	if err := db.GetDB().WithContext(ctx).Model(&model.APIKey{}).Where("id = ?", key.ID).Count(&count).Error; err != nil {
		t.Fatalf("count API keys: %v", err)
	}
	if count != 0 {
		t.Errorf("API key row still present after Delete, count = %d", count)
	}
}
