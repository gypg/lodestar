package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/email"
	"github.com/gypg/lodestar/internal/op/invite"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/twofa"
	usr "github.com/gypg/lodestar/internal/op/user"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
)

func init() {
	publicUserRoutes := router.NewGroupRouter("/api/v1/user").
		Use(middleware.RequireJSON())

	publicUserRoutes.AddRoute(
		router.NewRoute("/login", http.MethodPost).
			Use(middleware.LoginRateLimit()).
			Handle(login),
	)

	// Lodestar: public self-registration, gated by commercial_mode. In self-use
	// mode (default) this returns 403; flip commercial_mode on to open it up.
	publicUserRoutes.AddRoute(
		router.NewRoute("/register", http.MethodPost).
			Use(middleware.LoginRateLimit()).
			Use(middleware.VerifyTurnstile()).
			Handle(register),
	)

	// Lodestar: send an email verification code (used when register_email_required).
	publicUserRoutes.AddRoute(
		router.NewRoute("/send-email-code", http.MethodPost).
			Use(middleware.EmailCodeRateLimit()).
			Handle(sendEmailCode),
	)

	router.NewGroupRouter("/api/v1/user").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/create", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermUsersWrite)).
				Handle(createUser),
		).
		AddRoute(
			router.NewRoute("/change-password", http.MethodPost).
				Handle(changePassword),
		).
		AddRoute(
			router.NewRoute("/change-username", http.MethodPost).
				Handle(changeUsername),
		).
		AddRoute(
			router.NewRoute("/preferences", http.MethodGet).
				Handle(getPreferences),
		).
		AddRoute(
			router.NewRoute("/preferences", http.MethodPost).
				Handle(setPreferences),
		).
		AddRoute(
			router.NewRoute("/status", http.MethodGet).
				Handle(status),
		).
		AddRoute(
			router.NewRoute("/me", http.MethodGet).
				Handle(me),
		).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Use(middleware.RequirePermission(auth.PermUsersRead)).
				Handle(listUsers),
		).
		AddRoute(
			router.NewRoute("/update-role", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermUsersWrite)).
				Handle(updateUserRole),
		).
		AddRoute(
			router.NewRoute("/delete/:id", http.MethodDelete).
				Use(middleware.RequirePermission(auth.PermUsersWrite)).
				Handle(deleteUser),
		)
}

func createUser(c *gin.Context) {
	var req model.UserCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := usr.Create(req, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusBadRequest, "failed to create user")
		return
	}
	resp.Success(c, nil)
}

func listUsers(c *gin.Context) {
	users, err := usr.List(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, users)
}

func updateUserRole(c *gin.Context) {
	var req struct {
		ID   uint   `json:"id" binding:"required"`
		Role string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := usr.UpdateRole(req.ID, req.Role, c.Request.Context()); err != nil {
		if status, msg, ok := classifyUserMutationError(err); ok {
			resp.Error(c, status, msg)
			return
		}
		resp.Error(c, http.StatusBadRequest, "failed to update role")
		return
	}
	resp.Success(c, nil)
}

func deleteUser(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid user id")
		return
	}
	currentUserID := uint(c.GetInt("user_id"))
	if err := usr.Delete(uint(id), currentUserID, c.Request.Context()); err != nil {
		if status, msg, ok := classifyUserMutationError(err); ok {
			resp.Error(c, status, msg)
			return
		}
		resp.Error(c, http.StatusBadRequest, "failed to delete user")
		return
	}
	resp.Success(c, nil)
}

