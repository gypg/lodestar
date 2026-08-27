package migrate

import (
	"fmt"

	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 8,
		Up:      migrateStatsHourlyCompositePrimaryKey,
		Down:    stubDown(8),
	})
}

func migrateStatsHourlyCompositePrimaryKey(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable("stats_hourlies") {
		return nil
	}

	switch db.Dialector.Name() {
	case "sqlite":
		return migrateStatsHourlyCompositePrimaryKeySQLite(db)
	case "mysql":
		return migrateStatsHourlyCompositePrimaryKeyMySQL(db)
	case "postgres", "postgresql":
		return migrateStatsHourlyCompositePrimaryKeyPostgres(db)
	default:
		return nil
	}
}

func migrateStatsHourlyCompositePrimaryKeySQLite(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
CREATE TABLE IF NOT EXISTS stats_hourlies_new (
    hour INTEGER NOT NULL,
    date TEXT NOT NULL,
    input_token BIGINT,
    output_token BIGINT,
    -- REAL 在这里是安全的：本分支只跑 SQLite，而 SQLite 的 REAL 恒为 8 字节
    -- IEEE 双精度。这**不是** migrate/020.go 修的那个缺陷 —— 那个缺陷是在
    -- Postgres 上，real = float4（4 字节）。别顺手把这两行改成
    -- double precision：SQLite 的类型名只决定亲和，改了没有区别。
    input_cost REAL,
    output_cost REAL,
    wait_time BIGINT,
    request_success BIGINT,
    request_failed BIGINT,
    PRIMARY KEY (hour, date)
)`).Error; err != nil {
			return fmt.Errorf("create stats_hourlies_new: %w", err)
		}

		if err := tx.Exec(`
INSERT OR REPLACE INTO stats_hourlies_new (hour, date, input_token, output_token, input_cost, output_cost, wait_time, request_success, request_failed)
SELECT hour, date, input_token, output_token, input_cost, output_cost, wait_time, request_success, request_failed
FROM stats_hourlies
WHERE hour BETWEEN 0 AND 23 AND date IS NOT NULL AND TRIM(date) != ''`).Error; err != nil {
			return fmt.Errorf("copy stats_hourlies: %w", err)
		}

		if err := tx.Exec(`DROP TABLE stats_hourlies`).Error; err != nil {
			return fmt.Errorf("drop old stats_hourlies: %w", err)
		}
		if err := tx.Exec(`ALTER TABLE stats_hourlies_new RENAME TO stats_hourlies`).Error; err != nil {
			return fmt.Errorf("rename stats_hourlies_new: %w", err)
		}
		return nil
	})
}

func migrateStatsHourlyCompositePrimaryKeyMySQL(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		_ = tx.Exec("ALTER TABLE stats_hourlies DROP PRIMARY KEY").Error
		if err := tx.Exec("ALTER TABLE stats_hourlies ADD PRIMARY KEY (hour, date)").Error; err != nil {
			return fmt.Errorf("alter stats_hourlies primary key: %w", err)
		}
		return nil
	})
}

func migrateStatsHourlyCompositePrimaryKeyPostgres(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		_ = tx.Exec("ALTER TABLE stats_hourlies DROP CONSTRAINT IF EXISTS stats_hourlies_pkey").Error
		if err := tx.Exec("ALTER TABLE stats_hourlies ADD PRIMARY KEY (hour, date)").Error; err != nil {
			return fmt.Errorf("alter stats_hourlies primary key: %w", err)
		}
		return nil
	})
}
