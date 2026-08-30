package sitesync

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dbpkg "github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
)

// TestSyncAccountProjectsChannelFromRealNewAPIUpstreamShape replays the exact
// wire shapes observed on the production upstream (api.ggznb.xyz) on top of the
// exact production DB state: the token list masks keys, the plaintext comes
// back from /api/token/batch/keys, /api/user/self/groups returns a map, and
// /api/pricing carries enable_groups metadata. A full SyncAccount must leave
// the default group with exactly one managed channel.
func TestSyncAccountProjectsChannelFromRealNewAPIUpstreamShape(t *testing.T) {
	ctx := setupProjectTestDB(t)

	const (
		upstreamTokenID = 6
		plaintextKey    = "QPPlaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaHaYC" // 48 chars, no mask
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
	sessionModelsPayload := make([]string, 0, len(modelNames))
	for _, name := range modelNames {
		sessionModelsPayload = append(sessionModelsPayload, `"`+name+`"`)
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
		case r.URL.Path == "/api/user/models":
			_, _ = w.Write([]byte(`{"success":true,"data":[` + strings.Join(sessionModelsPayload, ",") + `]}`))
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
	token := model.SiteToken{
		SiteAccountID: account.ID,
		UpstreamID:    upstreamTokenID,
		Name:          "test",
		Token:         plaintextKey,
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

	result, err := SyncAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("SyncAccount returned error: %v", err)
	}
	if result.Status != model.SiteExecutionStatusPartial {
		t.Fatalf("expected partial sync status (vip has no key), got %q (%s)", result.Status, result.Message)
	}
	if result.ChannelCount != 1 {
		t.Fatalf("expected SyncAccount to project exactly 1 channel, got %d (message=%q)", result.ChannelCount, result.Message)
	}

	channelsByGroup := loadProjectedChannelsByGroupKey(t, ctx, account.ID)
	channel, ok := channelsByGroup["default"]
	if !ok {
		t.Fatalf("expected a projected channel bound to %q, got bindings %v", "default", bindingKeys(t, ctx, account.ID))
	}
	if len(channel.Keys) != 1 || channel.Keys[0].ChannelKey != "sk-"+plaintextKey {
		t.Fatalf("expected projected channel to carry the resolved plaintext key, got %+v", channel.Keys)
	}
}

func intPtr(v int) *int { return &v }

func itoa(v int) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
