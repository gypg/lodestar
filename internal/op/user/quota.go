package user

/*
Lodestar commercial layer — per-user prepaid quota (balance).

Ported in logic from new-api's prepaid-quota billing, adapted to Lodestar:
new-api uses integer quota units (QuotaPerUnit per $1); Lodestar already computes
per-request cost as float USD (StatsMetrics.Input/OutputCost), so we keep the
balance as float USD for a 1:1 match with the relay's cost accounting.

Only enforced when commercial_mode is on (see internal/op/billing).
*/

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"

	"gorm.io/gorm"
)

// ErrNonFiniteAmount is returned when a settlement amount is NaN or ±Inf.
var ErrNonFiniteAmount = errors.New("non-finite amount")

// ErrUserNotFound is returned when a settlement targets a user row that is gone.
var ErrUserNotFound = errors.New("user not found")

// GetQuota returns (remaining, used) balance for a user.
func GetQuota(userID uint, ctx context.Context) (remaining float64, used float64, err error) {
	var u model.User
	if err = db.GetDB().WithContext(ctx).Select("quota", "used_quota").First(&u, userID).Error; err != nil {
		return 0, 0, err
	}
	return u.Quota, u.UsedQuota, nil
}

// AddQuota credits a user's balance (top-up / admin grant / redemption).
func AddQuota(userID uint, amount float64, ctx context.Context) error {
	if amount == 0 {
		return nil
	}
	return db.GetDB().WithContext(ctx).Model(&model.User{}).
		Where("id = ?", userID).
		Update("quota", gorm.Expr("quota + ?", amount)).Error
}

// SettleUsage records the cost of usage that has ALREADY been delivered, and
// accumulates used_quota.
//
// Settlement is deliberately NOT all-or-nothing, unlike a purchase. The upstream
// call already happened and we already paid for it, so a charge larger than the
// remaining balance must be recorded as debt — the balance is allowed to go
// negative. The request gate (billing.HasBalanceForKey, remaining > 0) then
// blocks the next request until a top-up nets the debt off.
//
// This replaced an atomic `WHERE quota >= amount` guard that discarded the charge
// whenever it exceeded the balance, leaving quota and used_quota untouched. The
// gate therefore kept passing and the account served unlimited free requests once
// its balance fell below one request's cost — the terminal state of every prepaid
// account, not an edge case. Purchases keep their own affordability guard (see
// op/subscription.PurchaseWithBalance): you may not buy what you cannot afford,
// but you always owe for what you already consumed.
//
// amount must be finite. NaN and ±Inf are rejected rather than clamped, because
// `quota - NaN` poisons the column permanently: every later `remaining > 0` is
// false, so the account is locked out, and no top-up can arithmetically repair it.
// The removed WHERE guard rejected non-finite amounts as a side effect (every SQL
// comparison against NaN is false), so this check has to be explicit now. The
// upstream-cost path has no other guard — only the billing-expression path does
// (internal/pkg/billingexpr/run.go).
func SettleUsage(userID uint, amount float64, ctx context.Context) error {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return fmt.Errorf("%w: %v", ErrNonFiniteAmount, amount)
	}
	if amount <= 0 {
		return nil
	}
	res := db.GetDB().WithContext(ctx).Model(&model.User{}).
		Where("id = ?", userID).
		Updates(map[string]any{
			"quota":      gorm.Expr("quota - ?", amount),
			"used_quota": gorm.Expr("used_quota + ?", amount),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SetQuota overwrites a user's balance (admin adjust).
func SetQuota(userID uint, amount float64, ctx context.Context) error {
	return db.GetDB().WithContext(ctx).Model(&model.User{}).
		Where("id = ?", userID).
		Update("quota", amount).Error
}

// UpdateEmail sets a user's email (e.g. captured at verified registration).
func UpdateEmail(userID uint, email string, ctx context.Context) error {
	return db.GetDB().WithContext(ctx).Model(&model.User{}).
		Where("id = ?", userID).
		Update("email", email).Error
}
