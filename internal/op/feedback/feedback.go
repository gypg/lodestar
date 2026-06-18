package feedback

// GGZERO — 意见反馈操作。

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

// Create stores a feedback entry.
func Create(userID uint, content, contact string, ctx context.Context) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("content is required")
	}
	if len(content) > 4000 {
		content = content[:4000]
	}
	fb := model.Feedback{
		UserID:    userID,
		Content:   content,
		Contact:   strings.TrimSpace(contact),
		Status:    "new",
		CreatedAt: time.Now().Unix(),
	}
	return db.GetDB().WithContext(ctx).Create(&fb).Error
}

// List returns recent feedback (newest first), capped.
func List(ctx context.Context) ([]model.Feedback, error) {
	var items []model.Feedback
	err := db.GetDB().WithContext(ctx).Order("id DESC").Limit(500).Find(&items).Error
	return items, err
}
