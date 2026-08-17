package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/op/errorlog"
)

// TestPanicRecoveryPersistsToErrorLog 验证生产 recovery 闭包把 panic +
// debug.Stack() 落主库（此前只恢复+500，重启即丢）。
func TestPanicRecoveryPersistsToErrorLog(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(gin.CustomRecovery(panicRecoveryHandler))
	engine.GET("/boom", func(c *gin.Context) {
		panic("kaboom: simulated nil deref")
	})

	req := httptest.NewRequest(http.MethodGet, "/boom?x=1", nil)
	req.Header.Set("User-Agent", "test-agent/1.0")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic response code = %d, want 500", rec.Code)
	}

	entries, err := errorlog.List(context.Background(), errorlog.Filter{Source: "backend", Level: "panic"}, 1, 10)
	if err != nil {
		t.Fatalf("list error logs: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 backend panic entry, got %d", len(entries))
	}
	e := entries[0]
	if !strings.Contains(e.Message, "kaboom") {
		t.Fatalf("message %q does not contain panic value", e.Message)
	}
	if !strings.Contains(e.Stack, "goroutine") || !strings.Contains(e.Stack, "panicRecovery") {
		t.Fatalf("stack does not look like a real debug.Stack(): %q", e.Stack)
	}
	if e.RequestMethod != http.MethodGet || e.RequestPath != "/boom" {
		t.Fatalf("request info = %s %s, want GET /boom", e.RequestMethod, e.RequestPath)
	}
	if e.UserAgent != "test-agent/1.0" {
		t.Fatalf("user agent = %q", e.UserAgent)
	}
	if e.Version == "" {
		t.Fatal("version (conf.Version) not recorded")
	}
}

// TestErrorLogRoutesRegisteredInProductionEngine 验证错误日志四个端点挂在
// 生产路由表上且路径正确（report 供前端上报，list/detail/clear 供管理端）。
func TestErrorLogRoutesRegisteredInProductionEngine(t *testing.T) {
	engine := getProductionEngine(t)

	want := map[string]bool{
		"POST /api/v1/error-log/report":  false,
		"GET /api/v1/error-log/list":     false,
		"GET /api/v1/error-log/detail":   false,
		"DELETE /api/v1/error-log/clear": false,
	}
	for _, r := range engine.Routes() {
		key := r.Method + " " + r.Path
		if seen, ok := want[key]; ok {
			if seen {
				t.Fatalf("route %s registered more than once", key)
			}
			want[key] = true
		}
	}
	for key, seen := range want {
		if !seen {
			t.Errorf("route %s missing from production engine", key)
		}
	}
}