func login(c *gin.Context) {
	var user model.UserLogin
	if err := c.ShouldBindJSON(&user); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	loginKey := c.GetString("login_rate_limit_key")
	userObj, err := usr.Verify(user.Username, user.Password)
	if err != nil {
		if errors.Is(err, usr.ErrBootstrapAlreadySetUp) {
			resp.Error(c, http.StatusConflict, err.Error())
			return
		}
		if isTransientDatabaseError(err) {
			resp.Error(c, http.StatusServiceUnavailable, resp.ErrDatabase)
			return
		}
		if !isCredentialError(err) {
			resp.Error(c, http.StatusInternalServerError, resp.ErrDatabase)
			return
		}
		middleware.RecordLoginFailure(loginKey, time.Now())
		resp.Error(c, http.StatusUnauthorized, resp.ErrUnauthorized)
		return
	}
	middleware.ClearLoginFailures(loginKey)

	// 2FA: if the user has two-factor auth enabled, require a valid TOTP/backup
	// code before issuing a token. Without this check, a successful 2FA setup is
	// purely decorative — any holder of the password could log in regardless.
	twoFAStatus, err := twofa.GetStatus(userObj.ID)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, resp.ErrDatabase)
		return
	}
	if twoFAStatus != nil && twoFAStatus.Enabled {
		if strings.TrimSpace(user.TOTPCode) == "" {
			// Tell the client to prompt for a TOTP code and resubmit.
			resp.Success(c, model.UserLoginResponse{RequiresTwoFactor: true})
			return
		}
		if err := twofa.VerifyLogin(userObj.ID, user.TOTPCode); err != nil {
			middleware.RecordLoginFailure(loginKey, time.Now())
			resp.Error(c, http.StatusUnauthorized, "invalid two-factor code")
			return
		}
	}

	token, expire, err := auth.GenerateJWTToken(user.Expire, userObj.ID, userObj.Role)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, resp.ErrInternalServer)
		return
	}
	middleware.SetJWTCookie(c, token, middleware.JWTExpiryToSeconds(expire))
	resp.Success(c, model.UserLoginResponse{Token: token, ExpireAt: expire})
}

// Lodestar: public self-registration handler. Only succeeds when commercial_mode
// is enabled; creates a viewer-role account and auto-logs in (returns a token).
func register(c *gin.Context) {
	commercialMode, _ := setting.GetBool(model.SettingKeyCommercialMode)
	if !commercialMode {
		resp.Error(c, http.StatusForbidden, "registration is disabled in self-use mode")
		return
	}
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		Expire     int    `json:"expire"`
		InviteCode string `json:"invite_code"`
		Email      string `json:"email"`
		EmailCode  string `json:"email_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	// Lodestar: optional email verification gate (commercial mode only).
	emailRequired := false
	if v, _ := setting.GetBool(model.SettingKeyRegisterEmailRequired); v {
		emailRequired = true
		if strings.TrimSpace(req.Email) == "" || strings.TrimSpace(req.EmailCode) == "" {
			resp.Error(c, http.StatusBadRequest, "邮箱与验证码必填")
			return
		}
		if !email.Verify(req.Email, req.EmailCode) {
			resp.Error(c, http.StatusBadRequest, "验证码错误或已过期")
			return
		}
	}
	// Lodestar: optional invite-code gate (commercial mode only). Pre-validate before
	// creating the user so a taken username doesn't waste the invite; consume after.
	inviteRequired := false
	if v, _ := setting.GetBool(model.SettingKeyRegisterInviteRequired); v {
		inviteRequired = true
		code := strings.TrimSpace(req.InviteCode)
		if code == "" {
			resp.Error(c, http.StatusBadRequest, "邀请码必填")
			return
		}
		if !invite.IsValid(code, c.Request.Context()) {
			resp.Error(c, http.StatusBadRequest, "邀请码无效或已被使用")
			return
		}
	}
	loginKey := c.GetString("login_rate_limit_key")
	if err := usr.Create(model.UserCreateRequest{
		Username: req.Username,
		Password: req.Password,
		Role:     model.UserRoleUser,
	}, c.Request.Context()); err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "username already exists") {
			resp.Error(c, http.StatusConflict, "username already exists")
			return
		}
		resp.Error(c, http.StatusBadRequest, "failed to register")
		return
	}
	// Auto-login the freshly created account.
	userObj, err := usr.Verify(req.Username, req.Password)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, resp.ErrInternalServer)
		return
	}
	if inviteRequired {
		_ = invite.Consume(strings.TrimSpace(req.InviteCode), userObj.ID, c.Request.Context())
	}
	if emailRequired {
		_ = usr.UpdateEmail(userObj.ID, strings.ToLower(strings.TrimSpace(req.Email)), c.Request.Context())
	}
	middleware.ClearLoginFailures(loginKey)
	token, expire, err := auth.GenerateJWTToken(req.Expire, userObj.ID, userObj.Role)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, resp.ErrInternalServer)
		return
	}
	middleware.SetJWTCookie(c, token, middleware.JWTExpiryToSeconds(expire))
	resp.Success(c, model.UserLoginResponse{Token: token, ExpireAt: expire})
}

// Lodestar: send an email verification code (pre-registration). Rate-limited.
func sendEmailCode(c *gin.Context) {
	var req struct {
		Email string `json:"email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := email.GenerateAndSend(req.Email); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func isTransientDatabaseError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "sqlite_busy") || strings.Contains(msg, "sql: database is closed")
}

