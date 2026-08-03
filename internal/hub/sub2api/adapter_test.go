package sub2api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/utils/crypto"
)

// newTestSite wires a RemoteSite pointing at the given test server with an
// encrypted access token, and clears the process-wide token cache so each test
// starts from a known auth state.
func newTestSite(t *testing.T, baseURL string) *model.RemoteSite {
	t.Helper()
	crypto.Init("test-encryption-key")

	tokenMu.Lock()
	tokenCache = make(map[string]*cachedToken)
	tokenMu.Unlock()

	enc, err := crypto.Encrypt("jwt-access-token")
	if err != nil {
		t.Fatalf("encrypt token: %v", err)
	}
	return &model.RemoteSite{
		BaseURL:     baseURL,
		Username:    "alice",
		AccessToken: enc,
		SiteType:    model.SiteTypeSub2API,
	}
}

// envelope wraps a payload in Sub2API's {code, message, data} response shape.
func envelope(w http.ResponseWriter, data interface{}) {
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"code":    0,
		"message": "success",
		"data":    data,
	})
}

// TestFetchTokensMapsRealSub2APIShape locks in the real Sub2API contract:
// `status` is a string, `expires_at` is an ISO-8601 timestamp, and `quota` is
// the spending cap rather than the remaining balance.
func TestFetchTokensMapsRealSub2APIShape(t *testing.T) {
	expiresAt := time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC)
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	var gotPages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/keys" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		gotPages = append(gotPages, r.URL.Query().Get("page"))
		envelope(w, map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":         7,
					"name":       "编程",
					"key":        "sk-sub2api-abc",
					"status":     "active",
					"quota":      10.0,
					"quota_used": 2.5,
					"expires_at": expiresAt.Format(time.RFC3339),
					"created_at": createdAt.Format(time.RFC3339),
				},
				{
					"id":         8,
					"name":       "disabled-key",
					"key":        "sk-sub2api-def",
					"status":     "disabled",
					"quota":      0.0,
					"quota_used": 1.0,
					"expires_at": nil,
					"created_at": createdAt.Format(time.RFC3339),
				},
			},
			"total": 2,
		})
	}))
	defer server.Close()

	tokens, err := (&Adapter{}).FetchTokens(context.Background(), newTestSite(t, server.URL))
	if err != nil {
		t.Fatalf("FetchTokens: %v", err)
	}
	if len(tokens) != 2 {
		t.Fatalf("got %d tokens, want 2", len(tokens))
	}

	// Sub2API pagination is 1-based; page=0 would be rejected server-side.
	if len(gotPages) != 1 || gotPages[0] != "1" {
		t.Errorf("requested pages = %v, want exactly [1]", gotPages)
	}

	active := tokens[0]
	if active.Status != 1 {
		t.Errorf(`Status = %d for "active", want 1`, active.Status)
	}
	// quota 10 USD cap - 2.5 USD spent = 7.5 USD remaining.
	if want := 7.5 * quotaPerUSD; active.RemainQuota != want {
		t.Errorf("RemainQuota = %v, want %v (cap minus used)", active.RemainQuota, want)
	}
	if want := 2.5 * quotaPerUSD; active.UsedQuota != want {
		t.Errorf("UsedQuota = %v, want %v", active.UsedQuota, want)
	}
	if active.UnlimitedQuota {
		t.Error("UnlimitedQuota = true, want false for a capped key")
	}
	if active.ExpiredTime != expiresAt.Unix() {
		t.Errorf("ExpiredTime = %d, want %d (parsed from ISO-8601)", active.ExpiredTime, expiresAt.Unix())
	}
	if active.CreatedTime != createdAt.Unix() {
		t.Errorf("CreatedTime = %d, want %d", active.CreatedTime, createdAt.Unix())
	}

	disabled := tokens[1]
	if disabled.Status != 2 {
		t.Errorf(`Status = %d for "disabled", want 2`, disabled.Status)
	}
	if !disabled.UnlimitedQuota {
		t.Error("UnlimitedQuota = false, want true for quota=0")
	}
	// An unlimited key has no meaningful remaining balance.
	if disabled.RemainQuota != 0 {
		t.Errorf("RemainQuota = %v, want 0 for an unlimited key", disabled.RemainQuota)
	}
	if disabled.ExpiredTime != 0 {
		t.Errorf("ExpiredTime = %d, want 0 for a null expires_at", disabled.ExpiredTime)
	}
}

