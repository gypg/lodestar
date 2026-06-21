package imageportal

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/apikey"
)

// setupTestDB opens an in-memory SQLite DB with the full schema (matching the
// op/twofa test pattern) so ImageRecord + APIKey tables exist.
func setupTestDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
}

// seedUserAndKey creates a user and one of their API keys, returning the IDs.
// imageportal.Create asserts the caller owns the API key used, so tests need
// a real key row that ListByUser can see.
func seedUserAndKey(t *testing.T, username string) (uint, int) {
	t.Helper()
	user := model.User{Username: username, Password: "x", Role: model.UserRoleUser}
	if err := db.GetDB().Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	key := model.APIKey{UserID: user.ID, Name: username + "-key", APIKey: "sk-lodestar-test-" + username}
	if err := apikey.Create(&key, context.Background()); err != nil {
		t.Fatalf("create api key: %v", err)
	}
	return user.ID, key.ID
}

func TestCreate_persistsAndReturnsDetail(t *testing.T) {
	setupTestDB(t)
	uid, kid := seedUserAndKey(t, "alice")

	detail, err := Create(uid, CreateInput{
		Model: "dall-e-3", Prompt: "a snowflake", Size: "1024x1024",
		APIKeyID: kid, URL: "https://example.com/a.png",
	}, context.Background())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if detail.ID == 0 || detail.URL != "https://example.com/a.png" {
		t.Fatalf("unexpected detail: %+v", detail)
	}

	list, err := ListForUser(uid, context.Background())
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 record, got %d", len(list))
	}
	// List summary is a RecordSummary (no URL field) — only fields that exist.
	if list[0].ID != detail.ID || list[0].Prompt != "a snowflake" {
		t.Fatalf("summary mismatch: %+v", list[0])
	}
}

func TestCreate_rejectsEmptyURL(t *testing.T) {
	setupTestDB(t)
	uid, kid := seedUserAndKey(t, "alice")
	if _, err := Create(uid, CreateInput{APIKeyID: kid, URL: "  "}, context.Background()); err == nil {
		t.Fatal("want error for empty URL, got nil")
	}
}

func TestCreate_rejectsUnownedKey(t *testing.T) {
	setupTestDB(t)
	_, aliceKey := seedUserAndKey(t, "alice")
	bob, _ := seedUserAndKey(t, "bob")
	// Bob tries to attach Alice's key to his image record — must be rejected
	// to prevent cross-user key association.
	if _, err := Create(bob, CreateInput{APIKeyID: aliceKey, URL: "https://x"}, context.Background()); err == nil {
		t.Fatal("want error when key not owned by caller, got nil")
	}
}

func TestCreate_requiresAPIKeyID(t *testing.T) {
	setupTestDB(t)
	uid, _ := seedUserAndKey(t, "alice")
	if _, err := Create(uid, CreateInput{APIKeyID: 0, URL: "https://x"}, context.Background()); err == nil {
		t.Fatal("want error for missing api_key_id, got nil")
	}
}

