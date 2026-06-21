package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/twofa"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/pquerna/otp/totp"
)

// setupLoginEngine prepares an in-memory SQLite DB, bootstraps an admin user,
// and wires a gin engine exposing only /api/v1/user/login (the route under
// test). Returns the engine.
func setupLoginEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	conf.AppConfig.Auth.JWTSecret = "test-jwt-secret"

	if err := op.UserInit(); err != nil {
		t.Fatalf("user init: %v", err)
	}
	if err := op.UserBootstrapCreate("admin", "super-secret-123"); err != nil {
		t.Fatalf("bootstrap user: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}

	engine := gin.New()
	engine.POST(
		"/api/v1/user/login",
		middleware.RequireJSON(),
		middleware.LoginRateLimit(),
		login,
	)
	return engine
}

func doLogin(t *testing.T, engine *gin.Engine, payload string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/login", bytes.NewReader([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// dataField extracts the data object from a resp.ResponseStruct JSON body.
func dataField(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var response resp.ResponseStruct
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, string(body))
	}
	m, ok := response.Data.(map[string]any)
	if !ok {
		t.Fatalf("response.Data type = %T, want map; body=%s", response.Data, string(body))
	}
	return m
}

// enable2FAForAdmin runs the full 2FA setup+enable flow for the bootstrapped
// admin user and returns the TOTP secret so tests can mint valid codes.
func enable2FAForAdmin(t *testing.T) string {
	t.Helper()
	admin := op.UserGet()
	status, err := twofa.Setup(admin.ID)
	if err != nil {
		t.Fatalf("twofa.Setup: %v", err)
	}
	code, err := totp.GenerateCode(status.Secret, time.Now())
	if err != nil {
		t.Fatalf("totp.GenerateCode: %v", err)
	}
	if err := twofa.Enable(admin.ID, code); err != nil {
		t.Fatalf("twofa.Enable: %v", err)
	}
	return status.Secret
}

// TestLogin_2FAEnabledRequiresCode closes the 2FA wiring gap end-to-end:
// a password-only login for a 2FA-enabled user must NOT return a token.
func TestLogin_2FAEnabledRequiresCode(t *testing.T) {
	engine := setupLoginEngine(t)
	enable2FAForAdmin(t)

	code, body := doLogin(t, engine, `{"username":"admin","password":"super-secret-123","expire":60}`)
	if code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body=%s", code, http.StatusOK, string(body))
	}

	data := dataField(t, body)
	// requires_two_factor:true is serialized with a zero-value token/expire_at
	// (UserLoginResponse omits them via ,omitempty on bool only; token/expire_at
	// remain empty strings). The critical assertion: no usable token is issued.
	if tok, _ := data["token"].(string); strings.TrimSpace(tok) != "" {
		t.Fatalf("password-only login must not issue a usable token when 2FA is enabled; token=%q", tok)
	}
	if req, ok := data["requires_two_factor"].(bool); !ok || !req {
		t.Fatalf("expected requires_two_factor=true, got data=%v", data)
	}
}

// TestLogin_2FAWithValidCodeIssuesToken confirms the happy path: valid TOTP
// code yields a usable JWT.
func TestLogin_2FAWithValidCodeIssuesToken(t *testing.T) {
	engine := setupLoginEngine(t)
	secret := enable2FAForAdmin(t)

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("totp.GenerateCode: %v", err)
	}
	payload := fmt.Sprintf(`{"username":"admin","password":"super-secret-123","totp_code":%q,"expire":60}`, code)

	status, body := doLogin(t, engine, payload)
	if status != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body=%s", status, http.StatusOK, string(body))
	}

	data := dataField(t, body)
	tok, _ := data["token"].(string)
	if strings.TrimSpace(tok) == "" {
		t.Fatalf("expected a token with valid 2FA code; data=%v", data)
	}

	valid, _, role := auth.VerifyJWTToken(tok)
	if !valid {
		t.Fatal("token issued after 2FA is invalid")
	}
	if role != model.UserRoleAdmin {
		t.Fatalf("token role = %q, want %q", role, model.UserRoleAdmin)
	}
}

// TestLogin_2FAWithInvalidCodeRejected confirms the negative path: a wrong
// code is rejected with 401, no token.
func TestLogin_2FAWithInvalidCodeRejected(t *testing.T) {
	engine := setupLoginEngine(t)
	enable2FAForAdmin(t)

	payload := `{"username":"admin","password":"super-secret-123","totp_code":"000000","expire":60}`
	status, body := doLogin(t, engine, payload)
	if status != http.StatusUnauthorized {
		t.Fatalf("login status = %d, want %d; body=%s", status, http.StatusUnauthorized, string(body))
	}
}
