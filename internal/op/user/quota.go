package user

/*
Lodestar commercial layer — per-user prepaid quota (balance).

Ported in logic from new-api's prepaid-quota billing, adapted to octopus:
new-api uses integer quota units (QuotaPerUnit per $1); octopus already computes
per-request cost as float USD (StatsMetrics.Input/OutputCost), so we keep the
balance as float USD for a 1:1 match with the relay's cost accounting.

Only enforced when commercial_mode is on (see internal/op/billing).
*/

import (
	"context"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"

	"gorm.io/gorm"
)

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

// DeductQuota subtracts spent cost from balance and accumulates used_quota.
// Never drives quota below zero: at most MIN(remaining, amount) is taken (atomic
// single UPDATE). Concurrent requests may each pass HasBalanceForKey while
// balance is small; this caps total deduction without negative balances.
func DeductQuota(userID uint, amount float64, ctx context.Context) error {
	if amount <= 0 {
		return nil
	}
	res := db.GetDB().WithContext(ctx).Model(&model.User{}).
		Where("id = ?", userID).
		Updates(map[string]any{
			"quota":      gorm.Expr("quota - MIN(quota, ?)", amount),
			"used_quota": gorm.Expr("used_quota + MIN(quota, ?)", amount),
		})
	return res.Error
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
