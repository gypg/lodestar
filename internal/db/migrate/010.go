package migrate

import (
	"fmt"

	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 10,
		Up:      migrateStatsLatencyColumns,
		Down:    stubDown(10),
	})
}

func migrateStatsLatencyColumns(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable("stats_hourlies") {
		return nil
	}

	columns := []struct {
		name string
		typ  string
	}{
		{"latency_p50", "BIGINT DEFAULT 0"},
		{"latency_p95", "BIGINT DEFAULT 0"},
		{"latency_p99", "BIGINT DEFAULT 0"},
		{"ftut_avg", "BIGINT DEFAULT 0"},
		{"ftut_p50", "BIGINT DEFAULT 0"},
		{"ftut_p95", "BIGINT DEFAULT 0"},
		{"ftut_p99", "BIGINT DEFAULT 0"},
		{"histogram_lt100", "BIGINT DEFAULT 0"},
		{"histogram100to500", "BIGINT DEFAULT 0"},
		{"histogram500to1k", "BIGINT DEFAULT 0"},
		{"histogram1kto5k", "BIGINT DEFAULT 0"},
		{"histogram_gt5k", "BIGINT DEFAULT 0"},
	}

	for _, col := range columns {
		if !db.Migrator().HasColumn("stats_hourlies", col.name) {
			sql := fmt.Sprintf("ALTER TABLE stats_hourlies ADD COLUMN %s %s", col.name, col.typ)
			if err := db.Exec(sql).Error; err != nil {
				return fmt.Errorf("add column %s: %w", col.name, err)
			}
		}
	}

	return nil
}
