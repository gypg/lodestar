package middleware

import (
	"net/http"

	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gin-gonic/gin"
)

// RequirePermission returns a middleware that checks if the authenticated user has the required permission.
func RequirePermission(perm auth.Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		role := c.GetString("user_role")
		if role == "" {
			resp.Error(c, http.StatusForbidden, "permission denied")
			c.Abort()
			return
		}
		if !auth.HasPermission(role, perm) {
			resp.Error(c, http.StatusForbidden, "permission denied")
			c.Abort()
			return
		}
		c.Next()
	}
}
