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
	"github.com/gypg/lodestar/internal/model"
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

	// Public, no-auth Epay callback (gateway posts form params, not JSON).
	// Only POST accepted — GET is disabled to prevent CSRF via query-string
	// forgery. The Epay gateway always uses POST for server-to-server notify.
	router.NewGroupRouter("/api/v1/wallet").
		AddRoute(
			router.NewRoute("/epay/notify", http.MethodPost).
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
		).
		AddRoute(
			router.NewRoute("/reconcile", http.MethodGet).
				Handle(reconcileWallets),
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
// Only POST is accepted (GET was removed to prevent CSRF via query-string forgery).
func epayNotify(c *gin.Context) {
	params := map[string]string{}
	_ = c.Request.ParseForm()
	for k := range c.Request.PostForm {
		params[k] = c.Request.PostForm.Get(k)
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

// reconcileWallets 列出所有对不上账的用户 —— 不变式是
// `quota == Σ(quota_ledger.delta) - used_quota`。
//
// 只读体检入口：漂移意味着有一笔余额改动绕过了流水（漏斗之外的写入点），或者流水写了
// 但余额没落地。正常情况返回空数组。
func reconcileWallets(c *gin.Context) {
	drifts, err := user.ReconcileDrifts(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, gin.H{"drifts": drifts, "count": len(drifts)})
}

func listInvites(c *gin.Context) {
	codes, err := invite.ListCodes(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, codes)
}

// adminGrant 调整某个用户的余额。amount 有符号：正数加款，负数扣款（纠错、平账）。
//
// WO-017：改动走 user.MutateQuota 漏斗，余额与流水行落在同一事务内。三点不可退让：
//   - reason 必填 —— 无理由的余额改动等于无痕，用户争议时无从查证。
//   - ActorID 填**操作的管理员**，不是受益人 req.UserID。两者搞混则审计失效：
//     流水上看起来像用户自己给自己加了钱。
//   - amount 为 0 在入口拒掉，见下方注释。
func adminGrant(c *gin.Context) {
	var req struct {
		UserID uint    `json:"user_id"`
		Amount float64 `json:"amount"`
		Reason string  `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if req.UserID == 0 {
		resp.Error(c, http.StatusBadRequest, "user_id is required")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		resp.Error(c, http.StatusBadRequest, "reason is required")
		return
	}
	// 漏斗把 delta==0 当 no-op（既不改余额也不留流水），返回 200 会让调用方以为
	// 调整生效了。与其给出无痕的成功，不如在入口拒掉。
	if req.Amount == 0 {
		resp.Error(c, http.StatusBadRequest, "amount must not be zero")
		return
	}

	err := user.MutateQuota(nil, req.UserID, req.Amount, user.LedgerEntry{
		Kind:    model.LedgerKindAdminAdjust,
		ActorID: uint(c.GetInt("user_id")),
		Reason:  reason,
	}, c.Request.Context())
	switch {
	case errors.Is(err, user.ErrNonFiniteAmount):
		resp.Error(c, http.StatusBadRequest, "amount must be a finite number")
	case errors.Is(err, user.ErrUserNotFound):
		resp.Error(c, http.StatusNotFound, "user not found")
	case err != nil:
		resp.InternalError(c)
	default:
		resp.Success(c, nil)
	}
}
