package apikey

import (
	"context"
	"testing"

	"github.com/gypg/lodestar/internal/model"
)

func TestListByUser_filtersByOwner(t *testing.T) {
	keyCache.Clear()
	keyIDMap.Clear()
	t.Cleanup(func() {
		keyCache.Clear()
		keyIDMap.Clear()
	})

	keyCache.Set(1, model.APIKey{ID: 1, UserID: 10, Name: "a"})
	keyCache.Set(2, model.APIKey{ID: 2, UserID: 20, Name: "b"})
	keyCache.Set(3, model.APIKey{ID: 3, UserID: 10, Name: "c"})

	ctx := context.Background()
	got, err := ListByUser(10, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 keys for user 10, got %d", len(got))
	}
	for _, k := range got {
		if k.UserID != 10 {
			t.Fatalf("unexpected user_id %d on key %d", k.UserID, k.ID)
		}
	}

	all, err := List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("List should still return all keys, got %d", len(all))
	}
}