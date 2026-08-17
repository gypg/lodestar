package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/op/errorlog"
)

// newErrorLogReportTestEngine 按生产 init() 的挂法把 report handler 装到
// 本地引擎上（handlers 包不能用 RegisterAll，见 webauthn 测试头注释）。
func newErrorLogReportTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	resetReportRateLimitsForTest()
	t.Cleanup(resetReportRateLimitsForTest)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 模拟 Auth() 中间件设置的 user_id。
	engine.Use(func(c *gin.Context) { c.Set("user_id", 7); c.Next() })
	engine.POST("/api/v1/error-log/report", reportErrorLog)
	return engine
}

func postReport(engine *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/error-log/report", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestReportErrorLogPersistsFrontendEntry(t *testing.T) {
	engine := newErrorLogReportTestEngine(t)

	rec := postReport(engine, `{
		"level": "unhandledrejection",
		"message": "TypeError: Cannot read properties of undefined",
		"stack": "at f (http://localhost/x.js:1:1)",
		"page_url": "http://localhost/settings",
		"route_id": "settings",
		"version": "v2.1.4"
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("report code = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	entries, err := errorlog.List(context.Background(), errorlog.Filter{Source: "frontend"}, 1, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 frontend entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Level != "unhandledrejection" {
		t.Fatalf("level = %q", e.Level)
	}
	if e.Message != "TypeError: Cannot read properties of undefined" {
		t.Fatalf("message = %q", e.Message)
	}
	if e.Stack == "" || e.PageURL != "http://localhost/settings" || e.RouteID != "settings" {
		t.Fatalf("entry fields incomplete: %+v", e)
	}
	if e.Version != "v2.1.4" {
		t.Fatalf("version = %q", e.Version)
	}
}

func TestReportErrorLogNormalizesAndIgnoresEmpty(t *testing.T) {
	engine := newErrorLogReportTestEngine(t)

	// 非法 level 归一为 error；缺失字段容错。
	rec := postReport(engine, `{"level": "weird", "message": "oops"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("normalize case code = %d, want 200", rec.Code)
	}
	entries, _ := errorlog.List(context.Background(), errorlog.Filter{}, 1, 10)
	if len(entries) != 1 || entries[0].Level != "error" {
		t.Fatalf("level normalization failed: %+v", entries)
	}

	// 空 message 幂等忽略，不落库。
	rec = postReport(engine, `{"level": "error", "message": "   "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty message case code = %d, want 200", rec.Code)
	}
	entries, _ = errorlog.List(context.Background(), errorlog.Filter{}, 1, 10)
	if len(entries) != 1 {
		t.Fatalf("empty message should not persist, got %d entries", len(entries))
	}

	// 畸形 JSON → 400。
	rec = postReport(engine, `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed json code = %d, want 400", rec.Code)
	}
}

func TestReportRateLimitedPerUserPerMinute(t *testing.T) {
	resetReportRateLimitsForTest()
	t.Cleanup(resetReportRateLimitsForTest)

	const key = "user-42"
	for i := 0; i < reportRateLimitPerMinute; i++ {
		if reportRateLimited(key) {
			t.Fatalf("request %d within limit was throttled", i+1)
		}
	}
	if !reportRateLimited(key) {
		t.Fatalf("request %d should be throttled", reportRateLimitPerMinute+1)
	}
	// 其他用户不受影响。
	if reportRateLimited("user-43") {
		t.Fatal("per-user limit leaked across users")
	}

	// 窗口过期后放行：直接把窗口起点拨回一分钟前。
	reportRateLimits.Lock()
	entry := reportRateLimits.items[key]
	entry.at = time.Now().Add(-time.Minute - time.Second)
	reportRateLimits.items[key] = entry
	reportRateLimits.Unlock()
	if reportRateLimited(key) {
		t.Fatal("request after window expiry was still throttled")
	}
}

func TestReportErrorLogRateLimitedAtHandlerLevel(t *testing.T) {
	engine := newErrorLogReportTestEngine(t)

	var last int
	for i := 0; i < reportRateLimitPerMinute+5; i++ {
		rec := postReport(engine, `{"level": "error", "message": "flood"}`)
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("61st report in a minute returned %d, want 429", last)
	}
	// 限流生效期间落库条数恰为上限（60），之后的请求被拒之门外。
	entries, _ := errorlog.List(context.Background(), errorlog.Filter{}, 1, 200)
	if len(entries) != reportRateLimitPerMinute {
		t.Fatalf("persisted %d entries, want %d (rate-limited reports must not persist)", len(entries), reportRateLimitPerMinute)
	}
}