func isCredentialError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "incorrect username") || strings.Contains(msg, "incorrect password")
}

func changePassword(c *gin.Context) {
	var user model.UserChangePassword
	if err := c.ShouldBindJSON(&user); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	currentUserID := uint(c.GetInt("user_id"))
	if err := usr.ChangePassword(currentUserID, user.OldPassword, user.NewPassword); err != nil {
		if strings.Contains(err.Error(), "incorrect old password") {
			resp.Error(c, http.StatusUnauthorized, resp.ErrUnauthorized)
			return
		}
		resp.Error(c, http.StatusInternalServerError, resp.ErrDatabase)
		return
	}
	resp.Success(c, "password changed successfully")
}

func changeUsername(c *gin.Context) {
	var user model.UserChangeUsername
	if err := c.ShouldBindJSON(&user); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	currentUserID := uint(c.GetInt("user_id"))
	if err := usr.ChangeUsername(currentUserID, user.NewUsername); err != nil {
		if strings.Contains(err.Error(), "same as the old username") || strings.Contains(err.Error(), "username already exists") || strings.Contains(err.Error(), "username is required") {
			resp.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		resp.InternalError(c)
		return
	}
	resp.Success(c, "username changed successfully")
}

func status(c *gin.Context) {
	if !usr.Ready() {
		resp.Error(c, http.StatusConflict, usr.ErrBootstrapAlreadySetUp.Error())
		return
	}
	resp.Success(c, "ok")
}

// Lodestar: per-user UI preferences (opaque JSON). Self-scoped — any logged-in
// user reads/writes their own.
// Lodestar: current authenticated user (id/username/role/balance) — drives the
// role-aware frontend (admin console vs user self-service portal).
func me(c *gin.Context) {
	uid := uint(c.GetInt("user_id"))
	u, err := usr.GetByID(uid, c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, gin.H{
		"id":         u.ID,
		"username":   u.Username,
		"role":       u.Role,
		"quota":      u.Quota,
		"used_quota": u.UsedQuota,
	})
}

func getPreferences(c *gin.Context) {
	currentUserID := uint(c.GetInt("user_id"))
	user, err := usr.GetByID(currentUserID, c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, gin.H{"preferences": user.Preferences})
}

func setPreferences(c *gin.Context) {
	var req struct {
		Preferences string `json:"preferences"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	// N-02: cap preferences payload to 64 KB to prevent abuse.
	const maxPreferencesSize = 64 * 1024
	if len(req.Preferences) > maxPreferencesSize {
		resp.Error(c, http.StatusBadRequest, "preferences payload too large (max 64KB)")
		return
	}
	currentUserID := uint(c.GetInt("user_id"))
	if err := usr.UpdatePreferences(currentUserID, req.Preferences, c.Request.Context()); err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func classifyUserMutationError(err error) (int, string, bool) {
	if err == nil {
		return 0, "", false
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "user not found"):
		return http.StatusNotFound, "user not found", true
	case strings.Contains(msg, "invalid role"):
		return http.StatusBadRequest, err.Error(), true
	case strings.Contains(msg, "username already exists"):
		return http.StatusConflict, "username already exists", true
	case strings.Contains(msg, "cannot delete the active user"):
		return http.StatusBadRequest, "cannot delete the active user", true
	default:
		return 0, "", false
	}
}
