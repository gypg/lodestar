package middleware

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/model"
	ak "github.com/gypg/lodestar/internal/op/apikey"
	billing "github.com/gypg/lodestar/internal/op/billing"
	"github.com/gypg/lodestar/internal/op/stats"
	"github.com/gypg/lodestar/internal/op/user"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/resp"
)

const (
	// JWTCookieName is the name of the HttpOnly cookie that stores the JWT token.
	JWTCookieName = "token"
)

// extractToken reads the JWT from the cookie first (HttpOnly), then falls back
// to the Authorization header for backward compatibility with API clients.
func extractToken(c *gin.Context) string {
	// 1. Try cookie (preferred, HttpOnly).
	if token, err := c.Cookie(JWTCookieName); err == nil && token != "" {
		return token
	}
	// 2. Fallback to Authorization header (backwards-compatible).
	if auth := c.GetHeader("Authorization"); auth != "" {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractToken(c)
		if token == "" {
			resp.Error(c, http.StatusUnauthorized, resp.ErrUnauthorized)
			c.Abort()
			return
		}
		valid, userID, role := auth.VerifyJWTToken(token)
		if !valid {
			// If the token came from a cookie, clear the stale cookie so the
			// client falls back to the login flow cleanly.
			if _, err := c.Cookie(JWTCookieName); err == nil {
				SetJWTCookie(c, "", -1)
			}
			resp.Error(c, http.StatusUnauthorized, resp.ErrUnauthorized)
			c.Abort()
			return
		}

		if userID == 0 {
			resp.Error(c, http.StatusUnauthorized, resp.ErrUnauthorized)
			c.Abort()
			return
		}

		currentUser, err := user.GetByID(userID, c.Request.Context())
		if err != nil {
			resp.Error(c, http.StatusUnauthorized, resp.ErrUnauthorized)
			c.Abort()
			return
		}
		role = currentUser.Role
		if role == "" {
			role = model.UserRoleViewer
		}
		c.Set("user_id", int(currentUser.ID))
		c.Set("username", currentUser.Username)
		c.Set("user_role", role)
		c.Next()
	}
}

// SetJWTCookie writes (or clears) the JWT HttpOnly cookie on the response.
// Pass maxAge <= 0 to delete the cookie.
func SetJWTCookie(c *gin.Context, token string, maxAge int) {
	if token == "" {
		maxAge = -1
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		JWTCookieName,
		token,
		maxAge,
		"/",   // path
		"",    // domain (empty = current host)
		false, // secure (set true when serving over HTTPS)
		true,  // httpOnly
	)
}

// JWTExpiryToSeconds converts an expiry string (RFC3339 or "30m" style) to
// seconds for use as the cookie maxAge. Returns a default of 15 minutes on
// parse failure.
func JWTExpiryToSeconds(expiry string) int {
	// Try RFC3339 (the format auth.GenerateJWTToken returns).
	if t, err := time.Parse(time.RFC3339, expiry); err == nil {
		secs := int(time.Until(t).Seconds())
		if secs > 0 {
			return secs
		}
	}
	return 15 * 60 // default 15 minutes
}

func APIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		var apiKey string
		var requestType string

		if key := c.Request.Header.Get("x-api-key"); key != "" {
			apiKey = key
			requestType = "anthropic"
		} else if auth := c.Request.Header.Get("Authorization"); auth != "" {
			apiKey = strings.TrimPrefix(auth, "Bearer ")
			requestType = "openai"
		}

		if apiKey == "" {
			resp.Error(c, http.StatusUnauthorized, resp.ErrUnauthorized)
			c.Abort()
			return
		}

		if !strings.HasPrefix(apiKey, "sk-"+conf.APP_NAME+"-") {
			resp.Error(c, http.StatusUnauthorized, resp.ErrUnauthorized)
			c.Abort()
			return
		}
		apiKeyObj, err := ak.GetByKey(apiKey, c.Request.Context())
		if err != nil {
			resp.Error(c, http.StatusUnauthorized, resp.ErrUnauthorized)
			c.Abort()
			return
		}
		if !apiKeyObj.Enabled {
			resp.Error(c, http.StatusUnauthorized, "API key is disabled")
			c.Abort()
			return
		}
		if apiKeyObj.ExpireAt > 0 && apiKeyObj.ExpireAt < time.Now().Unix() {
			resp.Error(c, http.StatusUnauthorized, "API key has expired")
			c.Abort()
			return
		}
		statsAPIKey := stats.APIKeyGet(apiKeyObj.ID)
		if apiKeyObj.MaxCost > 0 && apiKeyObj.MaxCost < statsAPIKey.StatsMetrics.OutputCost+statsAPIKey.StatsMetrics.InputCost {
			resp.Error(c, http.StatusUnauthorized, "API key has reached the max cost")
			c.Abort()
			return
		}
		// Token 用量上限：累计 Token = 输入 + 输出，超限则拒绝
		if apiKeyObj.MaxTokens > 0 {
			usedTokens := statsAPIKey.StatsMetrics.InputToken + statsAPIKey.StatsMetrics.OutputToken
			if usedTokens >= apiKeyObj.MaxTokens {
				resp.Error(c, http.StatusUnauthorized, "API key has reached the max token limit")
				c.Abort()
				return
			}
		}
		// Lodestar commercial: when commercial_mode is on, the key owner must have
		// positive balance (no-op for unowned/admin keys or in self-use mode).
		if !billing.HasBalanceForKey(apiKeyObj.ID, c.Request.Context()) {
			resp.Error(c, http.StatusPaymentRequired, "insufficient balance, please top up")
			c.Abort()
			return
		}
		if !isIPAllowed(c.ClientIP(), apiKeyObj.AllowedIPs) {
			resp.Error(c, http.StatusForbidden, "IP address not allowed for this API key")
			c.Abort()
			return
		}
		c.Set("request_type", requestType)
		c.Set("supported_models", apiKeyObj.SupportedModels)
		c.Set("api_key_id", apiKeyObj.ID)
		c.Set("rate_limit_rpm", apiKeyObj.RateLimitRPM)
		c.Set("rate_limit_tpm", apiKeyObj.RateLimitTPM)
		c.Set("per_model_quota_json", apiKeyObj.PerModelQuotaJSON)
		c.Set("excluded_channels", apiKeyObj.ExcludedChannels)
		c.Next()
	}
}

// isIPAllowed checks whether clientIP matches any entry in the allowedIPs list.
// allowedIPs is a comma-separated list of IPs or CIDR ranges (e.g. "10.0.0.1,192.168.0.0/16").
// An empty allowedIPs string means all IPs are allowed.
func isIPAllowed(clientIP string, allowedIPs string) bool {
	if allowedIPs == "" {
		return true
	}
	parsedClient := parseClientIP(clientIP)
	if parsedClient == nil {
		return false
	}
	for _, entry := range strings.Split(allowedIPs, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			_, cidr, err := net.ParseCIDR(entry)
			if err != nil {
				continue
			}
			if cidr.Contains(parsedClient) {
				return true
			}
		} else {
			allowed := net.ParseIP(entry)
			if allowed != nil && allowed.Equal(parsedClient) {
				return true
			}
		}
	}
	return false
}

func parseClientIP(clientIP string) net.IP {
	client := strings.TrimSpace(clientIP)
	if client == "" {
		return nil
	}
	if host, _, err := net.SplitHostPort(client); err == nil {
		client = host
	}
	return net.ParseIP(client)
}
