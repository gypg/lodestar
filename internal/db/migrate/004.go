package migrate

import (
	"fmt"

	"github.com/gypg/lodestar/internal/model"
	"gorm.io/gorm"
)

// columnAddition describes a column to ensure exists on api_keys.
type columnAddition struct {
	modelField string // Go struct field name on model.APIKey
}

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 4,
		Up:      addAPIKeyQuotaColumns,
		Down:    stubDown(4),
	})
}

func addAPIKeyQuotaColumns(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable("api_keys") {
		return nil
	}

	columns := []columnAddition{
		{"RateLimitRPM"},
		{"RateLimitTPM"},
		{"PerModelQuotaJSON"},
	}

	for _, col := range columns {
		if db.Migrator().HasColumn(&model.APIKey{}, col.modelField) {
			continue
		}
		if err := db.Migrator().AddColumn(&model.APIKey{}, col.modelField); err != nil {
			return fmt.Errorf("failed to add column api_keys.%s: %w", col.modelField, err)
		}
	}
	return nil
}
