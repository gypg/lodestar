package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
)

// newChannelGetTestDB gives each test its own in-memory database plus a warm
// channel cache, mirroring the setup the sibling channel handler tests use.
func newChannelGetTestDB(t *testing.T) context.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)

	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", testName)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return context.Background()
}

func seedChannelForGet(t *testing.T, ctx context.Context, name string) *model.Channel {
	t.Helper()
	channel := &model.Channel{
		Name:      name,
		Type:      0,
		Enabled:   true,
		BaseUrls:  []model.BaseUrl{{URL: "https://upstream.example.com", Delay: 0}},
		Model:     "gpt-4o",
		AutoGroup: model.AutoGroupTypeNone,
		Keys: []model.ChannelKey{
			{Enabled: true, ChannelKey: "sk-secret-12345678", Remark: "primary"},
		},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return channel
}

func decodeChannelResponse(t *testing.T, body string) model.Channel {
	t.Helper()
	var response struct {
		Code int           `json:"code"`
		Data model.Channel `json:"data"`
	}
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&response); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, body)
	}
	return response.Data
}

// The production route-table guard for GET /api/v1/channel/:id lives in
// internal/server (channel_get_route_test.go), not here: router.RegisterAll
// nils the global table on completion (router.go:124), rbac_test.go already
// consumes the single allowed call in this package, and a second call would
// hand back an empty table — making every assertion order-dependent.
func TestGetChannelReturnsSingleChannel(t *testing.T) {
	ctx := newChannelGetTestDB(t)
	channel := seedChannelForGet(t, ctx, "airoute-target")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/channel/%d", channel.ID), nil)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(channel.ID)}}
	c.Set("user_role", model.UserRoleAdmin)

	getChannel(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	got := decodeChannelResponse(t, recorder.Body.String())
	if got.ID != channel.ID {
		t.Fatalf("id = %d, want %d", got.ID, channel.ID)
	}
	// The whole point of the endpoint: the caller needs base_urls and keys to
	// auto-fill the AI route config, which is exactly what the cached list
	// failed to provide.
	if len(got.BaseUrls) != 1 || got.BaseUrls[0].URL != "https://upstream.example.com" {
		t.Fatalf("base_urls = %+v, want the seeded upstream URL", got.BaseUrls)
	}
	if len(got.Keys) != 1 {
		t.Fatalf("keys = %+v, want exactly 1", got.Keys)
	}
	if got.Keys[0].ChannelKey != "sk-secret-12345678" {
		t.Fatalf("admin must receive the raw key, got %q", got.Keys[0].ChannelKey)
	}
}

func TestGetChannelMasksKeysForViewer(t *testing.T) {
	ctx := newChannelGetTestDB(t)
	channel := seedChannelForGet(t, ctx, "viewer-single")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/channel/%d", channel.ID), nil)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(channel.ID)}}
	c.Set("user_role", model.UserRoleViewer)

	getChannel(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	got := decodeChannelResponse(t, recorder.Body.String())
	if len(got.Keys) != 1 {
		t.Fatalf("keys = %+v, want exactly 1", got.Keys)
	}
	if got.Keys[0].ChannelKey == "sk-secret-12345678" {
		t.Fatalf("viewer received the raw key %q", got.Keys[0].ChannelKey)
	}
	if !strings.HasPrefix(got.Keys[0].ChannelKey, "sk-s") || !strings.HasSuffix(got.Keys[0].ChannelKey, "5678") {
		t.Fatalf("expected masked key to retain edges, got %q", got.Keys[0].ChannelKey)
	}
	// listChannel redacts the domain for viewers; the single-channel endpoint
	// must not become a way around that.
	if len(got.BaseUrls) != 1 {
		t.Fatalf("base_urls = %+v, want exactly 1", got.BaseUrls)
	}
	if got.BaseUrls[0].URL == "https://upstream.example.com" {
		t.Fatalf("viewer received the unredacted upstream URL %q", got.BaseUrls[0].URL)
	}
}

func TestGetChannelRejectsNonNumericID(t *testing.T) {
	_ = newChannelGetTestDB(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channel/not-a-number", nil)
	c.Params = gin.Params{{Key: "id", Value: "not-a-number"}}
	c.Set("user_role", model.UserRoleAdmin)

	getChannel(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestGetChannelReturnsNotFoundForUnknownID(t *testing.T) {
	_ = newChannelGetTestDB(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channel/424242", nil)
	c.Params = gin.Params{{Key: "id", Value: "424242"}}
	c.Set("user_role", model.UserRoleAdmin)

	getChannel(c)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
}
