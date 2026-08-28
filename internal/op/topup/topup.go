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
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/user"

	"gorm.io/gorm"
)

var ErrInvalidCode = errors.New("invalid or already-used code")

func genCode() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "ls-" + hex.EncodeToString(b)
}

// MaxNoteLen bounds the reconciliation note. Matches the column width so a note
// is rejected outright rather than silently truncated by the driver — a clipped
// audit trail is worse than a refused one, because it still looks complete.
const MaxNoteLen = 256

// validateGenerateCodes holds the input rules, separated from persistence so they
// can be tested without a database. Returns the note as it should be stored.
func validateGenerateCodes(count int, quota float64, note string) (string, error) {
	if count <= 0 || count > 1000 {
		return "", errors.New("count must be 1..1000")
	}
	if quota <= 0 {
		return "", errors.New("quota must be positive")
	}
	note = strings.TrimSpace(note)
	// Counted in runes, not bytes: the operator writes these in Chinese, where a
	// byte-based limit would cut the allowance to a third of what it looks like.
	if utf8.RuneCountInString(note) > MaxNoteLen {
		return "", fmt.Errorf("note must be at most %d characters", MaxNoteLen)
	}
	return note, nil
}

// GenerateCodes creates `count` unused codes each worth `quota` USD, all sharing
// the same reconciliation note (they come from one offline payment).
func GenerateCodes(count int, quota float64, note string, ctx context.Context) ([]model.TopupCode, error) {
	note, err := validateGenerateCodes(count, quota, note)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	codes := make([]model.TopupCode, 0, count)
	for i := 0; i < count; i++ {
		codes = append(codes, model.TopupCode{Code: genCode(), Quota: quota, Note: note, CreatedAt: now})
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
		// 入账走漏斗（WO-017）：余额与流水、兑换码置为已用在同一事务里。
		if err := user.MutateQuota(tx, userID, tc.Quota, user.LedgerEntry{
			Kind:    model.LedgerKindRedeem,
			RefType: model.LedgerRefTopupCode,
			RefID:   strconv.Itoa(tc.ID),
		}, ctx); err != nil {
			return err
		}
		credited = tc.Quota
		return nil
	})
	return credited, err
}
