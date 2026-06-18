package op

import (
	"context"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

func WSResponseAffinityCleanup(ctx context.Context, now time.Time) (int64, error) {
	if now.IsZero() {
		now = time.Now()
	}
	result := db.GetDB().WithContext(ctx).
		Where("expires_at <= ?", now).
		Delete(&model.WSResponseAffinity{})
	return result.RowsAffected, result.Error
}
