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
	"github.com/lingyuins/octopus/internal/op/payment"
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
		).
		AddRoute(
			router.NewRoute("/topup", http.MethodPost).
				Handle(requestTopup),
		)

	// Public, no-auth Epay callback (gateway posts form / query params, not JSON).
	router.NewGroupRouter("/api/v1/wallet").
		AddRoute(
			router.NewRoute("/epay/notify", http.MethodPost).
				Handle(epayNotify),
		).
		AddRoute(
			router.NewRoute("/epay/notify", http.MethodGet).
				Handle(epayNotify),
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
	resp.Success(c, gin.H{"quota": remaining, "used_quota": used, "epay_configured": payment.EpayConfigured()})
}

// requestTopup creates an online (Epay) payment order and returns the gateway
// URL + signed params for the frontend to submit.
func requestTopup(c *gin.Context) {
	var req struct {
		Amount float64 `json:"amount"`
		Method string  `json:"method"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	uri, params, err := payment.CreateEpayOrder(uint(c.GetInt("user_id")), req.Amount, req.Method, c.Request.Context())
	if err != nil {
		if errors.Is(err, payment.ErrNotConfigured) {
			resp.Error(c, http.StatusBadRequest, "管理员未配置在线支付")
			return
		}
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, gin.H{"url": uri, "params": params})
}

// epayNotify is the public Epay callback: verify signature + credit the user once.
func epayNotify(c *gin.Context) {
	params := map[string]string{}
	if c.Request.Method == http.MethodPost {
		_ = c.Request.ParseForm()
		for k := range c.Request.PostForm {
			params[k] = c.Request.PostForm.Get(k)
		}
	} else {
		for k := range c.Request.URL.Query() {
			params[k] = c.Request.URL.Query().Get(k)
		}
	}
	if payment.HandleEpayNotify(params, c.Request.Context()) {
		_, _ = c.Writer.Write([]byte("success"))
	} else {
		_, _ = c.Writer.Write([]byte("fail"))
	}
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
