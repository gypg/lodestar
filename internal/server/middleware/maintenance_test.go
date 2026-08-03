package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/server/auth"
)

func setMaintenance(t *testing.T, on bool) {
	t.Helper()
	val := "false"
	if on {
		val = "true"
	}
	setting.GetCache().Set(model.SettingKeyMaintenanceMode, val)
}

func newMaintenanceEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	conf.AppConfig.Auth.JWTSecret = "test-jwt-secret"
	r := gin.New()
	r.Use(MaintenanceGuard())
	r.Any("/api/v1/*any", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	r.Any("/v1/*any", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	return r
}

func staffToken(t *testing.T, role string) string {
	t.Helper()
	token, _, err := auth.GenerateJWTToken(60, 1, role)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return token
}

func doRequest(r *gin.Engine, method, path, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", "Bearer "+authHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestMaintenanceGuardAllowsAllWhenOff(t *testing.T) {
	setMaintenance(t, false)
	r := newMaintenanceEngine(t)
	// Non-staff write with no token: passes because maintenance is off.
	w := doRequest(r, http.MethodPost, "/api/v1/channel/create", "")
	if w.Code != http.StatusOK {
		t.Fatalf("maintenance off: got %d, want 200", w.Code)
	}
}

func TestMaintenanceGuardBlocksNonStaffWriteWhenOn(t *testing.T) {
	setMaintenance(t, true)
	r := newMaintenanceEngine(t)
	// No token -> not staff -> blocked.
	w := doRequest(r, http.MethodPost, "/api/v1/channel/create", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("maintenance on, no token: got %d, want 503", w.Code)
	}
}

func TestMaintenanceGuardBlocksUserRoleWriteWhenOn(t *testing.T) {
	setMaintenance(t, true)
	r := newMaintenanceEngine(t)
	// Logged-in commercial user (role "user") -> not staff -> blocked.
	w := doRequest(r, http.MethodPost, "/api/v1/wallet/redeem", staffToken(t, model.UserRoleUser))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("maintenance on, user role: got %d, want 503", w.Code)
	}
}

func TestMaintenanceGuardAllowsStaffWriteWhenOn(t *testing.T) {
	setMaintenance(t, true)
	r := newMaintenanceEngine(t)
	for _, role := range []string{model.UserRoleAdmin, model.UserRoleEditor} {
		w := doRequest(r, http.MethodPost, "/api/v1/channel/create", staffToken(t, role))
		if w.Code != http.StatusOK {
			t.Fatalf("maintenance on, staff %s: got %d, want 200", role, w.Code)
		}
	}
}

func TestMaintenanceGuardAllowsReadsWhenOn(t *testing.T) {
	setMaintenance(t, true)
	r := newMaintenanceEngine(t)
	// GET requests are never gated, even without a token.
	w := doRequest(r, http.MethodGet, "/api/v1/channel/list", "")
	if w.Code != http.StatusOK {
		t.Fatalf("maintenance on, GET: got %d, want 200", w.Code)
	}
}

func TestMaintenanceGuardAllowsExemptPathsWhenOn(t *testing.T) {
	setMaintenance(t, true)
	r := newMaintenanceEngine(t)
	exempt := []string{
		"/api/v1/bootstrap/status",
		"/api/v1/public/overview",
		"/api/v1/user/login",
		"/api/v1/user/register",
		"/api/v1/user/send-email-code",
		"/api/v1/ops/health",
	}
	for _, p := range exempt {
		// Use POST since GET would pass anyway; we want to verify the exempt-path rule.
		w := doRequest(r, http.MethodPost, p, "")
		if w.Code != http.StatusOK {
			t.Fatalf("maintenance on, exempt POST %s: got %d, want 200", p, w.Code)
		}
	}
}

func TestMaintenanceGuardDoesNotGateRelaySurface(t *testing.T) {
	setMaintenance(t, true)
	r := newMaintenanceEngine(t)
	// /v1/* is the relay surface (outside /api/v1/); maintenance must not
	// touch production inference traffic.
	w := doRequest(r, http.MethodPost, "/v1/chat/completions", "")
	if w.Code != http.StatusOK {
		t.Fatalf("maintenance on, relay POST: got %d, want 200 (relay not gated)", w.Code)
	}
}

func TestMaintenanceGuardAllowsInvalidTokenAsBlocked(t *testing.T) {
	setMaintenance(t, true)
	r := newMaintenanceEngine(t)
	// Garbage token -> parse fails -> treated as non-staff -> blocked.
	w := doRequest(r, http.MethodPost, "/api/v1/channel/create", "not-a-real-token")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("maintenance on, bad token: got %d, want 503 (fail closed)", w.Code)
	}
}
