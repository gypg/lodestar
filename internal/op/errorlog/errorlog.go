package errorlog

import (
	"context"
	"errors"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/utils/log"
	"github.com/gypg/lodestar/internal/utils/snowflake"
	"gorm.io/gorm"
)

// maxKeepCount 错误日志保留条数上限。错误日志量远小于转发日志，
// 超过后周期任务清理最旧一半，防止无界增长。
const maxKeepCount = 5000

// Add 写入一条错误日志。分配 snowflake ID 并落主库（错误日志随主库备份，
// 与转发日志分离——转发日志可能在独立日志库且被视为可丢弃数据）。
// 返回错误仅用于调用方记录；写入失败不应影响主流程。
func Add(ctx context.Context, entry model.ErrorLog) error {
	entry.ID = snowflake.GenerateID()
	if entry.Source == "" {
		entry.Source = "backend"
	}
	if entry.Time == 0 {
		entry.Time = time.Now().Unix()
	}
	if err := db.GetDB().WithContext(ctx).Create(&entry).Error; err != nil {
		log.Warnf("failed to save error log: %v", err)
		return err
	}
	return nil
}

// Filter 错误日志查询过滤条件。
type Filter struct {
	Source    string // backend | frontend；空 = 全部
	Level     string // panic | error | ...；空 = 全部
	StartTime *int64
	EndTime   *int64
}

// List 分页查询错误日志，按时间倒序返回完整条目（message/stack 直接返回，
// 错误日志量小且排障需要完整堆栈，不做大字段剥离）。
func List(ctx context.Context, filter Filter, page, pageSize int) ([]model.ErrorLog, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	query := db.GetDB().WithContext(ctx).Model(&model.ErrorLog{})
	if filter.Source != "" {
		query = query.Where("source = ?", filter.Source)
	}
	if filter.Level != "" {
		query = query.Where("level = ?", filter.Level)
	}
	if filter.StartTime != nil {
		query = query.Where("time >= ?", *filter.StartTime)
	}
	if filter.EndTime != nil {
		query = query.Where("time <= ?", *filter.EndTime)
	}
	var entries []model.ErrorLog
	if err := query.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}

// GetByID 按 ID 获取一条错误日志。记录不存在时返回 (nil, nil)。
func GetByID(ctx context.Context, id int64) (*model.ErrorLog, error) {
	var entry model.ErrorLog
	err := db.GetDB().WithContext(ctx).Where("id = ?", id).First(&entry).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// Clear 清空全部错误日志。
func Clear(ctx context.Context) error {
	return db.GetDB().WithContext(ctx).Where("1 = 1").Delete(&model.ErrorLog{}).Error
}

// Cleanup 周期清理：超过 maxKeepCount 时删除最旧的一半（与 relay_logs 的
// 清理策略一致，避免大表逐行删除）。
func Cleanup(ctx context.Context) error {
	var total int64
	if err := db.GetDB().WithContext(ctx).Model(&model.ErrorLog{}).Count(&total).Error; err != nil {
		return err
	}
	if total <= maxKeepCount {
		return nil
	}
	deleteCount := total / 2
	var thresholdID int64
	if err := db.GetDB().WithContext(ctx).Model(&model.ErrorLog{}).
		Order("id ASC").
		Offset(int(deleteCount)).
		Limit(1).
		Pluck("id", &thresholdID).Error; err != nil {
		return err
	}
	if thresholdID == 0 {
		return nil
	}
	return db.GetDB().WithContext(ctx).Where("id < ?", thresholdID).Delete(&model.ErrorLog{}).Error
}
