package handlers

/*
GGZERO commercial layer — wallet & top-up endpoints.

- User (any logged-in): view own balance, redeem a top-up code.
- Admin (users:write): generate codes, list codes, grant balance directly.

Balance is float USD; relay deducts per-request cost when commercial_mode is on
(see internal/op/billing). This is the no-payment-provider monetization path.
*/

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/op/topup"
	"github.com/lingyuins/octopus/internal/op/user"
	"github.com/lingyuins/octopus/internal/server/auth"
	"github.com/lingyuins/octopus/internal/server/middleware"
	"github.com/lingyuins/octopus/internal/server/resp"
	"github.com/lingyuins/octopus/internal/server/router"
)

func init() {
	router.NewGroupRouter("/api/v1/wallet").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/balance", http.MethodGet).
				Handle(getWallet),
		).
		AddRoute(
			router.NewRoute("/redeem", http.MethodPost).
				Handle(redeemCode),
		)

	router.NewGroupRouter("/api/v1/wallet").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		Use(middleware.RequirePermission(auth.PermUsersWrite)).
		AddRoute(
			router.NewRoute("/codes", http.MethodPost).
				Handle(generateCodes),
		).
		AddRoute(
			router.NewRoute("/codes", http.MethodGet).
				Handle(listCodes),
		).
		AddRoute(
			router.NewRoute("/grant", http.MethodPost).
				Handle(adminGrant),
		)
}

func getWallet(c *gin.Context) {
	uid := uint(c.GetInt("user_id"))
	remaining, used, err := user.GetQuota(uid, c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, gin.H{"quota": remaining, "used_quota": used})
}

func redeemCode(c *gin.Context) {
	var req struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		resp.Error(c, http.StatusBadRequest, "code is required")
		return
	}
	amount, err := topup.Redeem(code, uint(c.GetInt("user_id")), c.Request.Context())
	if err != nil {
		if errors.Is(err, topup.ErrInvalidCode) {
			resp.Error(c, http.StatusBadRequest, "invalid or already-used code")
			return
		}
		resp.InternalError(c)
		return
	}
	resp.Success(c, gin.H{"credited": amount})
}

func generateCodes(c *gin.Context) {
	var req struct {
		Count int     `json:"count"`
		Quota float64 `json:"quota"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	codes, err := topup.GenerateCodes(req.Count, req.Quota, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, codes)
}

func listCodes(c *gin.Context) {
	codes, err := topup.ListCodes(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, codes)
}

func adminGrant(c *gin.Context) {
	var req struct {
		UserID uint    `json:"user_id"`
		Amount float64 `json:"amount"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if req.UserID == 0 {
		resp.Error(c, http.StatusBadRequest, "user_id is required")
		return
	}
	if err := user.AddQuota(req.UserID, req.Amount, c.Request.Context()); err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}
