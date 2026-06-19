package payment

/*
Lodestar commercial layer — online top-up via Stripe.

Ported from GGGZERO's Stripe flow (controller/topup_stripe.go), adapted to
Lodestar's internal/op + internal/server/handlers pattern. Uses the Stripe
Checkout Session API to create a hosted payment page. The webhook handler
verifies the Stripe-Signature header, processes checkout.session.completed,
checkout.session.expired, and async payment events, then credits the user's
USD balance transactionally (idempotent via conditional status update).

Stripe credentials (API key, webhook secret, currency) are admin-configured
settings -- building this needs no credentials; the admin fills them in the
panel to go live.
*/

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"

	"github.com/stripe/stripe-go/v78"
	"github.com/stripe/stripe-go/v78/checkout/session"
	"github.com/stripe/stripe-go/v78/webhook"
	"gorm.io/gorm"
)

var (
	ErrStripeNotConfigured = errors.New("stripe not configured")
)

func stripeClient() (string, string, error) {
	enabled, _ := setting.GetBool(model.SettingKeyStripeEnabled)
	if !enabled {
		return "", "", ErrStripeNotConfigured
	}
	apiKey, _ := setting.GetString(model.SettingKeyStripeAPIKey)
	webhookSecret, _ := setting.GetString(model.SettingKeyStripeWebhookSecret)
	if apiKey == "" || webhookSecret == "" {
		return "", "", ErrStripeNotConfigured
	}
	return apiKey, webhookSecret, nil
}

func stripeCurrency() string {
	c, _ := setting.GetString(model.SettingKeyStripeCurrency)
	if c == "" {
		return "usd"
	}
	return strings.ToLower(c)
}

func stripeMinTopUp() float64 {
	s, _ := setting.GetString(model.SettingKeyStripeMinTopUp)
	var v float64
	_, _ = fmt.Sscanf(s, "%f", &v)
	if v < 1 {
		return 1
	}
	return v
}

func genStripeTradeNo(userID uint) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("stripe-ref-%d-%s-%d", userID, hex.EncodeToString(b), time.Now().UnixMilli())
}

// StripeConfigured reports whether Stripe is ready (for the frontend).
func StripeConfigured() bool {
	_, _, err := stripeClient()
	return err == nil
}

// CreateCheckoutSession creates a Stripe Checkout Session for wallet top-up
// and records a pending PaymentOrder. Returns the hosted checkout URL.
func CreateCheckoutSession(userID uint, amountUSD float64, successURL string, cancelURL string, ctx context.Context) (string, error) {
	if amountUSD <= 0 {
		return "", ErrInvalidAmount
	}
	minTopUp := stripeMinTopUp()
	if amountUSD < minTopUp {
		return "", fmt.Errorf("amount must be at least %.0f", minTopUp)
	}

	apiKey, _, err := stripeClient()
	if err != nil {
		return "", err
	}

	tradeNo := genStripeTradeNo(userID)

	stripe.Key = apiKey

	base := strings.TrimRight(strings.TrimSpace(mustSettingStr(model.SettingKeyPaymentCallbackBase)), "/")
	if successURL == "" {
		successURL = base + "/wallet"
	}
	if cancelURL == "" {
		cancelURL = base + "/wallet"
	}

	// Stripe expects amount in smallest currency unit (cents for USD).
	amountCents := int64(amountUSD * 100)

	params := &stripe.CheckoutSessionParams{
		ClientReferenceID: stripe.String(tradeNo),
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String(stripeCurrency()),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name: stripe.String(fmt.Sprintf("Lodestar credit $%.2f", amountUSD)),
					},
					UnitAmount: stripe.Int64(amountCents),
				},
				Quantity: stripe.Int64(1),
			},
		},
		Mode: stripe.String(string(stripe.CheckoutSessionModePayment)),
	}

	result, err := session.New(params)
	if err != nil {
		return "", fmt.Errorf("create checkout session: %w", err)
	}

	order := model.PaymentOrder{
		UserID:    userID,
		AmountUSD: amountUSD,
		Money:     amountUSD,
		TradeNo:   tradeNo,
		Method:    "stripe",
		Provider:  "stripe",
		Status:    "pending",
		CreateTime: time.Now().Unix(),
	}
	if err := db.GetDB().WithContext(ctx).Create(&order).Error; err != nil {
		return "", fmt.Errorf("create order: %w", err)
	}

	return result.URL, nil
}

// StripeWebhookResult indicates the outcome of processing a webhook event.
type StripeWebhookResult int

const (
	StripeWebhookOK        StripeWebhookResult = iota // event processed or ignored
	StripeWebhookDisabled                             // webhook disabled by admin
	StripeWebhookBadSignature                         // signature verification failed
	StripeWebhookReadError                            // failed to read request body
)

