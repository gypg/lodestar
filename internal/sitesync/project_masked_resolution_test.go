package sitesync

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dbpkg "github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
)

// TestSyncAccountEnablesResolvedMaskedKeyAndProjectsChannel reproduces the
// production incident: the account's only token sat as masked_pending and
// disabled (the pre-efb1bf7 state), then the deploying build started resolving
// the plaintext automatically. The merge inherited enabled=false from the
// masked row, so the projection still found zero usable keys and silently
// produced no channel -- no folder, no error, no log line.
//
// After a successful automatic resolution the key must come back enabled (the
// upstream reports the token as active) and the default group must project its
// managed channel.
func TestSyncAccountEnablesResolvedMaskedKeyAndProjectsChannel(t *testing.T) {
	ctx := setupProjectTestDB(t)

	const (
		upstreamTokenID = 6
		plaintextKey    = "QPPlaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaHaYC" // 48 chars
		maskedKey       = "QPPl**********HaYC"
	)
	modelNames := []string{
		"deepseek-ai/deepseek-v4-flash-0731",
		"minimaxai/minimax-m3",
		"moonshotai/kimi-k2.6",
		"stepfun-ai/step-3.7-flash",
		"moonshotai/kimi-k3",
	}
	modelsPayload := make([]string, 0, len(modelNames))
	for _, name := range modelNames {
		modelsPayload = append(modelsPayload, `{"id":"`+name+`"}`)
	}
	pricingItems := make([]string, 0, len(modelNames))
	for _, name := range modelNames {
		pricingItems = append(pricingItems, `{"model_name":"`+name+`","quota_type":0,"model_ratio":1,"completion_ratio":1,"enable_groups":["default"],"supported_endpoint_types":["openai"]}`)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/user/self":
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":1,"username":"managed-user"}}`))
		case r.URL.Path == "/api/token/":
			_, _ = w.Write([]byte(`{"data":{"page":1,"page_size":10,"total":1,"items":[{"id":` +
				itoa(upstreamTokenID) + `,"user_id":1,"key":"` + maskedKey + `","status":1,"name":"test","group":"default","unlimited_quota":true}]},"message":"","success":true}`))
		case r.URL.Path == "/api/token/batch/keys":
			_, _ = w.Write([]byte(`{"data":{"keys":{"` + itoa(upstreamTokenID) + `":"` + plaintextKey + `"}},"message":"","success":true}`))
		case r.URL.Path == "/api/token/6/key":
			_, _ = w.Write([]byte(`{"data":{"key":"` + plaintextKey + `"},"message":"","success":true}`))
		case r.URL.Path == "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"data":{"default":{"desc":"默认分组","ratio":1},"vip":{"desc":"vip分组","ratio":1}},"message":"","success":true}`))
		case r.URL.Path == "/api/pricing":
			_, _ = w.Write([]byte(`{"auto_groups":["default"],"data":[` + strings.Join(pricingItems, ",") + `],"group_ratio":{"default":1,"vip":1},"success":true}`))
		case r.URL.Path == "/models" || r.URL.Path == "/v1/models":
			_, _ = w.Write([]byte(`{"data":[` + strings.Join(modelsPayload, ",") + `]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := &model.Site{
		Name:         "主站",
		Platform:     model.SitePlatformNewAPI,
		BaseURL:      server.URL,
		Enabled:      true,
		CustomHeader: []model.CustomHeader{},
	}
	if err := op.SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}
	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "主站",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "test-access-token",
		PlatformUserID: intPtr(1),
		Enabled:        true,
		AutoSync:       true,
		AutoCheckin:    false,
	}
	if err := op.SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}
	groups := []model.SiteUserGroup{
		{SiteAccountID: account.ID, GroupKey: "default", Name: "default"},
		{SiteAccountID: account.ID, GroupKey: "vip", Name: "vip"},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&groups).Error; err != nil {
		t.Fatalf("create site user groups failed: %v", err)
	}

	// The pre-deploy state: the old binary stored the masked value verbatim and
	// force-disabled the row. No upstream id was recorded back then.
	token := model.SiteToken{
		SiteAccountID: account.ID,
		UpstreamID:    0,
		Name:          "test",
		Token:         maskedKey,
		ValueStatus:   model.SiteTokenValueStatusMaskedPending,
		GroupKey:      "default",
		GroupName:     "default",
		Enabled:       false,
		Source:        "sync",
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&token).Error; err != nil {
		t.Fatalf("create site token failed: %v", err)
	}

	result, err := SyncAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("SyncAccount returned error: %v", err)
	}
	if result.Status != model.SiteExecutionStatusPartial {
		t.Fatalf("expected partial sync status (vip has no key), got %q (%s)", result.Status, result.Message)
	}

	var stored model.SiteToken
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ?", account.ID).First(&stored).Error; err != nil {
		t.Fatalf("load stored token failed: %v", err)
	}
	if stored.Token != plaintextKey {
		t.Fatalf("expected sync to resolve the masked value to plaintext, got length %d masked=%v", len(stored.Token), strings.Contains(stored.Token, "*"))
	}
	if !stored.Enabled {
		t.Fatalf("expected the resolved key to be enabled (upstream reports the token active), got enabled=false")
	}
	if stored.ValueStatus != model.SiteTokenValueStatusReady {
		t.Fatalf("expected value_status ready after resolution, got %q", stored.ValueStatus)
	}

	if result.ChannelCount != 1 {
		t.Fatalf("expected the default group to project exactly 1 channel after resolution, got %d (message=%q)", result.ChannelCount, result.Message)
	}
	channelsByGroup := loadProjectedChannelsByGroupKey(t, ctx, account.ID)
	channel, ok := channelsByGroup["default"]
	if !ok {
		t.Fatalf("expected a projected channel bound to %q, got bindings %v", "default", bindingKeys(t, ctx, account.ID))
	}
	if !channel.Enabled {
		t.Fatalf("expected the projected channel to be enabled")
	}
	if len(channel.Keys) != 1 || channel.Keys[0].ChannelKey != "sk-"+plaintextKey {
		t.Fatalf("expected the projected channel to carry the resolved plaintext key, got %+v", channel.Keys)
	}
	if channel.Model != "deepseek-ai/deepseek-v4-flash-0731,minimaxai/minimax-m3,moonshotai/kimi-k2.6,moonshotai/kimi-k3,stepfun-ai/step-3.7-flash" {
		t.Fatalf("expected all five default-group models on the channel, got %q", channel.Model)
	}
}

// TestSyncAccountKeepsUserDisabledTokenDisabled pins the other half of the
// enabled contract: an operator who disabled an already-ready token must not
// have that decision silently overturned by the next sync (the upstream
// reporting the token active does not override a deliberate local disable).
func TestSyncAccountKeepsUserDisabledTokenDisabled(t *testing.T) {
	ctx := setupProjectTestDB(t)

	const (
		upstreamTokenID = 6
		plaintextKey    = "QPPlaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaHaYC"
		maskedKey       = "QPPl**********HaYC"
	)
	modelsPayload := `{"id":"deepseek-ai/deepseek-v4-flash-0731"}`
	pricingItems := `{"model_name":"deepseek-ai/deepseek-v4-flash-0731","quota_type":0,"model_ratio":1,"completion_ratio":1,"enable_groups":["default"],"supported_endpoint_types":["openai"]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/user/self":
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":1,"username":"managed-user"}}`))
		case r.URL.Path == "/api/token/":
			_, _ = w.Write([]byte(`{"data":{"page":1,"page_size":10,"total":1,"items":[{"id":` +
				itoa(upstreamTokenID) + `,"user_id":1,"key":"` + maskedKey + `","status":1,"name":"test","group":"default","unlimited_quota":true}]},"message":"","success":true}`))
		case r.URL.Path == "/api/token/batch/keys":
			_, _ = w.Write([]byte(`{"data":{"keys":{"` + itoa(upstreamTokenID) + `":"` + plaintextKey + `"}},"message":"","success":true}`))
		case r.URL.Path == "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"data":{"default":{"desc":"默认分组","ratio":1}},"message":"","success":true}`))
		case r.URL.Path == "/api/pricing":
			_, _ = w.Write([]byte(`{"auto_groups":["default"],"data":[` + pricingItems + `],"group_ratio":{"default":1},"success":true}`))
		case r.URL.Path == "/models" || r.URL.Path == "/v1/models":
			_, _ = w.Write([]byte(`{"data":[` + modelsPayload + `]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := &model.Site{
		Name:         "主站",
		Platform:     model.SitePlatformNewAPI,
		BaseURL:      server.URL,
		Enabled:      true,
		CustomHeader: []model.CustomHeader{},
	}
	if err := op.SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}
	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "主站",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "test-access-token",
		PlatformUserID: intPtr(1),
		Enabled:        true,
		AutoSync:       true,
		AutoCheckin:    false,
	}
	if err := op.SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	// The operator deliberately disabled this ready token. Seeded via a map so
	// the row actually lands disabled -- a struct Create would hit the same
	// gorm default:true substitution this test exists to catch.
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteToken{}).Create(map[string]any{
		"site_account_id": account.ID,
		"upstream_id":     upstreamTokenID,
		"name":            "test",
		"token":           plaintextKey,
		"value_status":    model.SiteTokenValueStatusReady,
		"group_key":       "default",
		"group_name":      "default",
		"enabled":         false,
		"source":          "sync",
	}).Error; err != nil {
		t.Fatalf("create site token failed: %v", err)
	}

	if _, err := SyncAccount(ctx, account.ID); err != nil {
		t.Fatalf("SyncAccount returned error: %v", err)
	}

	var stored model.SiteToken
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ?", account.ID).First(&stored).Error; err != nil {
		t.Fatalf("load stored token failed: %v", err)
	}
	if stored.Enabled {
		t.Fatalf("expected the user-disabled token to stay disabled after sync, got enabled=true")
	}
	if stored.ValueStatus != model.SiteTokenValueStatusReady {
		t.Fatalf("expected value_status to stay ready, got %q", stored.ValueStatus)
	}

	var bindingCount int64
	if err := dbpkg.GetDB().WithContext(ctx).
		Model(&model.SiteChannelBinding{}).
		Where("site_account_id = ?", account.ID).
		Count(&bindingCount).Error; err != nil {
		t.Fatalf("count site channel bindings failed: %v", err)
	}
	if bindingCount != 0 {
		t.Fatalf("expected no projected channels from a disabled token, got %d bindings", bindingCount)
	}
}

// TestProjectAccountDoesNotEnsureChannelFolderWithoutProjectableModels pins the
// folder behaviour: a group with a usable token but zero projectable models
// must not leave an empty site folder behind (same rule as the no-usable-token
// case documented in ProjectAccount).
func TestProjectAccountDoesNotEnsureChannelFolderWithoutProjectableModels(t *testing.T) {
	ctx := setupProjectTestDB(t)
	site, account := createProjectionFixture(t, ctx)

	// All models explicitly belong to a group that is neither projected for this
	// account nor present among the account's groups: the default group keeps
	// its usable token but loses every projectable model.
	payload := model.SiteModelRouteMetadata{
		Source:         "/api/pricing",
		RouteSupported: true,
		RouteType:      model.SiteModelRouteTypeOpenAIChat,
		EnableGroups:   []string{"foreign-group"},
	}.Marshal()
	if err := dbpkg.GetDB().WithContext(ctx).
		Model(&model.SiteModel{}).
		Where("site_account_id = ?", account.ID).
		Update("route_raw_payload", payload).Error; err != nil {
		t.Fatalf("update site model payload failed: %v", err)
	}

	channelIDs, err := ProjectAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("ProjectAccount returned error: %v", err)
	}
	if len(channelIDs) != 0 {
		t.Fatalf("expected no managed channels without projectable models, got %v", channelIDs)
	}

	var folderCount int64
	if err := dbpkg.GetDB().WithContext(ctx).
		Model(&model.ChannelGroup{}).
		Where("name = ?", site.Name).
		Count(&folderCount).Error; err != nil {
		t.Fatalf("count channel folders failed: %v", err)
	}
	if folderCount != 0 {
		t.Fatalf("expected no site channel folder without projectable models, got %d", folderCount)
	}
}