// TestFetchTokensPaginatesWithoutLooping guards the 1-based pagination walk:
// a full first page must advance to page 2 and terminate on the short page.
func TestFetchTokensPaginatesWithoutLooping(t *testing.T) {
	var gotPages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		gotPages = append(gotPages, page)

		count := 1 // short page terminates the walk
		if page == "1" {
			count = sub2apiPageSize
		}
		items := make([]map[string]interface{}, 0, count)
		for i := 0; i < count; i++ {
			items = append(items, map[string]interface{}{
				"id":     i + 1,
				"name":   "k",
				"status": "active",
				"quota":  1.0,
			})
		}
		envelope(w, map[string]interface{}{"items": items, "total": sub2apiPageSize + 1})
	}))
	defer server.Close()

	tokens, err := (&Adapter{}).FetchTokens(context.Background(), newTestSite(t, server.URL))
	if err != nil {
		t.Fatalf("FetchTokens: %v", err)
	}
	if want := sub2apiPageSize + 1; len(tokens) != want {
		t.Fatalf("got %d tokens, want %d", len(tokens), want)
	}
	if len(gotPages) != 2 || gotPages[0] != "1" || gotPages[1] != "2" {
		t.Errorf("requested pages = %v, want [1 2]", gotPages)
	}
}

// TestFetchUsageLogsUsesOneBasedPaging verifies the 0-based caller page index is
// translated to Sub2API's 1-based `page` query parameter.
func TestFetchUsageLogsUsesOneBasedPaging(t *testing.T) {
	var gotPage, gotPageSize string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPage = r.URL.Query().Get("page")
		gotPageSize = r.URL.Query().Get("page_size")
		envelope(w, map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"id":                101,
					"model":             "gpt-5.6-sol",
					"input_tokens":      4942,
					"output_tokens":     840,
					"cache_read_tokens": 15872,
					"total_cost":        0.057846,
					"created_at":        "2026-07-31T10:30:00Z",
					"api_key":           map[string]interface{}{"id": 7, "name": "编程"},
					"group":             map[string]interface{}{"id": 3, "name": "自用"},
				},
			},
			"total": 1,
		})
	}))
	defer server.Close()

	logs, err := (&Adapter{}).FetchUsageLogs(context.Background(), newTestSite(t, server.URL), 0, 50)
	if err != nil {
		t.Fatalf("FetchUsageLogs: %v", err)
	}
	if gotPage != "1" {
		t.Errorf("page = %q, want %q (caller page 0 maps to 1-based page 1)", gotPage, "1")
	}
	if gotPageSize != "50" {
		t.Errorf("page_size = %q, want %q", gotPageSize, "50")
	}
	if len(logs) != 1 {
		t.Fatalf("got %d logs, want 1", len(logs))
	}

	l := logs[0]
	if l.TokenName != "编程" {
		t.Errorf("TokenName = %q, want %q (from nested api_key.name)", l.TokenName, "编程")
	}
	if l.Group != "自用" {
		t.Errorf("Group = %q, want %q (from nested group.name)", l.Group, "自用")
	}
	if want := int64(4942 + 840 + 15872); l.TotalTokens != want {
		t.Errorf("TotalTokens = %d, want %d (summed when total_tokens is absent)", l.TotalTokens, want)
	}
	if want := 0.057846 * quotaPerUSD; l.Quota != want {
		t.Errorf("Quota = %v, want %v", l.Quota, want)
	}
}
