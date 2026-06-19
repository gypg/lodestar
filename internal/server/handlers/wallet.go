package handlers

/*
Lodestar commercial layer — wallet & top-up endpoints.

- User (any logged-in): view own balance, redeem a top-up code.
- Admin (users:write): generate codes, list codes, grant balance directly.

Balance is float USD; relay deducts per-request cost when commercial_mode is on
(see internal/op/billing). This is the no-payment-provider monetization path.
*/

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	apikey "github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/op/email"
	"github.com/gypg/lodestar/internal/op/invite"
	"github.com/gypg/lodestar/internal/op/payment"
	st "github.com/gypg/lodestar/internal/op/stats"
	"github.com/gypg/lodestar/internal/op/topup"
	"github.com/gypg/lodestar/internal/op/user"
	"github.com/gypg/lodestar/internal/op/walletusage"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
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
		).
		AddRoute(
			router.NewRoute("/usage", http.MethodGet).
				Handle(getUsage),
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
		).
		AddRoute(
			router.NewRoute("/invites", http.MethodPost).
				Handle(generateInvites),
		).
		AddRoute(
			router.NewRoute("/invites", http.MethodGet).
				Handle(listInvites),
		).
		AddRoute(
			router.NewRoute("/email-test", http.MethodPost).
				Handle(testEmail),
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

// getUsage returns the current user's own usage, aggregated over their API keys
// (each key's accumulated stats). Drives the user portal usage view.
func getUsage(c *gin.Context) {
	uid := uint(c.GetInt("user_id"))
	keys, err := apikey.ListByUser(uid, c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	type keyUsage struct {
		Name     string  `json:"name"`
		Requests int64   `json:"requests"`
		Tokens   int64   `json:"tokens"`
		Cost     float64 `json:"cost"`
	}
	perKey := make([]keyUsage, 0)
	var totReq, totTok int64
	var totCost float64
	for _, k := range keys {
		s := st.APIKeyGet(k.ID)
		req := s.StatsMetrics.RequestSuccess + s.StatsMetrics.RequestFailed
		tok := s.StatsMetrics.InputToken + s.StatsMetrics.OutputToken
		cost := s.StatsMetrics.InputCost + s.StatsMetrics.OutputCost
		perKey = append(perKey, keyUsage{Name: k.Name, Requests: req, Tokens: tok, Cost: cost})
		totReq += req
		totTok += tok
		totCost += cost
	}
	days := 14
	if v := c.Query("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	series, chartOK, uerr := walletusage.DailySeriesForUser(uid, days, c.Request.Context())
	if uerr != nil {
		resp.InternalError(c)
		return
	}
	heatDays := days
	if heatDays < 30 {
		heatDays = 30
	}
	heatmap, _, herr := walletusage.HeatmapForUser(uid, heatDays, c.Request.Context())
	if herr != nil {
		resp.InternalError(c)
		return
	}
	modelRows, _, merr := walletusage.ModelBreakdownForUser(uid, days, 16, c.Request.Context())
	if merr != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, gin.H{
		"total_requests":        totReq,
		"total_tokens":          totTok,
		"total_cost":            totCost,
		"per_key":               perKey,
		"daily_series":          series,
		"usage_chart_available": chartOK,
		"heatmap_by_day":        heatmap,
		"per_model":             modelRows,
	})
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

func generateInvites(c *gin.Context) {
	var req struct {
		Count int `json:"count"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	codes, err := invite.GenerateCodes(req.Count, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, codes)
}

func testEmail(c *gin.Context) {
	var req struct {
		To string `json:"to"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := email.SendTest(req.To); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func listInvites(c *gin.Context) {
	codes, err := invite.ListCodes(c.Request.Context())
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