func TestListForUser_isolatedByUser(t *testing.T) {
	setupTestDB(t)
	alice, aliceKey := seedUserAndKey(t, "alice")
	bob, bobKey := seedUserAndKey(t, "bob")

	ctx := context.Background()
	if _, err := Create(alice, CreateInput{APIKeyID: aliceKey, URL: "https://a", Prompt: "a"}, ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(bob, CreateInput{APIKeyID: bobKey, URL: "https://b", Prompt: "b"}, ctx); err != nil {
		t.Fatal(err)
	}

	aliceRecords, err := ListForUser(alice, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliceRecords) != 1 || aliceRecords[0].Prompt != "a" {
		t.Fatalf("alice should see only her record, got %+v", aliceRecords)
	}
}

func TestGetForUser_notFoundAcrossUsers(t *testing.T) {
	setupTestDB(t)
	alice, aliceKey := seedUserAndKey(t, "alice")
	bob, _ := seedUserAndKey(t, "bob")

	created, err := Create(alice, CreateInput{APIKeyID: aliceKey, URL: "https://a"}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Bob cannot read Alice's record by id — must look like not-found, not a leak.
	if _, err := GetForUser(bob, created.ID, context.Background()); err == nil {
		t.Fatal("want error when fetching another user's record, got nil")
	}
	// Alice can.
	if _, err := GetForUser(alice, created.ID, context.Background()); err != nil {
		t.Fatalf("alice should fetch her own record: %v", err)
	}
}

func TestDelete_isolatedByUser(t *testing.T) {
	setupTestDB(t)
	alice, aliceKey := seedUserAndKey(t, "alice")
	bob, _ := seedUserAndKey(t, "bob")

	created, err := Create(alice, CreateInput{APIKeyID: aliceKey, URL: "https://a"}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Bob deleting Alice's record must report not-found (0 rows affected), not delete it.
	if err := Delete(bob, created.ID, context.Background()); err == nil {
		t.Fatal("want error when deleting another user's record, got nil")
	}
	// Record still exists for Alice.
	if _, err := GetForUser(alice, created.ID, context.Background()); err != nil {
		t.Fatalf("record should still exist after failed cross-user delete: %v", err)
	}
	// Alice deletes her own — succeeds.
	if err := Delete(alice, created.ID, context.Background()); err != nil {
		t.Fatalf("alice delete: %v", err)
	}
	if _, err := GetForUser(alice, created.ID, context.Background()); err == nil {
		t.Fatal("record should be gone after owner delete")
	}
}

func TestCreate_trimsAndBoundsPrompt(t *testing.T) {
	setupTestDB(t)
	uid, kid := seedUserAndKey(t, "alice")
	long := strings.Repeat("雪", maxPromptRunes+50)
	detail, err := Create(uid, CreateInput{
		APIKeyID: kid, URL: "https://x", Prompt: "  " + long + "  ",
	}, context.Background())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len([]rune(detail.Prompt)) != maxPromptRunes {
		t.Fatalf("prompt should be trimmed+clamped to %d runes, got %d", maxPromptRunes, len([]rune(detail.Prompt)))
	}
}

func TestPruneExcess_capsRecordCount(t *testing.T) {
	setupTestDB(t)
	uid, kid := seedUserAndKey(t, "alice")
	ctx := context.Background()

	// Insert well over the cap; each insert triggers pruneExcess.
	const extra = 30
	total := maxRecordsPerUser + extra
	for i := 0; i < total; i++ {
		if _, err := Create(uid, CreateInput{
			APIKeyID: kid, URL: fmt.Sprintf("https://x/%d", i), Prompt: fmt.Sprintf("p%d", i),
		}, ctx); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	// Query the DB directly — ListForUser orders by created_at DESC and limits to
	// 100, which is non-deterministic when records share a timestamp second.
	var count int64
	if err := db.GetDB().Model(&model.ImageRecord{}).Where("user_id = ?", uid).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count > maxRecordsPerUser {
		t.Fatalf("record count %d exceeds cap %d (prune did not run)", count, maxRecordsPerUser)
	}

	// Pruning deletes the oldest (lowest id). The most recent insert must survive.
	// Query by id DESC directly to avoid created_at-tie nondeterminism.
	var newest model.ImageRecord
	if err := db.GetDB().Where("user_id = ?", uid).Order("id DESC").First(&newest).Error; err != nil {
		t.Fatalf("query newest: %v", err)
	}
	wantPrompt := fmt.Sprintf("p%d", total-1)
	if newest.Prompt != wantPrompt {
		t.Fatalf("newest record should be %q, got %q (id=%d)", wantPrompt, newest.Prompt, newest.ID)
	}

	// And the oldest surviving record should be the (extra)th insert — the first
	// `extra` records were pruned.
	var oldest model.ImageRecord
	if err := db.GetDB().Where("user_id = ?", uid).Order("id ASC").First(&oldest).Error; err != nil {
		t.Fatalf("query oldest: %v", err)
	}
	oldestWant := fmt.Sprintf("p%d", extra)
	if oldest.Prompt != oldestWant {
		t.Fatalf("oldest surviving should be %q, got %q (id=%d)", oldestWant, oldest.Prompt, oldest.ID)
	}
}
