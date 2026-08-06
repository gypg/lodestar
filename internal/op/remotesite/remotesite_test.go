package remotesite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	internaldb "github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/hub"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/utils/crypto"
)

func initTestDB(t *testing.T) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	if err := internaldb.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = internaldb.Close() })
	crypto.Init("test-encryption-key")
}

func TestCreateAndList(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()

	created, err := Create(ctx, &model.RemoteSiteCreateRequest{
		Name:        "test-site",
		BaseURL:     "https://example.com",
		SiteType:    model.SiteTypeNewAPI,
		AccessToken: "my-token",
		Password:    "my-pass",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Create returns masked secrets.
	if created.AccessToken != "***" {
		t.Errorf("expected masked access token \"***\", got %q", created.AccessToken)
	}
	if created.Password != "***" {
		t.Errorf("expected masked password \"***\", got %q", created.Password)
	}
	if created.Name != "test-site" {
		t.Errorf("expected name \"test-site\", got %q", created.Name)
	}
	if created.HealthStatus != model.HealthStatusUnknown {
		t.Errorf("expected health status %q, got %q", model.HealthStatusUnknown, created.HealthStatus)
	}
	if created.ExchangeRate != 7.0 {
		t.Errorf("expected default exchange rate 7.0, got %f", created.ExchangeRate)
	}
	if !created.Enabled {
		t.Error("expected enabled to default to true")
	}

	// List should return the created site with masked secrets.
	sites, err := List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(sites))
	}
	if sites[0].AccessToken != "***" {
		t.Errorf("list: expected masked access token \"***\", got %q", sites[0].AccessToken)
	}
	if sites[0].Password != "***" {
		t.Errorf("list: expected masked password \"***\", got %q", sites[0].Password)
	}
	if sites[0].Name != "test-site" {
		t.Errorf("list: expected name \"test-site\", got %q", sites[0].Name)
	}
}

