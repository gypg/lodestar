package migrate

import (
	"fmt"

	"github.com/gypg/lodestar/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 15,
		Up:      migrateRelayLogIsTest,
	})
}

// 015: 为 relay_logs 增加 is_test 列（issue #82：测试模型日志可显示）。
// gorm AutoMigrate 通常也会加列，这里幂等兜底，确保跨方言（SQLite/MySQL/Postgres）
// 以及运行时切换 DB 类型后该列存在。
func migrateRelayLogIsTest(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.RelayLog{}) {
		return nil
	}
	if db.Migrator().HasColumn(&model.RelayLog{}, "IsTest") {
		return nil
	}
	if err := db.Migrator().AddColumn(&model.RelayLog{}, "IsTest"); err != nil {
		return fmt.Errorf("add column is_test: %w", err)
	}
	return nil
}
