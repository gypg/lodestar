package invite

/*
GGZERO commercial layer — invitation code operations.

Same race-safe one-time-use pattern as top-up codes: Consume marks a code used in
a conditional update (RowsAffected check), so a code admits at most one registrant
even under concurrency.
*/

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

var ErrInvalidCode = errors.New("invalid or already-used invite code")

// IsValid reports whether the code exists and is unused (cheap pre-check before
// creating the user, so a taken username doesn't burn the invite).
func IsValid(code string, ctx context.Context) bool {
	var c model.InviteCode
	err := db.GetDB().WithContext(ctx).Where("code = ? AND used = ?", code, false).First(&c).Error
	return err == nil
}

func genCode() string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	return "inv-" + hex.EncodeToString(b)
}

// GenerateCodes creates `count` unused invite codes.
func GenerateCodes(count int, ctx context.Context) ([]model.InviteCode, error) {
	if count <= 0 || count > 1000 {
		return nil, errors.New("count must be 1..1000")
	}
	now := time.Now().Unix()
	codes := make([]model.InviteCode, 0, count)
	for i := 0; i < count; i++ {
		codes = append(codes, model.InviteCode{Code: genCode(), CreatedAt: now})
	}
	if err := db.GetDB().WithContext(ctx).Create(&codes).Error; err != nil {
		return nil, err
	}
	return codes, nil
}

// ListCodes returns recent invite codes (newest first), capped.
func ListCodes(ctx context.Context) ([]model.InviteCode, error) {
	var codes []model.InviteCode
	err := db.GetDB().WithContext(ctx).Order("id DESC").Limit(500).Find(&codes).Error
	return codes, err
}

// Consume marks an invite code used by userID, atomically and once.
func Consume(code string, userID uint, ctx context.Context) error {
	res := db.GetDB().WithContext(ctx).Model(&model.InviteCode{}).
		Where("code = ? AND used = ?", code, false).
		Updates(map[string]any{"used": true, "used_by": userID, "used_at": time.Now().Unix()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrInvalidCode
	}
	return nil
}
