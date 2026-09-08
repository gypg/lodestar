package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	userpkg "github.com/gypg/lodestar/internal/op/user"
	serverauth "github.com/gypg/lodestar/internal/server/auth"
)

// WO-045 顺手 5：JWT 吊销比较必须是 <=（Unix 秒粒度下 < 留一秒窗口——同秒
// 签发的 token 在改密后仍存活，成功登录不限流可主动轮询命中）。
func TestJWTRevocationSameSecondTokenIsRejected(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conf.AppConfig.Auth.JWTSecret = "test-jwt-secret-wo045-revocation"
	if err := op.UserInit(); err != nil {
		t.Fatalf("user init: %v", err)
	}

	u := model.User{Username: "wo045-revoke", Password: "old-password-wo045", Role: model.UserRoleUser, Quota: 1}
	if err := u.HashPassword(); err != nil {
		t.Fatal(err)
	}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	// 改密（打 PasswordChangedAt），再签 token；把 PasswordChangedAt 精确拨到
	// token 的 IssuedAt——模拟「同一 Unix 秒」的最坏边界。
	if err := userpkg.ChangePassword(u.ID, "old-password-wo045", "new-password-wo045"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	token, _, err := serverauth.GenerateJWTToken(60, u.ID, u.Role)
	if err != nil {
		t.Fatal(err)
	}
	valid, _, _, issuedAt := serverauth.VerifyJWTToken(token)
	if !valid {
		t.Fatal("sanity: fresh token invalid")
	}
	if err := db.GetDB().Model(&model.User{}).Where("id = ?", u.ID).
		Update("password_changed_at", issuedAt).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/probe", Auth(), func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("token issued in the same second as PasswordChangedAt got %d, want 401 — revocation must use <=", rec.Code)
	}

	// 对照：改密之前签发的 token（IssuedAt 更小）必须拒。
	older, _, err := serverauth.GenerateJWTToken(60, u.ID, u.Role)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.GetDB().Model(&model.User{}).Where("id = ?", u.ID).
		Update("password_changed_at", issuedAt+999).Error; err != nil {
		t.Fatal(err)
	}
	req2 := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req2.Header.Set("Authorization", "Bearer "+older)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("token older than PasswordChangedAt got %d, want 401", rec2.Code)
	}
}
