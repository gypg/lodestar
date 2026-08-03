package topup

/*
Lodestar commercial layer — top-up code operations.

Logic ported from new-api's redemption flow, adapted to Lodestar float-USD balance.
Redeem is transactional and race-safe (conditional update + RowsAffected check),
so a code can be redeemed at most once even under concurrency.
*/

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"

	"gorm.io/gorm"
)

var ErrInvalidCode = errors.New("invalid or already-used code")

func genCode() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "ls-" + hex.EncodeToString(b)
}

// GenerateCodes creates `count` unused codes each worth `quota` USD.
func GenerateCodes(count int, quota float64, ctx context.Context) ([]model.TopupCode, error) {
	if count <= 0 || count > 1000 {
		return nil, errors.New("count must be 1..1000")
	}
	if quota <= 0 {
		return nil, errors.New("quota must be positive")
	}
	now := time.Now().Unix()
	codes := make([]model.TopupCode, 0, count)
	for i := 0; i < count; i++ {
		codes = append(codes, model.TopupCode{Code: genCode(), Quota: quota, CreatedAt: now})
	}
	if err := db.GetDB().WithContext(ctx).Create(&codes).Error; err != nil {
		return nil, err
	}
	return codes, nil
}

// ListCodes returns recent codes (newest first), capped.
func ListCodes(ctx context.Context) ([]model.TopupCode, error) {
	var codes []model.TopupCode
	err := db.GetDB().WithContext(ctx).Order("id DESC").Limit(500).Find(&codes).Error
	return codes, err
}

// Redeem credits the user's balance with the code's quota, atomically and once.
func Redeem(code string, userID uint, ctx context.Context) (float64, error) {
	var credited float64
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tc model.TopupCode
		if err := tx.Where("code = ? AND used = ?", code, false).First(&tc).Error; err != nil {
			return ErrInvalidCode
		}
		res := tx.Model(&model.TopupCode{}).
			Where("id = ? AND used = ?", tc.ID, false).
			Updates(map[string]any{"used": true, "used_by": userID, "used_at": time.Now().Unix()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrInvalidCode
		}
		if err := tx.Model(&model.User{}).
			Where("id = ?", userID).
			Update("quota", gorm.Expr("quota + ?", tc.Quota)).Error; err != nil {
			return err
		}
		credited = tc.Quota
		return nil
	})
	return credited, err
}
