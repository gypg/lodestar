package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/oauth"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
	"github.com/gypg/lodestar/internal/utils/log"
)

func init() {
	// Public endpoints — no auth required for the OAuth flow.
	router.NewGroupRouter("/api/v1/oauth").
		AddRoute(
			router.NewRoute("/github/state", http.MethodGet).
				Handle(handleOAuthGitHubState),
		).
		AddRoute(
			router.NewRoute("/github/callback", http.MethodGet).
				Handle(handleOAuthGitHubCallback),
		)

	// Authenticated endpoints — binding management.
	router.NewGroupRouter("/api/v1/user/oauth").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/github/bind", http.MethodPost).
				Handle(handleOAuthGitHubBind),
		).
		AddRoute(
			router.NewRoute("/github/unbind", http.MethodPost).
				Handle(handleOAuthGitHubUnbind),
		).
		AddRoute(
			router.NewRoute("/github/status", http.MethodGet).
				Handle(handleOAuthGitHubStatus),
		)

	// Admin endpoint — view another user's binding.
	router.NewGroupRouter("/api/v1/admin/oauth").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermUsersRead)).
		AddRoute(
			router.NewRoute("/github/binding/:id", http.MethodGet).
				Handle(handleAdminGetOAuthBinding),
		)
}

// ---------------------------------------------------------------------------
// Public handlers
// ---------------------------------------------------------------------------

func handleOAuthGitHubState(c *gin.Context) {
	state, err := oauth.GenerateState(c.Request.Context())
	if err != nil {
		log.Errorf("OAuth state generation failed: %v", err)
		resp.InternalError(c)
		return
	}

	clientID, _ := setting.GetString(model.SettingKeyGitHubOAuthClientID)
	clientID = strings.TrimSpace(clientID)

	authorizeURL := "https://github.com/login/oauth/authorize?client_id=" +
		url.QueryEscape(clientID) +
		"&state=" + url.QueryEscape(state) +
		"&scope=read:user+user:email"

	resp.Success(c, gin.H{
		"state":         state,
		"authorize_url": authorizeURL,
	})
}

func handleOAuthGitHubCallback(c *gin.Context) {
	// 1. Validate CSRF state.
	state := c.Query("state")
	if state == "" || !oauth.ValidateState(state) {
		oauthRedirectError(c, "invalid or expired OAuth state")
		return
	}

	// 2. Handle provider error.
	if errCode := c.Query("error"); errCode != "" {
		desc := c.Query("error_description")
		if desc == "" {
			desc = errCode
		}
		oauthRedirectError(c, desc)
		return
	}

	// 3. Check if OAuth is enabled.
	if !oauth.IsEnabled() {
		oauthRedirectError(c, "GitHub OAuth is not enabled")
		return
	}

	// 4. Exchange code for token and get user info.
	code := c.Query("code")
	ghUser, err := oauth.ExchangeCodeAndUser(c.Request.Context(), code)
	if err != nil {
		oauthRedirectError(c, err.Error())
		return
	}

	// 5. Find or create user.
	userObj, err := oauth.FindOrCreateUser(c.Request.Context(), ghUser)
	if err != nil {
		oauthRedirectError(c, err.Error())
		return
	}

	// 6. Generate JWT and redirect to frontend with token.
	token, expire, err := auth.GenerateJWTToken(-1, userObj.ID, userObj.Role)
	if err != nil {
		oauthRedirectError(c, "failed to generate token")
		return
	}

	frontendBase := oauthFrontendBase(c)
	redirectURL := frontendBase + "?token=" + url.QueryEscape(token) + "&expire=" + url.QueryEscape(expire)
	c.Redirect(http.StatusTemporaryRedirect, redirectURL)
}

// ---------------------------------------------------------------------------
// Authenticated handlers
// ---------------------------------------------------------------------------

type oauthBindRequest struct {
	Code  string `json:"code" binding:"required"`
	State string `json:"state" binding:"required"`
}

func handleOAuthGitHubBind(c *gin.Context) {
	var req oauthBindRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}

	if !oauth.ValidateState(req.State) {
		resp.Error(c, http.StatusBadRequest, "invalid or expired OAuth state")
		return
	}

	ghUser, err := oauth.ExchangeCodeAndUser(c.Request.Context(), req.Code)
	if err != nil {
		log.Errorf("OAuth exchange failed: %v", err)
		resp.Error(c, http.StatusBadRequest, "GitHub authentication failed")
		return
	}

	userID := uint(c.GetInt("user_id"))
	if err := oauth.BindUser(c.Request.Context(), userID, ghUser); err != nil {
		if err == oauth.ErrAlreadyBound {
			resp.Error(c, http.StatusConflict, err.Error())
			return
		}
		resp.InternalError(c)
		return
	}

	resp.Success(c, gin.H{"message": "GitHub account bound successfully"})
}

func handleOAuthGitHubUnbind(c *gin.Context) {
	userID := uint(c.GetInt("user_id"))
	if err := oauth.UnbindUser(c.Request.Context(), userID); err != nil {
		if err == oauth.ErrNotBound {
			resp.Error(c, http.StatusNotFound, err.Error())
			return
		}
		resp.InternalError(c)
		return
	}
	resp.Success(c, gin.H{"message": "GitHub account unbound successfully"})
}

func handleOAuthGitHubStatus(c *gin.Context) {
	userID := uint(c.GetInt("user_id"))
	enabled := oauth.IsEnabled()
	binding, _ := oauth.GetBinding(userID)

	result := gin.H{
		"enabled": enabled,
		"bound":   binding != nil,
	}
	if binding != nil {
		result["github_username"] = binding.ProviderUsername
		result["github_id"] = binding.ProviderUserID
	}
	resp.Success(c, result)
}

// ---------------------------------------------------------------------------
// Admin handler
// ---------------------------------------------------------------------------

func handleAdminGetOAuthBinding(c *gin.Context) {
	idStr := c.Param("id")
	targetID, err := parseUintParam(idStr)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid user id")
		return
	}

	binding, err := oauth.GetBinding(targetID)
	if err != nil {
		resp.Error(c, http.StatusNotFound, "no GitHub OAuth binding found for this user")
		return
	}
	resp.Success(c, binding)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// oauthRedirectError sends the user back to the frontend with an error param.
func oauthRedirectError(c *gin.Context, errMsg string) {
	frontendBase := oauthFrontendBase(c)
	redirectURL := frontendBase + "?error=" + url.QueryEscape(errMsg)
	c.Redirect(http.StatusTemporaryRedirect, redirectURL)
}

// oauthFrontendBase returns the frontend URL for OAuth redirect.
// Requires PublicAPIBaseURL to be configured. Falls back to request Host
// (server-controlled) but NEVER to Origin/Referer headers (attacker-controlled).
func oauthFrontendBase(c *gin.Context) string {
	base, _ := setting.GetString(model.SettingKeyPublicAPIBaseURL)
	base = strings.TrimSpace(base)
	if base != "" {
		return strings.TrimRight(base, "/") + "/#/oauth/callback"
	}
	// Fallback to request Host (server-controlled, not attacker-controlled).
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host + "/#/oauth/callback"
}
