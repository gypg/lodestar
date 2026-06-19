package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
)

const turnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// turnstileVerifyResponse is the JSON structure returned by the Cloudflare Turnstile siteverify API.
type turnstileVerifyResponse struct {
	Success     bool     `json:"success"`
	ErrorCodes []string `json:"error-codes,omitempty"`
}

// VerifyTurnstile returns a gin.HandlerFunc that verifies the Cloudflare Turnstile
// token present in the request body field "cf_turnstile_response" or the HTTP header
// "cf-turnstile-response". If Turnstile is not enabled via settings, the middleware
// is a no-op and calls c.Next().
func VerifyTurnstile() gin.HandlerFunc {
	client := &http.Client{Timeout: 5 * time.Second}

	return func(c *gin.Context) {
		enabled, _ := setting.GetBool(model.SettingKeyTurnstileEnabled)
		if !enabled {
			c.Next()
			return
		}

		secretKey, _ := setting.GetString(model.SettingKeyTurnstileSecretKey)
		if secretKey == "" {
			c.Next()
			return
		}

		// Extract token from header or body field.
		token := strings.TrimSpace(c.GetHeader("cf-turnstile-response"))
		if token == "" {
			// Try body field (form or JSON).
			token = strings.TrimSpace(c.PostForm("cf_turnstile_response"))
		}
		if token == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "turnstile token is required",
			})
			return
		}

		remoteIP := c.ClientIP()

		form := url.Values{}
		form.Set("secret", secretKey)
		form.Set("response", token)
		if remoteIP != "" {
			form.Set("remoteip", remoteIP)
		}

		resp, err := client.PostForm(turnstileVerifyURL, form)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "turnstile verification failed",
			})
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "turnstile verification failed",
			})
			return
		}

		var result turnstileVerifyResponse
		if err := json.Unmarshal(body, &result); err != nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "turnstile verification failed",
			})
			return
		}

		if !result.Success {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "turnstile verification failed",
			})
			return
		}

		c.Next()
	}
}
