package payment

/*
Lodestar commercial layer — online top-up via 易支付 (Epay).

Ported from new-api's Epay flow (controller/topup.go) using the same go-epay
library, adapted to Lodestar float-USD balance: a user pays `money` (gateway
currency) = amountUSD * topup_rate, and on a verified TRADE_SUCCESS callback the
amountUSD is credited to their balance. Order completion is transactional and
idempotent (status pending->success conditional update), so duplicate callbacks
never double-credit.

Merchant credentials (PID/key/gateway) are admin-configured settings — building
this needs no credentials; the admin fills them in the panel to go live.
*/

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/utils/log"

	"gorm.io/gorm"
)

var (
	ErrNotConfigured = errors.New("payment not configured")
	ErrInvalidAmount = errors.New("invalid amount")
	ErrInvalidMethod = errors.New("invalid payment method")
)

func client() *epay.Client {
	if enabled, _ := setting.GetBool(model.SettingKeyEpayEnabled); !enabled {
		return nil
	}
	addr, _ := setting.GetString(model.SettingKeyPayAddress)
	pid, _ := setting.GetString(model.SettingKeyEpayPID)
	key, _ := setting.GetString(model.SettingKeyEpayKey)
	if addr == "" || pid == "" || key == "" {
		return nil
	}
	cli, err := epay.NewClient(&epay.Config{PartnerID: pid, Key: key}, addr)
	if err != nil {
		return nil
	}
	return cli
}

func rate() float64 {
	s, _ := setting.GetString(model.SettingKeyTopupRate)
	r, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || r <= 0 {
		return 1
	}
	return r
}

func genTradeNo(userID uint) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("USR%dNO%s%d", userID, hex.EncodeToString(b), time.Now().Unix())
}

// EpayConfigured reports whether online payment is ready (for the frontend).
func EpayConfigured() bool { return client() != nil }

// CreateEpayOrder builds a signed Epay payment request and records a pending order.
// Returns the gateway URL + signed params for the frontend to submit.
func CreateEpayOrder(userID uint, amountUSD float64, method string, ctx context.Context) (string, map[string]string, error) {
	if amountUSD <= 0 {
		return "", nil, ErrInvalidAmount
	}
	if method != "alipay" && method != "wxpay" {
		return "", nil, ErrInvalidMethod
	}
	cli := client()
	if cli == nil {
		return "", nil, ErrNotConfigured
	}
	base := strings.TrimRight(strings.TrimSpace(mustSetting(model.SettingKeyPaymentCallbackBase)), "/")
	notifyURL, _ := url.Parse(base + "/api/v1/wallet/epay/notify")
	returnURL, _ := url.Parse(base + "/")
	tradeNo := genTradeNo(userID)
	money := amountUSD * rate()

	uri, params, err := cli.Purchase(&epay.PurchaseArgs{
		Type:           method,
		ServiceTradeNo: tradeNo,
		Name:           fmt.Sprintf("Lodestar credit $%.2f", amountUSD),
		Money:          strconv.FormatFloat(money, 'f', 2, 64),
		Device:         epay.PC,
		NotifyUrl:      notifyURL,
		ReturnUrl:      returnURL,
	})
	if err != nil {
		return "", nil, err
	}
	order := model.PaymentOrder{
		UserID:     userID,
		AmountUSD:  amountUSD,
		Money:      money,
		TradeNo:    tradeNo,
		Method:     method,
		Provider:   "epay",
		Status:     "pending",
		CreateTime: time.Now().Unix(),
	}
	if err := db.GetDB().WithContext(ctx).Create(&order).Error; err != nil {
		return "", nil, err
	}
	return uri, params, nil
}

// HandleEpayNotify verifies a callback and, on success, credits the order's user
// exactly once. Returns whether to ack the gateway ("success") or not ("fail").
func HandleEpayNotify(params map[string]string, ctx context.Context) bool {
	cli := client()
	if cli == nil {
		return false
	}
	vi, err := cli.Verify(params)
	if err != nil || !vi.VerifyStatus {
		return false
	}
	if vi.TradeStatus != epay.StatusTradeSuccess {
		return true // verified signature but not a success event — ack and ignore
	}
	if err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order model.PaymentOrder
		if err := tx.Where("trade_no = ? AND provider = ?", vi.ServiceTradeNo, "epay").First(&order).Error; err != nil {
			return nil // unknown order — ack to stop retries
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
	}); err != nil {
		log.Errorf("epay callback transaction failed for trade %s: %v", vi.ServiceTradeNo, err)
		return false // tell gateway to retry
	}
	return true
}

func mustSetting(k model.SettingKey) string {
	v, _ := setting.GetString(k)
	return v
}
