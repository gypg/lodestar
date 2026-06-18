package migrate

import (
	"fmt"

	"github.com/gypg/lodestar/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 16,
		Up:      migrateSiteNameRebrand,
	})
}

// 016: 品牌改名 ggzero -> Lodestar 后，把数据库里仍是旧默认值 "GGZERO" 的
// site_name 升级为 "Lodestar"。幂等且保守：只改恰好等于旧默认值的记录，
// 用户自定义的站点名（任何其他值）一律不动。
func migrateSiteNameRebrand(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.Setting{}) {
		return nil
	}
	res := db.Model(&model.Setting{}).
		Where("key = ? AND value = ?", string(model.SettingKeySiteName), "GGZERO").
		Update("value", "Lodestar")
	if res.Error != nil {
		return fmt.Errorf("update site_name rebrand: %w", res.Error)
	}
	return nil
}
