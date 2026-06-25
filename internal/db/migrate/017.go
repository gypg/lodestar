package migrate

import (
	"fmt"

	"github.com/gypg/lodestar/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 17,
		Up:      migrateLandingAmbientDefault,
		Down:    stubDown(17),
	})
}

func migrateLandingAmbientDefault(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.Setting{}) {
		return nil
	}
	var n int64
	if err := db.Model(&model.Setting{}).
		Where("key = ?", string(model.SettingKeyLandingAmbientMode)).
		Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return db.Create(&model.Setting{
		Key:   model.SettingKeyLandingAmbientMode,
		Value: "photo",
	}).Error
}