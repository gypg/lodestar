package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
)

// initLogoutTestDB spins up an in-memory SQLite, seeds a single admin user,
// and returns its freshly minted JWT. Routes are wired by each test so the
// exact chain (public logout vs authenticated surface) is explicit.
func initLogoutTestDB(t *testing.T) (token string, userID uint) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := "file:logout-" + strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()) + "?mode=memory&cache=shared"
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conf.AppConfig.Auth.JWTSecret = "logout-test-secret"
	if err := op.UserInit(); err != nil {
		t.Fatalf("user init: %v", err)
	}
	if err := op.UserBootstrapCreate("admin", "super-secret-123"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	u := op.UserGet()
	tok, _, err := auth.GenerateJWTToken(60, u.ID, u.Role)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return tok, u.ID
}

// revokedCookieFromResponse extracts whether a response carries a deletion
// directive for the JWT cookie. Gin's c.SetCookie with maxAge=-1 emits
// "token=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax" (Gin writes Max-Age=0
// rather than -1; both mean delete). We check the cookie is blanked AND
// max-age forces immediate expiry.
func logoutCookieCleared(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var setCookie string
	for _, h := range w.Header().Values("Set-Cookie") {
		if strings.HasPrefix(h, middleware.JWTCookieName+"=") {
			setCookie = h
			break
		}
	}
	if setCookie == "" {
		t.Fatalf("logout did not emit a Set-Cookie for %q — cookie is left intact (M-d: logout handler not clearing cookie)", middleware.JWTCookieName)
	}
	// Cookie value must be emptied.
	if !strings.HasPrefix(setCookie, middleware.JWTCookieName+"=;") {
		t.Fatalf("logout Set-Cookie = %q, want empty value (token blanked)", setCookie)
	}
	// Must force immediate client-side expiry.
	if !strings.Contains(setCookie, "Max-Age=0") && !strings.Contains(setCookie, "Max-Age=-1") && !strings.Contains(setCookie, "expires=Thu, 01 Jan 1970") {
		t.Fatalf("logout Set-Cookie = %q, want Max-Age<=0 / epoch expiry (otherwise the browser keeps the cookie alive)", setCookie)
	}
}

// newLogoutEngine mounts: a public /logout, and an authenticated /me probe.
// logout is intentionally NOT behind Auth() — a stale token must still clear
// its own cookie (WO-023 缺陷 B 修复步骤 1).
func newLogoutEngine() *gin.Engine {
	r := gin.New()
	r.POST("/api/v1/user/logout", middleware.RequireJSON(), logout)
	r.GET("/api/v1/user/me", middleware.Auth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": c.GetInt("user_id")})
	})
	return r
}

// TestWO023_T4_LogoutClearsCookie_AuthFailsWithoutIt 钉住登出的可执行不变量：
//   - logout 端点必须在响应里下发删除 cookie 的指令（M-d 删掉这步会红）
//   - 一个不带 cookie / 带过期 token 的后续请求必须被 Auth() 判 401
//
// 关于"用原 cookie 调受保护接口必须 401"：JWT 是无状态的，登出只清浏览器侧
// cookie、不撤销 token 本身，token 在自身 TTL 内仍密码学有效。要拦下"token 被
// 偷走后继续用"需要服务端撤销名单（超出本工单范围）。本工单修复的安全收益是
// "共享机器上登出后下一个用户不再被前一个用户的 cookie 冒充"——即浏览器删掉
// cookie 后不再发送它。这条用它最贴切的可执行形式钉住：登出端点删 cookie，
// 且不带 cookie 的请求被 401。
func TestWO023_T4_LogoutClearsCookie_AuthFailsWithoutIt(t *testing.T) {
	token, _ := initLogoutTestDB(t)
	r := newLogoutEngine()

	// 1) 登出调用本身（带一个有效 cookie，模拟已登录用户点登出）。
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/user/logout", strings.NewReader("{}"))
	logoutReq.Header.Set("Content-Type", "application/json")
	logoutReq.AddCookie(&http.Cookie{Name: middleware.JWTCookieName, Value: token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, logoutReq)
	if w.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200（登出应当无条件成功）", w.Code)
	}
	logoutCookieCleared(t, w)

	// 2) 模拟浏览器遵守删除指令：后续请求不再带 cookie。受保护接口必须 401。
	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/user/me", nil)
	meW := httptest.NewRecorder()
	r.ServeHTTP(meW, meReq)
	if meW.Code != http.StatusUnauthorized {
		t.Fatalf("post-logout cookieless /me = %d, want 401（登出后浏览器不再发 cookie，Auth() 应判未授权）", meW.Code)
	}
}

// TestWO023_T5_StaleTokenCanStillLogout 钉住"陈旧 token 也能登出"：用户手持
// 已失效的 token（过期或被改坏）时，logout 必须仍能成功清 cookie，否则用户卡在
// "清不掉"的状态（M-e：把 logout 挂到 Auth() 后面会让这条红）。
func TestWO023_T5_StaleTokenCanStillLogout(t *testing.T) {
	_, _ = initLogoutTestDB(t)
	r := newLogoutEngine()

	// 用一个确已过期的 token（exp 留在过去）构造 cookie。GenerateJWTToken 不允许
	// 负数分钟表示"过去"，这里直接造一个 1 分钟过期且等 70ms 已过的——但 JIT 没
	// 有睡过去的低成本办法，改用一个畸形 token：它能让 VerifyJWTToken 判 false，
	// 这正是"失效 token"的一个合法形态。
	staleToken := "not-a-real-jwt"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/logout", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: middleware.JWTCookieName, Value: staleToken})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code >= 500 {
		t.Fatalf("logout with stale token = %d, must not be 5xx（用户必须能清掉坏 cookie）", w.Code)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("logout with stale token = %d, want 200（logout 不应要求鉴权通过）", w.Code)
	}
	logoutCookieCleared(t, w)
}
