package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	ch "github.com/gypg/lodestar/internal/op/channel"
)

// The channel cache hands out shallow struct copies: cache.shard.get returns
// the stored value, so BaseUrls and Keys still point at the cache's backing
// arrays. redactChannelBaseURLsForViewer rewrites BaseUrls[j].URL in place, so
// any viewer-role read would rewrite the LIVE cache entry that the relay routes
// on, not just the response payload.
//
// These tests pin that the viewer redaction stays confined to the response.

func seedChannelForCachePoisonTest(t *testing.T, ctx context.Context) *model.Channel {
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

	channel := &model.Channel{
		Name:      "cache-poison-probe",
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

// assertCacheStillHasRealURL reads straight from the channel cache — the same
// source the relay uses to pick an upstream — and fails if the viewer-facing
// redaction leaked into it.
func assertCacheStillHasRealURL(t *testing.T, ctx context.Context, id int) {
	t.Helper()
	cached, err := ch.Get(id, ctx)
	if err != nil {
		t.Fatalf("re-read channel from cache: %v", err)
	}
	if len(cached.BaseUrls) != 1 {
		t.Fatalf("cached base_urls = %+v, want exactly 1", cached.BaseUrls)
	}
	if cached.BaseUrls[0].URL != "https://upstream.example.com" {
		t.Fatalf("viewer redaction leaked into the live channel cache: cached URL = %q, want %q — the relay would now route to a masked host",
			cached.BaseUrls[0].URL, "https://upstream.example.com")
	}
}

func TestGetChannelViewerRedactionDoesNotPoisonCache(t *testing.T) {
	ctx := context.Background()
	channel := seedChannelForCachePoisonTest(t, ctx)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/channel/%d", channel.ID), nil)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(channel.ID)}}
	c.Set("user_role", model.UserRoleViewer)

	getChannel(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	// The response itself must still be redacted...
	got := decodeChannelResponse(t, recorder.Body.String())
	if len(got.BaseUrls) != 1 || got.BaseUrls[0].URL == "https://upstream.example.com" {
		t.Fatalf("viewer response was not redacted: %+v", got.BaseUrls)
	}
	// ...while the cache keeps the real upstream.
	assertCacheStillHasRealURL(t, ctx, channel.ID)
}

func TestListChannelViewerRedactionDoesNotPoisonCache(t *testing.T) {
	ctx := context.Background()
	channel := seedChannelForCachePoisonTest(t, ctx)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channel/list", nil)
	c.Set("user_role", model.UserRoleViewer)

	listChannel(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	assertCacheStillHasRealURL(t, ctx, channel.ID)
}

// A second viewer read must still be redacted. If the first read had mutated
// the cache, this would pass for the wrong reason (already-masked value), so
// this asserts the redaction is recomputed from a clean source every time.
func TestListChannelViewerRedactionIsRepeatable(t *testing.T) {
	ctx := context.Background()
	channel := seedChannelForCachePoisonTest(t, ctx)

	for attempt := 1; attempt <= 2; attempt++ {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channel/list", nil)
		c.Set("user_role", model.UserRoleViewer)

		listChannel(c)

		if recorder.Code != http.StatusOK {
			t.Fatalf("attempt %d: status = %d; body=%s", attempt, recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "upstream.example.com") {
			t.Fatalf("attempt %d: viewer response leaked the real upstream host", attempt)
		}
	}

	assertCacheStillHasRealURL(t, ctx, channel.ID)
}

// An admin read after a viewer read must still see the real upstream. This is
// the user-visible half of cache poisoning: the admin's channel editor would
// otherwise show (and then save back) the masked host.
func TestAdminSeesRealURLAfterViewerRead(t *testing.T) {
	ctx := context.Background()
	channel := seedChannelForCachePoisonTest(t, ctx)

	viewerRecorder := httptest.NewRecorder()
	viewerCtx, _ := gin.CreateTestContext(viewerRecorder)
	viewerCtx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channel/list", nil)
	viewerCtx.Set("user_role", model.UserRoleViewer)
	listChannel(viewerCtx)

	adminRecorder := httptest.NewRecorder()
	adminCtx, _ := gin.CreateTestContext(adminRecorder)
	adminCtx.Request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/channel/%d", channel.ID), nil)
	adminCtx.Params = gin.Params{{Key: "id", Value: fmt.Sprint(channel.ID)}}
	adminCtx.Set("user_role", model.UserRoleAdmin)
	getChannel(adminCtx)

	if adminRecorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", adminRecorder.Code, http.StatusOK, adminRecorder.Body.String())
	}
	got := decodeChannelResponse(t, adminRecorder.Body.String())
	if len(got.BaseUrls) != 1 {
		t.Fatalf("base_urls = %+v, want exactly 1", got.BaseUrls)
	}
	if got.BaseUrls[0].URL != "https://upstream.example.com" {
		t.Fatalf("admin saw %q after a viewer read, want the real upstream", got.BaseUrls[0].URL)
	}
}
