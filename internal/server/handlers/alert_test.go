package handlers

import (
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

func setupAlertHandlerTest(t *testing.T) {
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
}

func TestDeleteAlertRuleNotFoundReturns404(t *testing.T) {
	setupAlertHandlerTest(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/alert/rule/delete/404", nil)
	c.Params = gin.Params{{Key: "id", Value: "404"}}

	deleteAlertRule(c)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
}

func TestCreateAlertRuleIgnoresReadonlyID(t *testing.T) {
	setupAlertHandlerTest(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/alert/rule/create", strings.NewReader(`{
		"id":404,
		"name":"created",
		"enabled":true,
		"condition_type":"error_rate",
		"threshold":10,
		"cooldown_sec":300
	}`))
	c.Request.Header.Set("Content-Type", "application/json")

	createAlertRule(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response struct {
		Data model.AlertRule `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Data.ID == 404 {
		t.Fatalf("create response preserved client supplied id")
	}
}

func TestUpdateAlertRuleNotFoundReturns404(t *testing.T) {
	setupAlertHandlerTest(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/alert/rule/update", strings.NewReader(`{
		"id":404,
		"name":"missing",
		"enabled":true,
		"condition_type":"error_rate",
		"threshold":10,
		"cooldown_sec":300
	}`))
	c.Request.Header.Set("Content-Type", "application/json")

	updateAlertRule(c)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
}

func TestDeleteNotifChannelNotFoundReturns404(t *testing.T) {
	setupAlertHandlerTest(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/alert/notif/delete/404", nil)
	c.Params = gin.Params{{Key: "id", Value: "404"}}

	deleteNotifChannel(c)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
}

func TestCreateNotifChannelIgnoresReadonlyID(t *testing.T) {
	setupAlertHandlerTest(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/alert/notif/create", strings.NewReader(`{
		"id":404,
		"name":"created",
		"type":"webhook",
		"url":"https://example.com/hook"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")

	createNotifChannel(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response struct {
		Data model.AlertNotifChannel `json:"data"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Data.ID == 404 {
		t.Fatalf("create response preserved client supplied id")
	}
}

func TestUpdateNotifChannelNotFoundReturns404(t *testing.T) {
	setupAlertHandlerTest(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/alert/notif/update", strings.NewReader(`{
		"id":404,
		"name":"missing",
		"type":"webhook",
		"url":"https://example.com/hook"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")

	updateNotifChannel(c)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
}

func TestNotifChannelReportsConfigError(t *testing.T) {
	setupAlertHandlerTest(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	// gotify channel with neither config nor url/secret fallbacks -> validation error
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/alert/notif/test", strings.NewReader(`{
		"name":"bad-gotify",
		"type":"gotify"
	}`))
	c.Request.Header.Set("Content-Type", "application/json")

	testNotifChannel(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "gotify") {
		t.Fatalf("expected error to mention gotify, got: %s", recorder.Body.String())
	}
}

// TestNotifChannelRejectsLoopbackWebhook 钉死 WO-025 修复行为：alert/notif/test 对
// 指向 loopback 的 webhook URL 必须返回 SSRF 校验错误，而不是真的拨号出去。
//
// 原 TestNotifChannelSucceedsAgainstWebhook 用 httptest.NewServer（必然监听 127.0.0.1）
// 端到端验证 webhook 链路。SSRF 校验加在 SendWebhook 内部后，loopback 必被拒——
// 那个端到端形态与安全校验根本冲突（校验必须拒 loopback，测试 server 必然在 loopback），
// 故改写为两个互补断言：本测试钉“loopback 被拒”，下一个钉“公网不被 SSRF 拒”。
func TestNotifChannelRejectsLoopbackWebhook(t *testing.T) {
	setupAlertHandlerTest(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := `{"name":"my-webhook","type":"webhook","url":"http://127.0.0.1:9999/hook"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/alert/notif/test", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	testNotifChannel(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (loopback webhook must be refused); body=%s",
			recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "url is not allowed") {
		t.Fatalf("loopback webhook must be refused with SSRF error, got: %s", recorder.Body.String())
	}
}

// TestNotifChannelAcceptsPublicWebhook 钉死 WO-025 T2 侧：公网 webhook URL 不应被
// SSRF 校验拒绝。example.com 能解析、其 IP 不在 IsDisallowedIP 名单内，校验应放行。
// 后续可能因 example.com 不是真 webhook 而返回其他错误（如 404/连接失败）——
// 只断言“不是因为 SSRF 拒绝”。
func TestNotifChannelAcceptsPublicWebhook(t *testing.T) {
	setupAlertHandlerTest(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := `{"name":"my-webhook","type":"webhook","url":"https://example.com/hook"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/alert/notif/test", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	testNotifChannel(c)

	if strings.Contains(recorder.Body.String(), "url is not allowed") {
		t.Fatalf("public webhook example.com must NOT be SSRF-refused, got: %s", recorder.Body.String())
	}
}
