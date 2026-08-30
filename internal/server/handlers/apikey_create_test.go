package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	dbpkg "github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	apikeypkg "github.com/gypg/lodestar/internal/op/apikey"
)

// APIKey.Enabled 带 gorm default:true 标签，且鉴权链
// （auth.GetByKey → keyIDMap → Get → keyCache）**只读缓存、零 DB 回落**：
// struct Create 之后 DB 和缓存都是 enabled=true，新建的停用 Key 能直接通过鉴权。
// 三条测试都真走 JSON 绑定；断言除了 DB 行，还必须穿过
// apikey.Get / GetByKey（鉴权实际读的那条路）——只断言 DB 行不算完成这条路径。

func setupAPIKeyCreateHandlerTest(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", testName)
	if err := dbpkg.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	t.Cleanup(func() {
		_ = dbpkg.Close()
	})
}

func createAPIKeyViaHandler(t *testing.T, body string) model.APIKey {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/apikey/create", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	createAPIKey(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response struct {
		Code int          `json:"code"`
		Data model.APIKey `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode create response failed: %v", err)
	}
	return response.Data
}

func TestCreateAPIKeyPersistsExplicitlyDisabledKeyAsDisabled(t *testing.T) {
	setupAPIKeyCreateHandlerTest(t)

	created := createAPIKeyViaHandler(t, `{"name":"b3-disabled","enabled":false,"api_key":"b3-disabled-key"}`)

	// 响应体必须如实
	if created.Enabled {
		t.Fatalf("create response reports enabled=true; caller asked for false")
	}

	// 鉴权实际读的那条路：GetByKey → keyIDMap → Get → keyCache
	byKey, err := apikeypkg.GetByKey(created.APIKey, nil)
	if err != nil {
		t.Fatalf("GetByKey(%q) failed: %v", created.APIKey, err)
	}
	if byKey.Enabled {
		t.Fatalf("auth path reports the explicitly disabled key as enabled (GetByKey); it would pass authentication")
	}

	byID, err := apikeypkg.Get(created.ID, nil)
	if err != nil {
		t.Fatalf("Get(%d) failed: %v", created.ID, err)
	}
	if byID.Enabled {
		t.Fatalf("auth path reports the explicitly disabled key as enabled (Get)")
	}

	// DB 行（权威持久层）
	saved := loadAPIKeyRow(t, created.ID)
	if saved.Enabled {
		t.Fatalf("user asked for enabled=false; stored enabled=true")
	}
}

func TestCreateAPIKeyPersistsExplicitlyEnabledKeyAsEnabled(t *testing.T) {
	setupAPIKeyCreateHandlerTest(t)

	created := createAPIKeyViaHandler(t, `{"name":"b3-enabled","enabled":true,"api_key":"b3-enabled-key"}`)

	byKey, err := apikeypkg.GetByKey(created.APIKey, nil)
	if err != nil {
		t.Fatalf("GetByKey failed: %v", err)
	}
	if !byKey.Enabled {
		t.Fatalf("user asked for enabled=true; auth path reports enabled=false")
	}

	saved := loadAPIKeyRow(t, created.ID)
	if !saved.Enabled {
		t.Fatalf("user asked for enabled=true; stored enabled=false")
	}
}

func TestCreateAPIKeyAddsKeyWithoutEnabledFieldAsEnabled(t *testing.T) {
	setupAPIKeyCreateHandlerTest(t)

	// 老客户端的形状：JSON 里没有 "enabled" 键，必须保持默认启用。
	created := createAPIKeyViaHandler(t, `{"name":"b3-missing","api_key":"b3-missing-key"}`)

	byKey, err := apikeypkg.GetByKey(created.APIKey, nil)
	if err != nil {
		t.Fatalf("GetByKey failed: %v", err)
	}
	if !byKey.Enabled {
		t.Fatalf("expected key without explicit enabled to default to enabled=true, got enabled=false")
	}
}

func loadAPIKeyRow(t *testing.T, id int) model.APIKey {
	t.Helper()
	var saved model.APIKey
	if err := dbpkg.GetDB().First(&saved, id).Error; err != nil {
		t.Fatalf("load api key %d failed: %v", id, err)
	}
	return saved
}