// HandleWebhook reads the raw request body, verifies the Stripe-Signature,
// and dispatches the event. The caller should write 200 on StripeWebhookOK,
// 403 on StripeWebhookDisabled, 400 on StripeWebhookBadSignature, and
// 503 on StripeWebhookReadError.
func HandleWebhook(payload []byte, sigHeader string, ctx context.Context) StripeWebhookResult {
	_, webhookSecret, err := stripeClient()
	if err != nil {
		return StripeWebhookDisabled
	}

	event, err := webhook.ConstructEvent(payload, sigHeader, webhookSecret)
	if err != nil {
		return StripeWebhookBadSignature
	}

	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted:
		handleSessionCompleted(ctx, event)
	case stripe.EventTypeCheckoutSessionExpired:
		handleSessionExpired(ctx, event)
	case stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded:
		handleAsyncPaymentSucceeded(ctx, event)
	case stripe.EventTypeCheckoutSessionAsyncPaymentFailed:
		handleAsyncPaymentFailed(ctx, event)
	default:
		// unhandled event type -- ack and ignore
	}

	return StripeWebhookOK
}

// handleSessionCompleted handles the checkout.session.completed event.
// If payment_status is "paid", credits the user's balance.
func handleSessionCompleted(ctx context.Context, event stripe.Event) {
	referenceID := event.GetObjectValue("client_reference_id")
	status := event.GetObjectValue("status")
	if status != "complete" {
		return
	}
	paymentStatus := event.GetObjectValue("payment_status")
	if paymentStatus != "paid" {
		return // async payment -- wait for async event
	}
	customerID := event.GetObjectValue("customer")
	fulfillOrder(ctx, referenceID, customerID)
}

// handleAsyncPaymentSucceeded handles delayed payment methods (bank transfer, SEPA, etc.)
func handleAsyncPaymentSucceeded(ctx context.Context, event stripe.Event) {
	referenceID := event.GetObjectValue("client_reference_id")
	customerID := event.GetObjectValue("customer")
	fulfillOrder(ctx, referenceID, customerID)
}

// handleAsyncPaymentFailed marks the order as failed.
func handleAsyncPaymentFailed(ctx context.Context, event stripe.Event) {
	referenceID := event.GetObjectValue("client_reference_id")
	if referenceID == "" {
		return
	}

	_ = db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order model.PaymentOrder
		if err := tx.Where("trade_no = ? AND provider = ?", referenceID, "stripe").First(&order).Error; err != nil {
			return nil // unknown order -- ack to stop retries
		}
		if order.Status != "pending" {
			return nil // already processed
		}
		return tx.Model(&model.PaymentOrder{}).
			Where("id = ? AND status = ?", order.ID, "pending").
			Updates(map[string]any{"status": "failed", "complete_time": time.Now().Unix()}).Error
	})
}

// handleSessionExpired marks pending orders as expired.
func handleSessionExpired(ctx context.Context, event stripe.Event) {
	referenceID := event.GetObjectValue("client_reference_id")
	status := event.GetObjectValue("status")
	if status != "expired" || referenceID == "" {
		return
	}

	_ = db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order model.PaymentOrder
		if err := tx.Where("trade_no = ? AND provider = ?", referenceID, "stripe").First(&order).Error; err != nil {
			return nil
		}
		if order.Status != "pending" {
			return nil
		}
		return tx.Model(&model.PaymentOrder{}).
			Where("id = ? AND status = ?", order.ID, "pending").
			Updates(map[string]any{"status": "expired", "complete_time": time.Now().Unix()}).Error
	})
}

// fulfillOrder is the shared logic for crediting quota after payment is confirmed.
func fulfillOrder(ctx context.Context, referenceID string, customerID string) {
	if referenceID == "" {
		return
	}

	_ = db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order model.PaymentOrder
		if err := tx.Where("trade_no = ? AND provider = ?", referenceID, "stripe").First(&order).Error; err != nil {
			return nil // unknown order
		}
		res := tx.Model(&model.PaymentOrder{}).
			Where("id = ? AND status = ?", order.ID, "pending").
			Updates(map[string]any{"status": "success", "complete_time": time.Now().Unix()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // already processed (idempotent)
		}
		return tx.Model(&model.User{}).
			Where("id = ?", order.UserID).
			Update("quota", gorm.Expr("quota + ?", order.AmountUSD)).Error
	})
}

func mustSettingStr(k model.SettingKey) string {
	v, _ := setting.GetString(k)
	return v
}

// ReadBody is a helper that reads the entire request body for webhook processing.
func ReadBody(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}
