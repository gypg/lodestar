package handlers

/*
Lodestar commercial layer -- Stripe payment handlers.

- POST /api/v1/wallet/stripe/topup  (authenticated) -- create a Stripe Checkout Session
- POST /api/v1/webhook/stripe       (public, no auth) -- Stripe webhook callback

The webhook endpoint is intentionally PUBLIC (no auth middleware) because
Stripe posts server-to-server callbacks with signature verification instead
of user authentication.
*/

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/op/payment"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
)

func init() {
	// Authenticated Stripe topup endpoint.
	router.NewGroupRouter("/api/v1/wallet/stripe").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/topup", http.MethodPost).
				Handle(stripeTopup),
		)

	// Public webhook endpoint -- NO auth middleware (Stripe posts S2S).
	router.NewGroupRouter("/api/v1/webhook").
		AddRoute(
			router.NewRoute("/stripe", http.MethodPost).
				Handle(stripeWebhook),
		)
}

func stripeTopup(c *gin.Context) {
	var req struct {
		Amount float64 `json:"amount"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if req.Amount <= 0 {
		resp.Error(c, http.StatusBadRequest, "amount must be positive")
		return
	}

	uid := uint(c.GetInt("user_id"))
	payLink, err := payment.CreateCheckoutSession(uid, req.Amount, c.Request.Context())
	if err != nil {
		if errors.Is(err, payment.ErrStripeNotConfigured) {
			resp.Error(c, http.StatusBadRequest, "admin has not configured Stripe")
			return
		}
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, gin.H{"pay_link": payLink})
}

// maxStripeWebhookBytes 限制 webhook 体积。这条路由刻意无鉴权（Stripe S2S 回调，
// 靠签名而非登录态），故是**匿名可达**的无限 io.ReadAll —— 比站点导入那条更易触达。
// Stripe 事件体实际只有几十 KB，1 MiB 已是充裕上限。
// 注意签名校验发生在读完 body 之后（HandleWebhook 需要原始字节算 HMAC），
// 所以闸门必须落在读取处，不能指望"签名不对就不会读"。
const maxStripeWebhookBytes int64 = 1 << 20

func stripeWebhook(c *gin.Context) {
	if c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxStripeWebhookBytes)
	}
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.Status(http.StatusServiceUnavailable)
		return
	}

	sig := c.GetHeader("Stripe-Signature")
	result := payment.HandleWebhook(payload, sig, c.Request.Context())

	switch result {
	case payment.StripeWebhookOK:
		c.Status(http.StatusOK)
	case payment.StripeWebhookDisabled:
		c.Status(http.StatusForbidden)
	case payment.StripeWebhookBadSignature:
		c.Status(http.StatusBadRequest)
	case payment.StripeWebhookReadError:
		c.Status(http.StatusServiceUnavailable)
	default:
		c.Status(http.StatusOK)
	}
}
