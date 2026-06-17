package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 13,
		Up:      ensureRelayLogsTimeIndex,
	})
}

// 013: ensure relay_logs.time has an index for time-range queries.
// Nearly all analytics, ops, and log queries filter on this column.
// Without an index the database performs full table scans, causing
// extreme IO (e.g. 24 GB reads on an 842 MB database).
func ensureRelayLogsTimeIndex(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable("relay_logs") {
		return nil
	}
	if db.Migrator().HasIndex(&model.RelayLog{}, "Time") {
		return nil
	}
	if err := db.Migrator().CreateIndex(&model.RelayLog{}, "Time"); err != nil {
		return fmt.Errorf("failed to create relay_logs.time index: %w", err)
	}
	return nil
}
