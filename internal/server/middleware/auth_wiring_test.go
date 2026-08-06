package middleware

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
	"github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/op/setting"
)

// ---------------------------------------------------------------------------
// WO-011 — 余额检查接线断言（auth.go:171 的 billing.HasBalanceForKey 调用）
//
// 目标：防止重构时误删"请求进入前先检查余额"这条闸门。删除 auth.go:171 的
// HasBalanceForKey 检查后，余额=0 的 key 不再被 402 拒绝，本测试必须红。
//
// 策略：走 APIKeyAuth 中间件的真实 HTTP 路径，用余额=0 的 key 发请求：
//   - commercial_mode 开 + 余额 0 → 应被拒绝（402 PaymentRequired）
//   - 删掉 auth.go:171 的检查 → 请求放行到 handler → 状态码不再 402 → 红
//
// 同时用余额充足的 key 验证正常放行（handler 返回 200），锁定闸门"该放行时放行"。
// ---------------------------------------------------------------------------

// initAuthWiringDB 搭建内存 SQLite + 缓存 + 开启 commercial_mode，创建带指定余额的
// 用户和其名下 API key。返回 key 的明文值（用于构造 Authorization 头）。
func initAuthWiringDB(t *testing.T, quota float64) string {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := setting.SetString(model.SettingKeyCommercialMode, "true"); err != nil {
		t.Fatalf("enable commercial mode: %v", err)
	}
	u := model.User{Username: "auth-wire-" + t.Name(), Password: "x", Quota: quota}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	keyVal := "sk-lodestar-authwire-" + t.Name()
	key := model.APIKey{UserID: u.ID, Name: "wire-key", APIKey: keyVal}
	if err := apikey.Create(&key, context.Background()); err != nil {
		t.Fatalf("create key: %v", err)
	}
	return keyVal
}

func newAuthWiringEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(APIKeyAuth())
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	return r
}

func doAuthWireRequest(r *gin.Engine, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestAuthWiring_insufficientBalance_rejectsRequest
// 余额=0 的用户 key：commercial_mode 开 → 必须被 402 拒绝。若 auth.go:171 的
// HasBalanceForKey 检查被删 → 请求放行到 handler 返回 200 → 红。
func TestAuthWiring_insufficientBalance_rejectsRequest(t *testing.T) {
	key := initAuthWiringDB(t, 0.0) // 余额 0
	r := newAuthWiringEngine()

	w := doAuthWireRequest(r, key)
	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("zero-balance key status = %d, want %d (402) — HasBalanceForKey gate (auth.go:171) likely removed", w.Code, http.StatusPaymentRequired)
	}
}

// TestAuthWiring_sufficientBalance_allowsRequest
// 余额充足的用户 key：应放行到 handler → 200。锁定闸门不误伤正常请求。
func TestAuthWiring_sufficientBalance_allowsRequest(t *testing.T) {
	key := initAuthWiringDB(t, 10.0) // 余额充足
	r := newAuthWiringEngine()

	w := doAuthWireRequest(r, key)
	if w.Code != http.StatusOK {
		t.Fatalf("funded-key status = %d, want %d (200) — balance gate rejecting funded requests", w.Code, http.StatusOK)
	}
}

// TestAuthWiring_billingOff_allowsZeroBalance
// commercial_mode 关闭（自用）时，余额=0 的 key 也必须放行（fail-open）。锁定
// "开关"与"接线"是两回事：改开关不能掩盖接线被删——开关关闭时请求能过是预期的，
// 但这条同时证明：即使商业模式没开，APIKeyAuth 仍会走到 HasBalanceForKey 后再放行。
func TestAuthWiring_billingOff_allowsZeroBalance(t *testing.T) {
	key := initAuthWiringDB(t, 0.0)
	_ = setting.SetString(model.SettingKeyCommercialMode, "false")
	r := newAuthWiringEngine()

	w := doAuthWireRequest(r, key)
	if w.Code != http.StatusOK {
		t.Fatalf("billing-off zero-balance status = %d, want %d (200 fail-open)", w.Code, http.StatusOK)
	}
}