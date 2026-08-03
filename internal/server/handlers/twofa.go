package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/twofa"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
	"github.com/gypg/lodestar/internal/utils/log"
)

func init() {
	router.NewGroupRouter("/api/v1/user/2fa").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/setup", http.MethodPost).
				Handle(handle2FASetup),
		).
		AddRoute(
			router.NewRoute("/enable", http.MethodPost).
				Handle(handle2FAEnable),
		).
		AddRoute(
			router.NewRoute("/disable", http.MethodPost).
				Handle(handle2FADisable),
		).
		AddRoute(
			router.NewRoute("/status", http.MethodGet).
				Handle(handle2FAStatus),
		).
		AddRoute(
			router.NewRoute("/backup-codes", http.MethodPost).
				Handle(handle2FARegenerateBackupCodes),
		)

	// Admin endpoint for force-disabling a user's 2FA.
	router.NewGroupRouter("/api/v1/admin/2fa").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermUsersWrite)).
		AddRoute(
			router.NewRoute("/disable/:id", http.MethodPost).
				Handle(handleAdminDisable2FA),
		)
}

type codeRequest struct {
	Code string `json:"code" binding:"required"`
}

func handle2FASetup(c *gin.Context) {
	userID := uint(c.GetInt("user_id"))
	result, err := twofa.Setup(userID)
	if err != nil {
		log.Errorf("2FA setup failed (user=%d): %v", userID, err)
		resp.Error(c, http.StatusBadRequest, "Failed to set up 2FA")
		return
	}
	resp.Success(c, result)
}

func handle2FAEnable(c *gin.Context) {
	var req codeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}

	userID := uint(c.GetInt("user_id"))
	if err := twofa.Enable(userID, req.Code); err != nil {
		log.Errorf("2FA enable failed (user=%d): %v", userID, err)
		resp.Error(c, http.StatusBadRequest, "Invalid verification code")
		return
	}
	resp.Success(c, nil)
}

func handle2FADisable(c *gin.Context) {
	var req codeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}

	userID := uint(c.GetInt("user_id"))
	if err := twofa.Disable(userID, req.Code); err != nil {
		log.Errorf("2FA disable failed (user=%d): %v", userID, err)
		if err == model.ErrTwoFANotEnabled {
			resp.Error(c, http.StatusNotFound, "2FA is not enabled")
			return
		}
		resp.Error(c, http.StatusBadRequest, "Invalid verification code")
		return
	}
	resp.Success(c, nil)
}

func handle2FAStatus(c *gin.Context) {
	userID := uint(c.GetInt("user_id"))
	result, err := twofa.GetStatus(userID)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, result)
}

func handle2FARegenerateBackupCodes(c *gin.Context) {
	var req codeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}

	userID := uint(c.GetInt("user_id"))
	codes, err := twofa.RegenerateBackupCodes(userID, req.Code)
	if err != nil {
		log.Errorf("2FA regenerate backup codes failed (user=%d): %v", userID, err)
		resp.Error(c, http.StatusBadRequest, "Invalid verification code")
		return
	}
	resp.Success(c, gin.H{"backup_codes": codes})
}

func handleAdminDisable2FA(c *gin.Context) {
	idStr := c.Param("id")
	targetID, err := parseUintParam(idStr)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid user id")
		return
	}

	if err := twofa.AdminDisable(targetID); err != nil {
		if err == model.ErrTwoFANotEnabled {
			resp.Error(c, http.StatusNotFound, "user does not have 2FA enabled")
			return
		}
		log.Errorf("admin disable 2FA failed (target=%d): %v", targetID, err)
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}
