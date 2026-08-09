package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/router"
)

// S-8 生产接线守卫：/api/v1/webauthn/login/begin 必须挂 LoginRateLimit。
//
// ★ 为什么这个测试在 internal/server 而不是 handlers 包：
// router.RegisterAll 在结尾把全局 registeredRouters 置 nil（router.go:124），
// **每个进程只能成功注册一次**。handlers 包里 rbac_test 已经消费掉了它，
// 所以在那边再调只会得到空路由表（我第一版就踩了，4 个测试里 3 个拿到 404，
// 其中一个还因为断言宽松被 404 蒙成假绿）。本包的 audit_route_test 才是
// RegisterAll 的持有者，故生产路由表相关的断言都应该落在这里。
//
// ★ 为什么不检查 middleware 链本身：gin 的 engine.Routes() 只暴露最终 handler
// 的名字（实测 "handlers.webauthnLoginBegin"），拿不到中间件列表；
// route 注册表又已被置 nil。所以只能走行为观测。
//
// 观测法：把某个 IP 的登录失败计数打到封禁线，然后经**生产路由表**打
// /login/begin。LoginRateLimit 若在链上，会在进入 handler 前 abort 成 429；
// 若不在，请求会抵达 handler，而 webauthn 默认未配置 ⇒ 400。
// 两者状态码不同，故能区分接线在不在。
// productionEngine 是本包共享的、由 RegisterAll 注册出来的生产路由表。
//
// ★ RegisterAll 只能成功一次（router.go:124 注册完把全局表置 nil），所以本包
// 任何需要生产路由的测试都必须共用同一个 engine，不能各自调 RegisterAll——
// 否则谁先跑谁拿到路由，其余全部 404，测试结果依赖执行顺序。
var (
	productionEngine     *gin.Engine
	productionEngineErr  error
	productionEngineOnce sync.Once
)

func getProductionEngine(t *testing.T) *gin.Engine {
	t.Helper()
	productionEngineOnce.Do(func() {
		gin.SetMode(gin.TestMode)
		engine := gin.New()
		if err := router.RegisterAll(engine); err != nil {
			productionEngineErr = err
			return
		}
		productionEngine = engine
	})
	if productionEngineErr != nil {
		t.Fatalf("RegisterAll: %v", productionEngineErr)
	}
	if productionEngine == nil {
		t.Fatal("production engine is nil; RegisterAll was already consumed elsewhere in this package")
	}
	return productionEngine
}

func TestWebAuthnLoginBeginIsRateLimitedInProductionRoutes(t *testing.T) {
	engine := getProductionEngine(t)

	// httptest.NewRequest 的 RemoteAddr 固定为 192.0.2.1:1234；
	// gin 未设可信代理时 ClientIP() 取的就是它。
	const clientIP = "192.0.2.1"
	middleware.ClearLoginFailures(clientIP)
	t.Cleanup(func() { middleware.ClearLoginFailures(clientIP) })

	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webauthn/login/begin", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	// 基线：未封禁时请求应抵达 handler。webauthn 未配置 ⇒ 400。
	// 若这里就拿到 429，说明测试环境残留了封禁状态，断言会失去区分力。
	if rec := post(); rec.Code == http.StatusTooManyRequests {
		t.Fatalf("baseline already rate-limited (stale block state); body=%s", rec.Body.String())
	}

	// 打到封禁线（默认阈值 5 次失败）。
	for i := 0; i < 6; i++ {
		middleware.RecordLoginFailure(clientIP, time.Now())
	}

	rec := post()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d — LoginRateLimit is not wired on "+
			"POST /api/v1/webauthn/login/begin (a blocked IP still reached the handler); body=%s",
			rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
}
