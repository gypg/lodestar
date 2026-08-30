package sitesync

import (
	"context"
	"testing"

	dbpkg "github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/transformer/outbound"
)

// TestProjectAccountProjectsDefaultGroupWhenSiblingGroupHasNoKeys mirrors the
// production state of the "主站" new-api site (as of 2026-08-30): two synced
// user groups where only default owns a ready token, vip has none, and five
// openai_chat models with /api/pricing route metadata all live in default.
// The default group must still project exactly one managed channel; an empty
// sibling group must not take the whole account's projection down.
func TestProjectAccountProjectsDefaultGroupWhenSiblingGroupHasNoKeys(t *testing.T) {
	ctx := setupProjectTestDB(t)

	site := &model.Site{
		Name:         "主站",
		Platform:     model.SitePlatformNewAPI,
		BaseURL:      "https://api.ggznb.xyz",
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
		AccessToken:    "site-access-token",
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

	// 48-char ready plaintext key, source=sync, is_default=true — exactly what
	// production site_tokens holds for account 3 (id=274).
	token := model.SiteToken{
		SiteAccountID: account.ID,
		UpstreamID:    6,
		Name:          "test",
		Token:         "sk-" + string([]rune("0123456789abcdefghijklmnopqrstuvwxyz0123456789ab")),
		ValueStatus:   model.SiteTokenValueStatusReady,
		GroupKey:      "default",
		GroupName:     "default",
		Enabled:       true,
		Source:        "sync",
		IsDefault:     true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&token).Error; err != nil {
		t.Fatalf("create site token failed: %v", err)
	}

	pricingPayload := `{"kind":"site_route_metadata","version":1,"source":"/api/pricing","route_supported":true,"route_type":"openai_chat","enable_groups":["default"],"supported_endpoint_types":["openai"],"normalized_endpoint_types":["openai_chat"]}`
	modelNames := []string{
		"deepseek-ai/deepseek-v4-flash-0731",
		"minimaxai/minimax-m3",
		"moonshotai/kimi-k2.6",
		"stepfun-ai/step-3.7-flash",
		"moonshotai/kimi-k3",
	}
	models := make([]model.SiteModel, 0, len(modelNames))
	for _, name := range modelNames {
		models = append(models, model.SiteModel{
			SiteAccountID:   account.ID,
			GroupKey:        "default",
			ModelName:       name,
			Source:          "sync",
			RouteType:       model.SiteModelRouteTypeOpenAIChat,
			RouteSource:     model.SiteModelRouteSourceSyncInferred,
			RouteRawPayload: pricingPayload,
		})
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&models).Error; err != nil {
		t.Fatalf("create site models failed: %v", err)
	}

	channelIDs, err := ProjectAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("ProjectAccount returned error: %v", err)
	}
	if len(channelIDs) != 1 {
		t.Fatalf("expected exactly 1 managed channel for the default group, got %d (ids=%v)", len(channelIDs), channelIDs)
	}

	channelsByGroup := loadProjectedChannelsByGroupKey(t, ctx, account.ID)
	channel, ok := channelsByGroup["default"]
	if !ok {
		t.Fatalf("expected a projected channel bound to group key %q, got bindings %v", "default", bindingKeys(t, ctx, account.ID))
	}
	if channel.Name != "主站/主站/default-Chat" {
		t.Fatalf("expected channel name %q, got %q", "主站/主站/default-Chat", channel.Name)
	}
	if channel.Type != outbound.OutboundTypeOpenAIChat {
		t.Fatalf("expected channel type openai chat, got %q", channel.Type)
	}
	if !channel.Enabled {
		t.Fatalf("expected projected channel to be enabled")
	}
	if len(channel.Keys) != 1 {
		t.Fatalf("expected projected channel to carry exactly 1 key, got %d", len(channel.Keys))
	}
	if channel.Keys[0].ChannelKey != token.Token {
		t.Fatalf("expected projected key to equal the ready token value, got %q", channel.Keys[0].ChannelKey)
	}
	if channel.Model != "deepseek-ai/deepseek-v4-flash-0731,minimaxai/minimax-m3,moonshotai/kimi-k2.6,moonshotai/kimi-k3,stepfun-ai/step-3.7-flash" {
		t.Fatalf("expected all five default-group models on the channel, got %q", channel.Model)
	}
}

func bindingKeys(t *testing.T, ctx context.Context, accountID int) []string {
	t.Helper()
	var bindings []model.SiteChannelBinding
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ?", accountID).Find(&bindings).Error; err != nil {
		t.Fatalf("load bindings failed: %v", err)
	}
	keys := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		keys = append(keys, binding.GroupKey)
	}
	return keys
}
