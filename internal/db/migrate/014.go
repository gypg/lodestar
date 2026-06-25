package migrate

import (
	"fmt"

	"github.com/gypg/lodestar/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 14,
		Up:      migrateAPIKeyExcludedChannels,
		Down:    stubDown(14),
	})
}

// 014: 为 api_keys 增加 excluded_channels 列（issue #55：API Key 渠道黑名单）。
// gorm AutoMigrate 通常也会加列，这里幂等兜底，确保跨方言（SQLite/MySQL/Postgres）
// 以及运行时切换 DB 类型后该列存在。
func migrateAPIKeyExcludedChannels(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.APIKey{}) {
		return nil
	}
	if db.Migrator().HasColumn(&model.APIKey{}, "ExcludedChannels") {
		return nil
	}
	if err := db.Migrator().AddColumn(&model.APIKey{}, "ExcludedChannels"); err != nil {
		return fmt.Errorf("add column excluded_channels: %w", err)
	}
	return nil
}
