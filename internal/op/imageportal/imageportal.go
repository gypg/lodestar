package imageportal

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/apikey"
)

const (
	maxPromptRunes = 2000
	// maxRecordsPerUser bounds storage growth: a self-hosted single-user
	// deployment rarely needs more, and each record may carry a large base64
	// payload. Older records are pruned on insert.
	maxRecordsPerUser = 200
)

// RecordSummary is a list item without the (potentially huge) URL payload.
type RecordSummary struct {
	ID        int    `json:"id"`
	Prompt    string `json:"prompt"`
	Model     string `json:"model"`
	Size      string `json:"size"`
	APIKeyID  int    `json:"api_key_id"`
	CreatedAt int64  `json:"created_at"`
}

// RecordDetail includes the URL so the client can render/download the image.
type RecordDetail struct {
	RecordSummary
	URL string `json:"url"`
}

// CreateInput is what the handler accepts from the client after a successful
// upstream generation.
type CreateInput struct {
	Model    string
	Prompt   string
	Size     string
	APIKeyID int
	URL      string
}

func ListForUser(uid uint, ctx context.Context) ([]RecordSummary, error) {
	var rows []model.ImageRecord
	err := db.GetDB().WithContext(ctx).
		Where("user_id = ?", uid).
		Order("created_at DESC").
		Limit(100).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]RecordSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, RecordSummary{
			ID: r.ID, Prompt: r.Prompt, Model: r.Model, Size: r.Size,
			APIKeyID: r.APIKeyID, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

func GetForUser(uid uint, id int, ctx context.Context) (*RecordDetail, error) {
	var row model.ImageRecord
	err := db.GetDB().WithContext(ctx).
		Where("id = ? AND user_id = ?", id, uid).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	return &RecordDetail{
		RecordSummary: RecordSummary{
			ID: row.ID, Prompt: row.Prompt, Model: row.Model, Size: row.Size,
			APIKeyID: row.APIKeyID, CreatedAt: row.CreatedAt,
		},
		URL: row.URL,
	}, nil
}

// Create persists one generated image. It validates that the caller owns the
// API key used (preventing cross-user key association), bounds the prompt, and
// prunes the oldest records beyond maxRecordsPerUser to keep storage bounded.
func Create(uid uint, in CreateInput, ctx context.Context) (*RecordDetail, error) {
	in.URL = strings.TrimSpace(in.URL)
	if in.URL == "" {
		return nil, errors.New("image url is required")
	}
	if err := assertKeyOwned(uid, in.APIKeyID, ctx); err != nil {
		return nil, err
	}
	if r := []rune(in.Prompt); len(r) > maxPromptRunes {
		in.Prompt = string(r[:maxPromptRunes])
	}
	now := time.Now().Unix()
	row := model.ImageRecord{
		UserID: uid, Prompt: in.Prompt, Model: strings.TrimSpace(in.Model),
		Size: strings.TrimSpace(in.Size), APIKeyID: in.APIKeyID,
		URL: in.URL, CreatedAt: now,
	}
	if err := db.GetDB().WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	if err := pruneExcess(uid, ctx); err != nil {
		// prune failure must not fail the create; the record is already saved.
		_ = err
	}
	return &RecordDetail{
		RecordSummary: RecordSummary{
			ID: row.ID, Prompt: row.Prompt, Model: row.Model, Size: row.Size,
			APIKeyID: row.APIKeyID, CreatedAt: row.CreatedAt,
		},
		URL: row.URL,
	}, nil
}

func Delete(uid uint, id int, ctx context.Context) error {
	res := db.GetDB().WithContext(ctx).
		Where("id = ? AND user_id = ?", id, uid).
		Delete(&model.ImageRecord{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("image record not found")
	}
	return nil
}

// pruneExcess deletes the user's oldest records that exceed maxRecordsPerUser.
// Called after each insert so the per-user history stays bounded.
//
// We pluck the IDs to delete first and then delete by id, rather than relying on
// DELETE ... LIMIT: SQLite (used in tests/local) silently ignores LIMIT on
// DELETE and would wipe the user's entire history on the first overflow.
func pruneExcess(uid uint, ctx context.Context) error {
	var count int64
	if err := db.GetDB().WithContext(ctx).Model(&model.ImageRecord{}).
		Where("user_id = ?", uid).Count(&count).Error; err != nil {
		return err
	}
	if count <= maxRecordsPerUser {
		return nil
	}
	excess := int(count) - maxRecordsPerUser
	// id is monotonic with created_at, so the oldest `excess` rows by id are
	// the ones to drop.
	var ids []int
	if err := db.GetDB().WithContext(ctx).Model(&model.ImageRecord{}).
		Where("user_id = ?", uid).
		Order("id ASC").
		Limit(excess).
		Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	return db.GetDB().WithContext(ctx).
		Where("id IN ?", ids).
		Delete(&model.ImageRecord{}).Error
}

func assertKeyOwned(uid uint, apiKeyID int, ctx context.Context) error {
	if apiKeyID <= 0 {
		return errors.New("api_key_id required")
	}
	keys, err := apikey.ListByUser(uid, ctx)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if k.ID == apiKeyID {
			return nil
		}
	}
	return errors.New("api key not found")
}
