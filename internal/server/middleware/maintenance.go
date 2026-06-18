package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/resp"
)

// maintenanceExemptPaths are write routes that must stay reachable during
// maintenance: the bootstrap/status probe (frontend uses it to render the
// maintenance screen), public overview, and the auth entry points (login,
// register, email-code, passkey login) so an admin can still sign in to turn
// maintenance off. Relay (/v1/*) is outside /api/v1/ and is never gated —
// maintenance mode is a management-plane control, not a production-traffic
// kill switch.
var maintenanceExemptPaths = map[string]bool{
	"/api/v1/bootstrap/status":      true,
	"/api/v1/bootstrap/create-admin": true,
	"/api/v1/public/overview":       true,
	"/api/v1/user/login":            true,
	"/api/v1/user/register":         true,
	"/api/v1/user/send-email-code":  true,
	"/api/v1/webauthn/login/begin":  true,
	"/api/v1/webauthn/login/finish": true,
	"/api/v1/ops/health":            true,
}

// isMaintenanceWriteRequest reports whether a request is a management-plane
// write that maintenance mode should gate: POST/PUT/PATCH/DELETE under
// /api/v1/, excluding the exempt paths above. GET and the relay surface
// (/v1/*) are never gated.
func isMaintenanceWriteRequest(method, path string) bool {
	if !strings.HasPrefix(path, "/api/v1/") {
		return false
	}
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	return !maintenanceExemptPaths[path]
}

// MaintenanceGuard blocks non-staff management-plane writes while
// maintenance_mode is on. Staff (admin/editor) requests pass through so an
// admin can perform the maintenance and turn it off. The guard parses the JWT
// itself (it runs as a global middleware, before per-route Auth()), but only
// when a request actually needs gating — no token parse for reads, relay, or
// exempt paths.
func MaintenanceGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isMaintenanceWriteRequest(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}

		on, _ := setting.GetBool(model.SettingKeyMaintenanceMode)
		if !on {
			c.Next()
			return
		}

		// Maintenance is on and this is a gated write. Allow staff.
		if isStaffRequest(c) {
			c.Next()
			return
		}

		resp.Error(c, http.StatusServiceUnavailable, "site is under maintenance, please try again later")
		c.Abort()
	}
}

// isStaffRequest reports whether the request carries a valid staff JWT
// (admin or editor). Anonymous or non-staff requests return false. Token
// parse failures are treated as non-staff (fail closed under maintenance).
func isStaffRequest(c *gin.Context) bool {
	token := c.GetHeader("Authorization")
	if token == "" {
		return false
	}
	valid, userID, role := auth.VerifyJWTToken(strings.TrimPrefix(token, "Bearer "))
	if !valid || userID == 0 {
		return false
	}
	return role == model.UserRoleAdmin || role == model.UserRoleEditor
}
