package server

/*
WO-026 阶段 B：忘记密码的生产路由链验证。

用本包共享的 getProductionEngine（RegisterAll 单例，见 webauthn_ratelimit_route_test.go
头注释），钉两件事：
  - T-B4（HTTP 层）：POST /api/v1/user/forgot-password 对存在/不存在的邮箱返回
    **字节级相同**的响应（状态码 + body）——枚举防护。差异哪怕只差一个字符，端点
    就变成"哪些邮箱注册过"的探测器。
  - reset 端点存在且对错码返回 400（路由链形状），完整流程的语义在 op 层测
    （internal/op/user/password_reset_test.go）。
*/

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
)

func setupForgotPasswordRouteTest(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conf.AppConfig.Auth.JWTSecret = "test-jwt-secret-wo026-forgot"
	if err := op.UserInit(); err != nil {
		t.Fatalf("user init: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	// 一个有邮箱的用户。SMTP 不配置 —— 发送链路静默失败，这正是枚举防护要的形态：
	// 存在与否、发得出去与否，HTTP 响应都必须一致。
	u := model.User{Username: "resetee-" + t.Name(), Password: "x", Role: model.UserRoleUser, Email: "resetee@example.com"}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
}

func postForgotPassword(engine *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/forgot-password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestForgotPasswordEnumerationSafeOnTheWire 钉死 T-B4：存在与不存在的邮箱，
// 状态码与响应体字节级一致。
func TestForgotPasswordEnumerationSafeOnTheWire(t *testing.T) {
	setupForgotPasswordRouteTest(t)
	engine := getProductionEngine(t)

	existing := postForgotPassword(engine, `{"email":"resetee@example.com"}`)
	absent := postForgotPassword(engine, `{"email":"ghost@example.com"}`)

	if existing.Code != http.StatusOK {
		t.Fatalf("existing email: status = %d, want 200; body=%s", existing.Code, existing.Body.String())
	}
	if existing.Code != absent.Code {
		t.Fatalf("enumeration oracle: existing=%d absent=%d — status codes differ (T-B4)",
			existing.Code, absent.Code)
	}
	if existing.Body.String() != absent.Body.String() {
		t.Fatalf("enumeration oracle: response bodies differ (T-B4)\n  existing: %q\n  absent:   %q",
			existing.Body.String(), absent.Body.String())
	}
}

// TestForgotPasswordResetEndpointRejectsBadCode 钉死 reset 端点在真实路由链上存在，
// 且错码返回 400（不泄露"邮箱不存在"与"码错误"的区别）。
func TestForgotPasswordResetEndpointRejectsBadCode(t *testing.T) {
	setupForgotPasswordRouteTest(t)
	engine := getProductionEngine(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/reset-password",
		strings.NewReader(`{"email":"resetee@example.com","code":"000000","new_password":"strongPassword123"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reset with wrong code: status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	// 错误文案不得区分"邮箱不存在"与"码错误"——两者统一"验证码错误或已过期"。
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "验证码错误或已过期") {
		t.Fatalf("reset failure message must be the generic one (no user-existence leak), got: %s", body)
	}
}
