package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
)

// Channel.Enabled 也是 default:true 裸 bool：创建启用态之外的任何意图都会被
// create 回调吞成 true。三条测试真走 JSON 绑定，断言覆盖 DB、缓存（op.ChannelGet）
// 与响应体三个面。

func TestCreateChannelPersistsExplicitlyDisabledChannelAsDisabled(t *testing.T) {
	setupChannelCreateHandlerTest(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := `{"name":"b2-disabled-channel","type":0,"enabled":false,"model":"gpt-4o-mini"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/channel/create", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	createChannel(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response struct {
		Code int           `json:"code"`
		Data model.Channel `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode create response failed: %v", err)
	}
	if response.Data.Enabled {
		t.Fatalf("create response reports enabled=true; caller asked for false")
	}

	saved := loadChannelRowByName(t, "b2-disabled-channel")
	if saved.Enabled {
		t.Fatalf("user asked for enabled=false; stored enabled=true")
	}

	cached, err := op.ChannelGet(saved.ID, context.Background())
	if err == nil && cached.Enabled {
		t.Fatalf("cache reports the explicitly disabled channel as enabled")
	}
}

func TestCreateChannelPersistsExplicitlyEnabledChannelAsEnabled(t *testing.T) {
	setupChannelCreateHandlerTest(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := `{"name":"b2-enabled-channel","type":0,"enabled":true,"model":"gpt-4o-mini"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/channel/create", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	createChannel(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	saved := loadChannelRowByName(t, "b2-enabled-channel")
	if !saved.Enabled {
		t.Fatalf("user asked for enabled=true; stored enabled=false")
	}
}

func TestCreateChannelWithoutEnabledFieldDefaultsToEnabled(t *testing.T) {
	setupChannelCreateHandlerTest(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	// 老客户端的形状：JSON 里没有 "enabled" 键，必须保持默认启用。
	body := `{"name":"b2-missing-channel","type":0,"model":"gpt-4o-mini"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/channel/create", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	createChannel(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	saved := loadChannelRowByName(t, "b2-missing-channel")
	if !saved.Enabled {
		t.Fatalf("expected channel without explicit enabled to default to enabled=true, got enabled=false")
	}
}

func loadChannelRowByName(t *testing.T, name string) model.Channel {
	t.Helper()
	var saved model.Channel
	if err := db.GetDB().Where("name = ?", name).First(&saved).Error; err != nil {
		t.Fatalf("load channel %q failed: %v", name, err)
	}
	return saved
}
