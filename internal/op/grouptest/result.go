package grouptest

import (
	"context"
	"errors"
	"fmt"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/utils/snowflake"
	"gorm.io/gorm"
)

// GroupTestResultSave persists a completed (or failed) group test result.
func GroupTestResultSave(ctx context.Context, result *model.GroupTestResult) error {
	if result == nil {
		return fmt.Errorf("result is nil")
	}
	if db.GetDB() == nil {
		return fmt.Errorf("db is nil")
	}

	if result.ID == 0 {
		result.ID = snowflake.GenerateID()
	}
	return db.GetDB().WithContext(ctx).Create(result).Error
}

// GroupTestResultList returns the most recent group test results.
func GroupTestResultList(ctx context.Context, limit int) ([]model.GroupTestResult, error) {
	if db.GetDB() == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	var results []model.GroupTestResult
	if err := db.GetDB().WithContext(ctx).
		Order("finished_at DESC").
		Limit(limit).
		Find(&results).Error; err != nil {
		return nil, fmt.Errorf("failed to list group test results: %w", err)
	}

	if results == nil {
		results = make([]model.GroupTestResult, 0)
	}
	return results, nil
}

// GroupTestResultGet returns a single group test result by ID.
func GroupTestResultGet(ctx context.Context, id int64) (*model.GroupTestResult, error) {
	if db.GetDB() == nil {
		return nil, nil
	}

	var result model.GroupTestResult
	if err := db.GetDB().WithContext(ctx).First(&result, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get group test result: %w", err)
	}
	return &result, nil
}
