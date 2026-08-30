package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
)

// The B2 tests all create a channel WITHOUT keys, so the path where both
// compensations run (channel disable + KeysForceDisabled, whose RefreshCacheByID
// reloads the channel from the DB) is never exercised. That is the ordering the
// code comment claims is load-bearing. This covers the combined case.
func TestCreateChannelDisabledChannelWithDisabledKey(t *testing.T) {
	setupChannelCreateHandlerTest(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := `{"name":"zz-combined","type":0,"enabled":false,"model":"gpt-4o-mini",` +
		`"keys":[{"channel_key":"sk-zz-off","enabled":false,"remark":"off"},` +
		`{"channel_key":"sk-zz-on","enabled":true,"remark":"on"}]}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/channel/create", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	createChannel(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Code int           `json:"code"`
		Data model.Channel `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Logf("response: channel.Enabled=%v keys=%d", response.Data.Enabled, len(response.Data.Keys))
	if response.Data.Enabled {
		t.Errorf("response says channel enabled; asked for false")
	}

	saved := loadChannelRowByName(t, "zz-combined")
	t.Logf("DB: channel.Enabled=%v", saved.Enabled)
	if saved.Enabled {
		t.Errorf("DB says channel enabled; asked for false")
	}

	cached, err := op.ChannelGet(saved.ID, context.Background())
	if err != nil {
		t.Fatalf("ChannelGet: %v", err)
	}
	t.Logf("CACHE: channel.Enabled=%v keys=%d", cached.Enabled, len(cached.Keys))
	if cached.Enabled {
		t.Errorf("CACHE says channel enabled while DB says disabled -- ordering bug")
	}
	for _, k := range cached.Keys {
		t.Logf("CACHE key remark=%q enabled=%v", k.Remark, k.Enabled)
		if k.Remark == "off" && k.Enabled {
			t.Errorf("CACHE key %q enabled; asked for false", k.Remark)
		}
		if k.Remark == "on" && !k.Enabled {
			t.Errorf("CACHE key %q disabled; asked for true", k.Remark)
		}
	}
}
