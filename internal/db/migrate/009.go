package migrate

import (
	"fmt"

	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 9,
		Up: func(db *gorm.DB) error {
			// Guard on table existence like the sibling migrations (003/005/006
			// ...). Without this, on a fresh DB where AutoMigrate hasn't created
			// `groups` yet, HasColumn returns false and the ALTER TABLE below
			// errors with "relation does not exist" on PostgreSQL.
			if !db.Migrator().HasTable("groups") {
				return nil
			}
			if db.Migrator().HasColumn("groups", "endpoint_provider") {
				return nil
			}

			if err := db.Exec("ALTER TABLE groups ADD COLUMN endpoint_provider TEXT NOT NULL DEFAULT ''").Error; err != nil {
				return fmt.Errorf("failed to add groups.endpoint_provider: %w", err)
			}
			return nil
		},
		Down: stubDown(9),
	})
}
