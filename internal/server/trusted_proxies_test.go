package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/conf"
)

/*
S-3 调用点守卫 —— Start() 必须把可信代理收窄，否则 c.ClientIP() 可被伪造。

缺陷（2026-08-08 探针复现确认）：gin.New() 默认 trustedProxies 为
["0.0.0.0/0", "::/0"]（gin@v1.11.0 gin.go:215），无条件信任任何直连来源发来的
X-Forwarded-For / X-Real-IP。实测一个来自 203.0.113.9 的直连请求带上
`X-Forwarded-For: 10.0.0.5` 后 c.ClientIP() 返回 10.0.0.5，于是：

  - middleware/auth.go:176 的 API key IP 白名单被单个请求头完全绕过
    （实测 isIPAllowed("10.0.0.5", "10.0.0.5")==true，而真实来源不在白名单里）
  - middleware/rate_limit.go:179/:292 的登录与邮件验证码限流可被换头绕过
  - middleware/turnstile.go:58 上报给 Cloudflare 的 remoteip 不可信

入口在 Start()（不是 conf.TrustedProxies()），因为要守的是"启动时到底有没有接线"。
若从 conf.TrustedProxies() 切入，守的是"这个函数返回什么"，而不是"服务器用没用它"
—— 那正是 [[lodestar-worker-false-evidence]] 第七变体踩过的坑。
观测点是 Start() 装配出的真实 handler（httpSrv.Handler），用 httptest 发请求看 ClientIP。
*/

// probeEngine 是 Start() 装配好的引擎，外加一条回读 ClientIP 的探针路由。
// 路由只注册一次（gin 对重复路径会 panic），每次请求把结果写进 lastClientIP。
type probeEngine struct {
	engine         *gin.Engine
	lastClientIP   *string
	probeRoutePath string
}

// startTestServer 用随机端口真正跑一次 Start()，返回它装配好的引擎。
// port 0 让内核分配，避免与本机既有服务撞端口；Cleanup 里 Close。
func startTestServer(t *testing.T) *probeEngine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	prev := conf.AppConfig.Server
	t.Cleanup(func() { conf.AppConfig.Server = prev })
	conf.AppConfig.Server.Host = "127.0.0.1"
	conf.AppConfig.Server.Port = 0

	if err := Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = Close() })

	if httpSrv.Handler == nil {
		t.Fatal("Start did not install a handler")
	}
	engine, ok := httpSrv.Handler.(*gin.Engine)
	if !ok {
		t.Fatalf("handler is %T, want *gin.Engine", httpSrv.Handler)
	}

	// 探针路由挂在 Start() 装配出的那个引擎上，所以探到的信任配置与生产完全一致
	// （现有公开端点都不回显 IP，故只能自己加一条观测路由）。
	//
	// 路径必须落在 /api 前缀下：静态资源中间件（middleware/static.go:32）只对
	// /api 与 /v1 放行，其它路径交给 http.FileServer，会被规范化成 301 而永远
	// 到不了这条路由。前端未构建时 static.StaticFS 为 nil、中间件根本不挂载，
	// 于是本地绿而带 static/out 的环境红 —— 别依赖这个偶然条件。
	var seen string
	path := "/api/v1/__s3_clientip_probe__/" + t.Name()
	engine.GET(path, func(c *gin.Context) {
		seen = c.ClientIP()
		c.Status(http.StatusOK)
	})

	return &probeEngine{engine: engine, lastClientIP: &seen, probeRoutePath: path}
}

// clientIP 发一次带指定 TCP 源地址与 XFF 的请求，返回服务器算出的 ClientIP。
func (p *probeEngine) clientIP(t *testing.T, remoteAddr, xff string) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, p.probeRoutePath, nil)
	req.RemoteAddr = remoteAddr
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rec := httptest.NewRecorder()
	p.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("probe route status: want 200, got %d", rec.Code)
	}
	return *p.lastClientIP
}

// TestStart_publicClientCannotSpoofClientIP
// ★ S-3 核心守卫：公网直连客户端伪造 XFF 必须无效。
//
// 回归时的表现（把 SetTrustedProxies 那行删掉）：ClientIP() 返回伪造的
// 10.0.0.5，本断言红。断言写死"必须等于真实 TCP 源地址"而不是"不等于伪造值"，
// 后者在 ClientIP() 返回空串时也会绿。
func TestStart_publicClientCannotSpoofClientIP(t *testing.T) {
	srv := startTestServer(t)

	got := srv.clientIP(t, "203.0.113.9:44321", "10.0.0.5")
	if got != "203.0.113.9" {
		t.Errorf("ClientIP for a direct public client spoofing XFF: want %q (the real TCP source), got %q — "+
			"a spoofable ClientIP bypasses the API key IP allowlist (middleware/auth.go:176)",
			"203.0.113.9", got)
	}
}

// TestStart_trustedProxyStillForwardsRealIP
// ★ 反方向守卫：不能为了防伪造把反代场景一起打死。
//
// 这条挡的是"改坏成谁都不信"（例如把默认值改成空、或误删私网网段）——
// 那样反代后面的真实客户端 IP 会全部退化成反代自己的地址，限流和白名单
// 会按反代 IP 聚合，等于另一种失效。只有 M1(删接线) 红而本条绿，才说明修复方向对。
func TestStart_trustedProxyStillForwardsRealIP(t *testing.T) {
	srv := startTestServer(t)

	// 回环反代（Cloudflare 隧道 / nginx 转 127.0.0.1，见 docs/DEPLOY.md:112）
	if got := srv.clientIP(t, "127.0.0.1:5555", "198.51.100.7"); got != "198.51.100.7" {
		t.Errorf("ClientIP behind a loopback reverse proxy: want %q, got %q — "+
			"real client IPs would collapse to the proxy address", "198.51.100.7", got)
	}

	// 容器网络内反代（docker 默认 bridge 172.17.0.0/16 ⊂ 172.16.0.0/12）
	if got := srv.clientIP(t, "172.17.0.3:5555", "198.51.100.8"); got != "198.51.100.8" {
		t.Errorf("ClientIP behind a container-network reverse proxy: want %q, got %q",
			"198.51.100.8", got)
	}
}