func TestCreateEncryptsSecrets(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()

	_, err := Create(ctx, &model.RemoteSiteCreateRequest{
		Name:        "encrypt-test",
		BaseURL:     "https://example.com",
		SiteType:    model.SiteTypeOctopus,
		AccessToken: "secret-token",
		Password:    "secret-password",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Read directly from DB to verify values are encrypted.
	var site model.RemoteSite
	if err := internaldb.GetDB().First(&site).Error; err != nil {
		t.Fatalf("direct DB read: %v", err)
	}

	if !strings.HasPrefix(site.AccessToken, "enc:") {
		t.Errorf("expected access token to have \"enc:\" prefix, got %q", site.AccessToken)
	}
	if site.AccessToken == "secret-token" {
		t.Error("access token stored as plaintext, expected encrypted")
	}
	if !strings.HasPrefix(site.Password, "enc:") {
		t.Errorf("expected password to have \"enc:\" prefix, got %q", site.Password)
	}
	if site.Password == "secret-password" {
		t.Error("password stored as plaintext, expected encrypted")
	}
}

func TestDelete(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()

	created, err := Create(ctx, &model.RemoteSiteCreateRequest{
		Name:     "delete-me",
		BaseURL:  "https://example.com",
		SiteType: model.SiteTypeNewAPI,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	sites, err := List(ctx)
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(sites) != 0 {
		t.Errorf("expected 0 sites after delete, got %d", len(sites))
	}

	// Verify Get also fails for the deleted ID.
	if _, err := Get(ctx, created.ID); err == nil {
		t.Error("expected error from Get after delete, got nil")
	}
}

func TestListOrderByPinned(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()

	// Create non-pinned site first so it gets a lower ID.
	_, err := Create(ctx, &model.RemoteSiteCreateRequest{
		Name:     "not-pinned",
		BaseURL:  "https://a.example.com",
		SiteType: model.SiteTypeNewAPI,
	})
	if err != nil {
		t.Fatalf("Create non-pinned: %v", err)
	}

	// Create pinned site second (higher ID).
	_, err = Create(ctx, &model.RemoteSiteCreateRequest{
		Name:     "pinned",
		BaseURL:  "https://b.example.com",
		SiteType: model.SiteTypeNewAPI,
	})
	if err != nil {
		t.Fatalf("Create pinned: %v", err)
	}

	// Pin the second site via direct DB update.
	if err := internaldb.GetDB().Model(&model.RemoteSite{}).Where("name = ?", "pinned").Update("pinned", true).Error; err != nil {
		t.Fatalf("pin site: %v", err)
	}

	sites, err := List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sites) != 2 {
		t.Fatalf("expected 2 sites, got %d", len(sites))
	}

	// Pinned site should come first (pinned DESC, sort_order ASC, id ASC).
	if sites[0].Name != "pinned" {
		t.Errorf("expected first site to be \"pinned\", got %q", sites[0].Name)
	}
	if !sites[0].Pinned {
		t.Error("expected first site to have Pinned=true")
	}
	if sites[1].Name != "not-pinned" {
		t.Errorf("expected second site to be \"not-pinned\", got %q", sites[1].Name)
	}
	if sites[1].Pinned {
		t.Error("expected second site to have Pinned=false")
	}
}

// mockSiteAdapter implements hub.SiteAdapter for testing SyncUsageHistory.
// When pages is non-nil it takes precedence over usageLogs and serves one
// slice per page index, so multi-page pagination can be exercised.
type mockSiteAdapter struct {
	usageLogs []hub.RemoteUsageLog
	pages     [][]hub.RemoteUsageLog
	fetchCall int
}

func (m *mockSiteAdapter) FetchUserInfo(_ context.Context, _ *model.RemoteSite) (*hub.UserInfoResult, error) {
	return nil, nil
}

func (m *mockSiteAdapter) PerformCheckIn(_ context.Context, _ *model.RemoteSite) (*hub.CheckInResult, error) {
	return nil, nil
}

func (m *mockSiteAdapter) FetchCheckInStatus(_ context.Context, _ *model.RemoteSite) (*bool, error) {
	return nil, nil
}

func (m *mockSiteAdapter) FetchModels(_ context.Context, _ *model.RemoteSite) ([]string, error) {
	return nil, nil
}

func (m *mockSiteAdapter) FetchModelPricing(_ context.Context, _ *model.RemoteSite) ([]hub.ModelPricingEntry, error) {
	return nil, nil
}

func (m *mockSiteAdapter) FetchTokens(_ context.Context, _ *model.RemoteSite) ([]hub.RemoteToken, error) {
	return nil, nil
}

func (m *mockSiteAdapter) CreateToken(_ context.Context, _ *model.RemoteSite, _ hub.CreateTokenRequest) error {
	return nil
}

func (m *mockSiteAdapter) ListChannels(_ context.Context, _ *model.RemoteSite) ([]hub.RemoteChannel, error) {
	return nil, nil
}

func (m *mockSiteAdapter) CreateChannel(_ context.Context, _ *model.RemoteSite, _ hub.RemoteChannelCreateReq) error {
	return nil
}

func (m *mockSiteAdapter) UpdateChannel(_ context.Context, _ *model.RemoteSite, _ hub.RemoteChannelUpdateReq) error {
	return nil
}

func (m *mockSiteAdapter) DeleteChannel(_ context.Context, _ *model.RemoteSite, _ int) error {
	return nil
}

func (m *mockSiteAdapter) FetchAnnouncement(_ context.Context, _ *model.RemoteSite) (string, error) {
	return "", nil
}

func (m *mockSiteAdapter) FetchSiteStatus(_ context.Context, _ *model.RemoteSite) (*hub.SiteStatusInfo, error) {
	return nil, nil
}

func (m *mockSiteAdapter) RedeemCode(_ context.Context, _ *model.RemoteSite, _ string) (*hub.RedeemResult, error) {
	return nil, nil
}

func (m *mockSiteAdapter) FetchUsageLogs(_ context.Context, _ *model.RemoteSite, page, _ int) ([]hub.RemoteUsageLog, error) {
	m.fetchCall++
	if m.pages != nil {
		if page < 0 || page >= len(m.pages) {
			return nil, nil
		}
		return m.pages[page], nil
	}
	return m.usageLogs, nil
}

// TestSyncUsageHistoryStoresExtendedFields verifies that extended rich fields
// (cache tokens, model mapping, group, subscription) are persisted during sync.
// Values are chosen to match real Sub2API response shape observed during live probe.
func TestSyncUsageHistoryStoresExtendedFields(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()

	created, err := Create(ctx, &model.RemoteSiteCreateRequest{
		Name:     "sub2api-test",
		BaseURL:  "https://example-sub2api.com",
		SiteType: model.SiteTypeSub2API,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	mock := &mockSiteAdapter{
		usageLogs: []hub.RemoteUsageLog{
			{
				ID: 101,
				// Must stay inside the usageLogMaxDays look-back window, so keep it
				// relative to now. An absolute date silently ages out of the window
				// and turns this test red on a fixed calendar day (see the idiom in
				// TestSyncUsageHistoryFlushesBatchBeforeAgeCutoff).
				CreatedAt:           time.Now().Add(-24 * time.Hour).Unix(),
				ModelName:           "gpt-5.6-sol",
				TokenName:           "编程",
				PromptTokens:        4942,
				CompletionTokens:    840,
				TotalTokens:         4942 + 840 + 15872,
				Quota:               0.057846 * 500000,
				CacheReadTokens:     15872,
				CacheCreationTokens: 0,
				RequestedModel:      "gpt-5.6-sol",
				UpstreamModel:       "",
				BillingTier:         "",
				Group:               "自用",
				SubscriptionID:      "",
			},
		},
	}

	hub.Register("sub2api-mock", mock)

	if err := internaldb.GetDB().Model(&model.RemoteSite{}).Where("id = ?", created.ID).Update("site_type", "sub2api-mock").Error; err != nil {
		t.Fatalf("update site type for mock: %v", err)
	}

	n, err := SyncUsageHistory(ctx, created.ID)
	if err != nil {
		t.Fatalf("SyncUsageHistory: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 inserted, got %d", n)
	}

	records, _, err := QueryUsageHistory(ctx, &model.RemoteUsageQuery{SiteID: created.ID, Limit: 10})
	if err != nil {
		t.Fatalf("QueryUsageHistory: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.TokenName != "编程" {
		t.Errorf("TokenName = %q, want %q (from real api_key.name)", r.TokenName, "编程")
	}
	if r.CacheReadTokens != 15872 {
		t.Errorf("CacheReadTokens = %d, want 15872", r.CacheReadTokens)
	}
	if r.Group != "自用" {
		t.Errorf("Group = %q, want %q (from real group.name)", r.Group, "自用")
	}
	if r.ModelName != "gpt-5.6-sol" {
		t.Errorf("ModelName = %q, want %q", r.ModelName, "gpt-5.6-sol")
	}
}

// TestSyncUsageHistoryDeduplicatesByRemoteLogID verifies second sync does not insert duplicates.
func TestSyncUsageHistoryDeduplicatesByRemoteLogID(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()

	created, err := Create(ctx, &model.RemoteSiteCreateRequest{
		Name:     "dedup-test",
		BaseURL:  "https://example.com",
		SiteType: model.SiteTypeSub2API,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	mock := &mockSiteAdapter{
		usageLogs: []hub.RemoteUsageLog{
			{
				ID: 42,
				// Relative to now, not an absolute date: SyncUsageHistory skips
				// entries older than usageLogMaxDays, so a pinned date makes this
				// test start failing once it drifts out of the window.
				CreatedAt:   time.Now().Add(-24 * time.Hour).Unix(),
				ModelName:   "gpt-3.5-turbo",
				TotalTokens: 10,
				Quota:       100,
			},
		},
	}
	hub.Register("sub2api-dedup-mock", mock)

	if err := internaldb.GetDB().Model(&model.RemoteSite{}).Where("id = ?", created.ID).Update("site_type", "sub2api-dedup-mock").Error; err != nil {
		t.Fatalf("update site type: %v", err)
	}

	n1, err := SyncUsageHistory(ctx, created.ID)
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if n1 != 1 {
		t.Fatalf("first sync inserted %d, want 1", n1)
	}

	n2, err := SyncUsageHistory(ctx, created.ID)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second sync inserted %d, want 0 (dedup)", n2)
	}

	records, total, err := QueryUsageHistory(ctx, &model.RemoteUsageQuery{SiteID: created.ID, Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(records) != 1 || total != 1 {
		t.Fatalf("after dedup: got %d records, total=%d; want 1/1", len(records), total)
	}
}

// seedMockSite creates a remote site wired to the given mock adapter and
// returns the new site ID. siteType must be unique per test to avoid clobbering
// the process-wide adapter registry.
func seedMockSite(t *testing.T, ctx context.Context, name, siteType string, mock *mockSiteAdapter) int {
	t.Helper()

	created, err := Create(ctx, &model.RemoteSiteCreateRequest{
		Name:     name,
		BaseURL:  "https://example.com",
		SiteType: model.SiteTypeSub2API,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	hub.Register(siteType, mock)
	if err := internaldb.GetDB().Model(&model.RemoteSite{}).
		Where("id = ?", created.ID).Update("site_type", siteType).Error; err != nil {
		t.Fatalf("update site type: %v", err)
	}
	return created.ID
}

// TestSyncUsageHistoryFlushesBatchBeforeAgeCutoff reproduces the production data
// shape that surfaced the bug: a page whose first entries are fresh and whose
// last entry falls outside the look-back window. The age cutoff must stop the
// scan without discarding the fresh records already accumulated for that page.
func TestSyncUsageHistoryFlushesBatchBeforeAgeCutoff(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()

	now := time.Now()
	fresh := func(id int64, ageHours int) hub.RemoteUsageLog {
		return hub.RemoteUsageLog{
			ID:          id,
			CreatedAt:   now.Add(-time.Duration(ageHours) * time.Hour).Unix(),
			ModelName:   "gpt-5.6-sol",
			TotalTokens: 10,
			Quota:       100,
		}
	}

	// 4 in-window records followed by 1 record older than usageLogMaxDays.
	mock := &mockSiteAdapter{
		usageLogs: []hub.RemoteUsageLog{
			fresh(105, 1),
			fresh(104, 2),
			fresh(103, 3),
			fresh(102, 4),
			fresh(101, (usageLogMaxDays+1)*24),
		},
	}
	siteID := seedMockSite(t, ctx, "age-cutoff", "sub2api-age-cutoff-mock", mock)

	n, err := SyncUsageHistory(ctx, siteID)
	if err != nil {
		t.Fatalf("SyncUsageHistory: %v", err)
	}
	if n != 4 {
		t.Fatalf("inserted %d, want 4 (the 4 in-window records must survive the age cutoff)", n)
	}

	records, total, err := QueryUsageHistory(ctx, &model.RemoteUsageQuery{SiteID: siteID, Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 4 || len(records) != 4 {
		t.Fatalf("persisted %d records (total=%d), want 4", len(records), total)
	}
	for _, r := range records {
		if r.RemoteLogID == 101 {
			t.Error("out-of-window record 101 was persisted, want it skipped")
		}
	}
}

// TestSyncUsageHistoryFlushesBatchBeforeLastSyncedCutoff covers the incremental
// path: a later sync sees new records ahead of already-synced ones on the same
// page. Hitting the already-synced marker must not drop the new records.
func TestSyncUsageHistoryFlushesBatchBeforeLastSyncedCutoff(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()

	now := time.Now()
	entry := func(id int64) hub.RemoteUsageLog {
		return hub.RemoteUsageLog{
			ID:          id,
			CreatedAt:   now.Add(-time.Duration(id) * time.Minute).Unix(),
			ModelName:   "gpt-5.6-sol",
			TotalTokens: 10,
			Quota:       100,
		}
	}

	mock := &mockSiteAdapter{usageLogs: []hub.RemoteUsageLog{entry(50)}}
	siteID := seedMockSite(t, ctx, "incremental", "sub2api-incremental-mock", mock)

	if n, err := SyncUsageHistory(ctx, siteID); err != nil || n != 1 {
		t.Fatalf("first sync: n=%d err=%v; want 1/nil", n, err)
	}

	// Remote now returns 3 newer records ahead of the already-synced id 50.
	mock.usageLogs = []hub.RemoteUsageLog{entry(53), entry(52), entry(51), entry(50)}

	n, err := SyncUsageHistory(ctx, siteID)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if n != 3 {
		t.Fatalf("second sync inserted %d, want 3 (new records before the already-synced marker)", n)
	}

	_, total, err := QueryUsageHistory(ctx, &model.RemoteUsageQuery{SiteID: siteID, Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if total != 4 {
		t.Fatalf("total records = %d, want 4", total)
	}
}

// TestSyncUsageHistoryStopsScanningAfterCutoff verifies the cutoff still ends
// the scan: once an out-of-window entry is seen, no further page is fetched.
func TestSyncUsageHistoryStopsScanningAfterCutoff(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()

	now := time.Now()
	page0 := make([]hub.RemoteUsageLog, 0, usageLogPageSize)
	for i := 0; i < usageLogPageSize-1; i++ {
		page0 = append(page0, hub.RemoteUsageLog{
			ID:          int64(1000 - i),
			CreatedAt:   now.Add(-time.Duration(i+1) * time.Minute).Unix(),
			ModelName:   "gpt-5.6-sol",
			TotalTokens: 10,
			Quota:       100,
		})
	}
	// Full page, last entry out of window -> scan must stop here.
	page0 = append(page0, hub.RemoteUsageLog{
		ID:          1,
		CreatedAt:   now.Add(-time.Duration(usageLogMaxDays+1) * 24 * time.Hour).Unix(),
		ModelName:   "gpt-5.6-sol",
		TotalTokens: 10,
		Quota:       100,
	})

	mock := &mockSiteAdapter{pages: [][]hub.RemoteUsageLog{page0, {}}}
	siteID := seedMockSite(t, ctx, "cutoff-stops", "sub2api-cutoff-stop-mock", mock)

	n, err := SyncUsageHistory(ctx, siteID)
	if err != nil {
		t.Fatalf("SyncUsageHistory: %v", err)
	}
	if n != usageLogPageSize-1 {
		t.Fatalf("inserted %d, want %d", n, usageLogPageSize-1)
	}
	if mock.fetchCall != 1 {
		t.Errorf("fetched %d pages, want 1 (cutoff must stop pagination)", mock.fetchCall)
	}
}
