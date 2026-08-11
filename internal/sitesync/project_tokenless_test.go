package sitesync

import (
	"testing"

	dbpkg "github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
)

// TestProjectAccountOnTokenlessAccountCreatesNothing pins the precondition that
// makes ProjectAccount a no-op right after account creation.
//
// A freshly created site account has zero site_tokens and zero site_models: the
// create payload carries no tokens (the API type omits the field) and
// op.SiteAccountCreate only inserts the account row. ProjectAccount then filters
// desiredKeys on `len(tokenGroups[groupKey]) > 0` (project.go:116-121), so with
// no tokens there is nothing to project.
//
// This is why calling ProjectAccount from createSiteAccount cannot, on its own,
// make a managed channel appear. The channel is created by the SyncAccount path,
// which writes site_tokens first and then projects. Guarding this keeps anyone
// from "fixing" a missing-channel report by adding another ProjectAccount call.
func TestProjectAccountOnTokenlessAccountCreatesNothing(t *testing.T) {
	ctx := setupProjectTestDB(t)

	site := &model.Site{
		Name:     "tokenless-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := op.SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	// Mirrors what the create handler persists: credentials only, no tokens.
	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "tokenless-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "sk-access-token",
		Enabled:        true,
		AutoSync:       true,
	}
	if err := op.SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	// Precondition: the account really has no tokens yet.
	var tokenCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteToken{}).
		Where("site_account_id = ?", account.ID).Count(&tokenCount).Error; err != nil {
		t.Fatalf("count site_tokens: %v", err)
	}
	if tokenCount != 0 {
		t.Fatalf("site_tokens for a freshly created account = %d, want 0", tokenCount)
	}

	channelIDs, err := ProjectAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("ProjectAccount returned error: %v", err)
	}
	if len(channelIDs) != 0 {
		t.Fatalf("ProjectAccount projected %d channels for a token-less account, want 0", len(channelIDs))
	}

	var bindingCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteChannelBinding{}).
		Where("site_account_id = ?", account.ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count site_channel_bindings: %v", err)
	}
	if bindingCount != 0 {
		t.Fatalf("site_channel_bindings = %d, want 0", bindingCount)
	}

	var channelCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.Channel{}).Count(&channelCount).Error; err != nil {
		t.Fatalf("count channels: %v", err)
	}
	if channelCount != 0 {
		t.Fatalf("channels = %d, want 0: no channel can exist without tokens", channelCount)
	}
}

// TestProjectAccountSkipsGroupsWithOnlyMaskedTokens pins the second, independent
// defect behind "站点管理 shows a key but the channel has none".
//
// buildChannelKeys drops tokens that are masked or not ready (project.go:354-366),
// but the enclosing loop decides whether to create — and ENABLE — the channel
// from `len(groupTokens) > 0` (project.go:135), which counts those same dropped
// tokens. So a group whose only token is masked yields an enabled managed channel
// with zero usable keys: it looks healthy, and every request through it fails.
func TestProjectAccountSkipsGroupsWithOnlyMaskedTokens(t *testing.T) {
	ctx := setupProjectTestDB(t)

	site := &model.Site{
		Name:     "masked-token-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := op.SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "masked-token-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "sk-access-token",
		Enabled:        true,
		AutoSync:       true,
	}
	if err := op.SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	// A masked token is what the upstream returns when it refuses to disclose the
	// key value; sk-**** is the shape maskProjectedChannelKey/IsMasked recognise.
	maskedToken := model.SiteToken{
		SiteAccountID: account.ID,
		Name:          "masked-key",
		Token:         "sk-abc***************xyz",
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       true,
		ValueStatus:   model.SiteTokenValueStatusMaskedPending,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&maskedToken).Error; err != nil {
		t.Fatalf("create masked site token: %v", err)
	}

	siteModel := model.SiteModel{
		SiteAccountID: account.ID,
		ModelName:     "gpt-4o-mini",
		GroupKey:      model.SiteDefaultGroupKey,
		RouteType:     model.SiteModelRouteTypeOpenAIChat,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&siteModel).Error; err != nil {
		t.Fatalf("create site model: %v", err)
	}

	if _, err := ProjectAccount(ctx, account.ID); err != nil {
		t.Fatalf("ProjectAccount returned error: %v", err)
	}

	var channels []model.Channel
	if err := dbpkg.GetDB().WithContext(ctx).Preload("Keys").Find(&channels).Error; err != nil {
		t.Fatalf("load channels: %v", err)
	}

	// Report the exact shape so a regression is self-explaining.
	for _, channel := range channels {
		if channel.Enabled && len(channel.Keys) == 0 {
			t.Fatalf("channel %q is enabled with 0 keys: a masked-only token group must not yield an enabled keyless channel (站点管理 counts the masked token, buildChannelKeys drops it)", channel.Name)
		}
	}
}