// TestStart_explicitEmptyListTrustsNoProxy
// 显式空数组的语义：运维明确表达"本服务直接对外，不认 XFF"，必须尊重，
// 不能被 nil 回落逻辑吞掉改成默认列表。
//
// 与上一条测试合起来锁死 conf.TrustedProxies() 的三态：
// nil→默认值 / []→空 / 有内容→该内容。只测其中一态会让另外两态可以随便改。
func TestStart_explicitEmptyListTrustsNoProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prev := conf.AppConfig.Server
	t.Cleanup(func() { conf.AppConfig.Server = prev })
	conf.AppConfig.Server.Host = "127.0.0.1"
	conf.AppConfig.Server.Port = 0
	conf.AppConfig.Server.TrustedProxies = []string{}

	if err := Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = Close() })

	engine, ok := httpSrv.Handler.(*gin.Engine)
	if !ok {
		t.Fatalf("handler is %T, want *gin.Engine", httpSrv.Handler)
	}
	var seen string
	engine.GET("/api/v1/__s3_empty_probe__", func(c *gin.Context) {
		seen = c.ClientIP()
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/__s3_empty_probe__", nil)
	req.RemoteAddr = "127.0.0.1:5555" // 即便是回环，显式空列表下也不该被信任
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("probe route status: want 200, got %d", rec.Code)
	}

	if seen != "127.0.0.1" {
		t.Errorf("ClientIP with an explicitly empty trusted_proxies: want %q, got %q — "+
			"an explicit [] must mean 'trust no proxy', not fall back to the defaults",
			"127.0.0.1", seen)
	}
}

// TestStart_normalizesEnvStyleWhitespace
// env 形式的配置必须能直接用：
// `LODESTAR_SERVER_TRUSTED_PROXIES=127.0.0.1, 10.0.0.0/8` 被 viper 按逗号切开后
// 第二项带前导空格，gin 的 net.ParseCIDR 不接受（实测报
// `invalid CIDR address:  10.0.0.0/8`）—— 不归一化就是启动直接失败。
//
// 这条锁的是 conf.TrustedProxies() 里的 TrimSpace：把它改成无操作后，
// Start() 会返回错误，本测试红。
func TestStart_normalizesEnvStyleWhitespace(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prev := conf.AppConfig.Server
	t.Cleanup(func() { conf.AppConfig.Server = prev })
	conf.AppConfig.Server.Host = "127.0.0.1"
	conf.AppConfig.Server.Port = 0
	// 逐字模拟 viper 切分 `127.0.0.1, 10.0.0.0/8` 的结果。
	conf.AppConfig.Server.TrustedProxies = []string{"127.0.0.1", " 10.0.0.0/8"}

	if err := Start(); err != nil {
		t.Fatalf("Start with env-style spacing: want success, got %v — "+
			"un-trimmed entries make gin reject the whole list at startup", err)
	}
	t.Cleanup(func() { _ = Close() })

	engine, ok := httpSrv.Handler.(*gin.Engine)
	if !ok {
		t.Fatalf("handler is %T, want *gin.Engine", httpSrv.Handler)
	}
	var seen string
	engine.GET("/api/v1/__s3_trim_probe__", func(c *gin.Context) {
		seen = c.ClientIP()
		c.Status(http.StatusOK)
	})

	// 空格那一项必须真的生效：来自 10.0.0.0/8 的反代转发要被采信。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/__s3_trim_probe__", nil)
	req.RemoteAddr = "10.1.2.3:5555"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("probe route status: want 200, got %d", rec.Code)
	}
	if seen != "198.51.100.7" {
		t.Errorf("ClientIP behind the space-prefixed CIDR entry: want %q, got %q — "+
			"the trimmed entry did not make it into the trusted list",
			"198.51.100.7", seen)
	}
}

// TestStart_rejectsMalformedTrustedProxies
// ★ fail-closed 守卫：配置写错时必须启动失败，不能带着半套信任列表继续跑。
//
// 实测过 gin 的 error 路径会残留：SetTrustedProxies(["203.0.113.0/24","not-an-ip"])
// 返回 error，但 203.0.113.0/24 仍然生效（parseTrustedProxies 在 gin.go:452-456
// 无条件把已解析部分赋给 trustedCIDRs 后才返回 err）。所以忽略这个 error
// 会得到一个"部分信任"的引擎 —— 比完全不设更难察觉。
func TestStart_rejectsMalformedTrustedProxies(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prev := conf.AppConfig.Server
	t.Cleanup(func() { conf.AppConfig.Server = prev })
	conf.AppConfig.Server.Host = "127.0.0.1"
	conf.AppConfig.Server.Port = 0
	conf.AppConfig.Server.TrustedProxies = []string{"203.0.113.0/24", "not-an-ip"}

	err := Start()
	if err == nil {
		_ = Close()
		t.Fatal("Start with a malformed trusted_proxies entry: want an error, got nil — " +
			"gin keeps the already-parsed CIDRs on error, so ignoring it yields a partially trusting engine")
	}
	if !strings.Contains(err.Error(), "trusted_proxies") || !strings.Contains(err.Error(), "not-an-ip") {
		t.Errorf("error message should name the offending config and value, got: %v", err)
	}
}
