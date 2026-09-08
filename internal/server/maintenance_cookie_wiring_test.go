package server

/*
WO-045 阻断 3（octopus #240 × 维护守卫交界）：

maintenance.go 的 isStaffRequest 原来只读 Authorization 头；WO-044 #240 把 JWT
从 localStorage 撤下后，管理员刷新页面内存 token 为 null、头不再发，会话只剩
HttpOnly cookie——维护模式开启时所有管理写请求（包括用来关维护的
POST /api/v1/setting/set）被判非员工 503，恢复途径只剩登出重登。

接线方式说明：getProductionEngine 只跑 router.RegisterAll，**全局中间件
（MaintenanceGuard 等）只挂在 server.Start() 的引擎上**，且 RegisterAll 末尾
registeredRouters = nil 是单次消费——无法在测试里再组一次全量路由。因此这里
按 server.go:83-86 的真实顺序组最小引擎：gin.New + MaintenanceGuard +
与 /api/v1/setting/set 同构的中间件链（Auth + RequirePermission）。守卫、
员工判定、cookie 通路、权限链全部是生产函数本体，只有 handler 是探针。
*/

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/setting"
	serverauth "github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
)

func TestMaintenanceGuardStaffViaCookieWhenNoAuthHeader(t *testing.T) {
	dsn := "file:wo045-maintenance-cookie?mode=memory&cache=shared"
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	conf.AppConfig.Auth.JWTSecret = "test-jwt-secret-wo045-maintenance"
	if err := op.UserInit(); err != nil {
		t.Fatalf("user init: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	if err := setting.RefreshCache(nil); err != nil {
		t.Fatalf("refresh settings: %v", err)
	}

	admin := dbmodel.User{Username: "wo045-admin", Password: "x", Role: dbmodel.UserRoleAdmin, Quota: 0}
	if err := db.GetDB().Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	viewer := dbmodel.User{Username: "wo045-viewer", Password: "x", Role: dbmodel.UserRoleViewer, Quota: 0}
	if err := db.GetDB().Create(&viewer).Error; err != nil {
		t.Fatal(err)
	}

	adminToken, _, err := serverauth.GenerateJWTToken(60, admin.ID, dbmodel.UserRoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	viewerToken, _, err := serverauth.GenerateJWTToken(60, viewer.ID, dbmodel.UserRoleViewer)
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 与 server.go:85 同序：MaintenanceGuard 是全局层，先于路由级 Auth。
	engine.Use(middleware.MaintenanceGuard())
	// 与 /api/v1/setting/set 同构的受守卫写路径（Auth + settings:write）。
	engine.POST("/api/v1/setting/set",
		middleware.Auth(),
		middleware.RequirePermission(serverauth.PermSettingsWrite),
		func(c *gin.Context) { resp.Success(c, gin.H{"ok": true}) },
	)

	// 开维护模式（在引擎组好之后、请求之前——守卫每次请求实时读设置）。
	if err := setting.SetString(dbmodel.SettingKeyMaintenanceMode, "true"); err != nil {
		t.Fatalf("enable maintenance: %v", err)
	}
	t.Cleanup(func() { _ = setting.SetString(dbmodel.SettingKeyMaintenanceMode, "false") })

	post := func(withCookie func(*http.Request), tokenHeader string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/setting/set", strings.NewReader(`{"key":"site_banner_text","value":"wo045"}`))
		req.Header.Set("Content-Type", "application/json")
		if tokenHeader != "" {
			req.Header.Set("Authorization", "Bearer "+tokenHeader)
		}
		if withCookie != nil {
			withCookie(req)
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec.Code
	}
	addCookie := func(value string) func(*http.Request) {
		return func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "token", Value: value})
		}
	}

	// 主判据：维护开 + admin JWT 只在 cookie、无 Authorization 头 → 不是 503。
	// （cookie 通路进 Auth → admin 有 settings:write → 200。）
	if code := post(addCookie(adminToken), ""); code == http.StatusServiceUnavailable {
		t.Fatalf("admin with cookie-only session got %d under maintenance — isStaffRequest must read the cookie (extractToken), not just the Authorization header", code)
	}

	// 对照组 1：无 cookie 无头 → 503（维护守卫先于 Auth 拦下）。
	if code := post(nil, ""); code != http.StatusServiceUnavailable {
		t.Fatalf("anonymous write under maintenance = %d, want 503", code)
	}
	// 对照组 2：viewer cookie → 503（员工判定不因 cookie 通路放宽）。
	if code := post(addCookie(viewerToken), ""); code != http.StatusServiceUnavailable {
		t.Fatalf("viewer cookie write under maintenance = %d, want 503", code)
	}
	// 对照组 3：admin 头（旧通路）→ 不是 503，回归保障。
	if code := post(nil, adminToken); code == http.StatusServiceUnavailable {
		t.Fatal("admin with Authorization header got 503 — header path regressed")
	}
}
