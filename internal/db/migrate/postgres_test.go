package migrate

import (
	"os"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// These tests exercise the migrations against a live PostgreSQL. They exist
// because migration 011 previously used MySQL/SQLite-style backtick quoting
// (`key` = ?) for the reserved-word column "key", which is a syntax error on
// PostgreSQL — the bug only surfaced when booting the new image on the
// production Postgres database. SQLite tests can't catch cross-dialect issues;
// these tests do.
//
// They are skipped unless LODESTAR_TEST_POSTGRES_DSN (or a "postgres*"
// DATABASE_URL) is set, so `go test ./...` on a dev machine without Postgres
// still passes. CI runs them via a postgres service container (see
// .github/workflows/quality.yml).

// pgTestDSN returns the PostgreSQL DSN for opt-in integration tests, or "".
func pgTestDSN() string {
	if v := os.Getenv("LODESTAR_TEST_POSTGRES_DSN"); v != "" {
		return v
	}
	if v := os.Getenv("DATABASE_URL"); strings.HasPrefix(v, "postgres") {
		return v
	}
	return ""
}

// openPostgresForTest opens a live PostgreSQL connection for integration
// tests. Skipped when no DSN is configured. The connected database MUST be a
// disposable scratch DB — tests drop and recreate tables in it.
func openPostgresForTest(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := pgTestDSN()
	if dsn == "" {
		t.Skip("set LODESTAR_TEST_POSTGRES_DSN to run PostgreSQL integration tests")
	}
	conn, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	return conn
}

// TestPostgresMigration011NavCleanup exercises migration 011 against live
// PostgreSQL. "settings.key" references a reserved word, so the original
// backtick-quoted WHERE ("`key` = ?") failed with `syntax error at or near
// "="`. Verifies the cross-dialect clause.Column quoting path works on PG.
func TestPostgresMigration011NavCleanup(t *testing.T) {
	conn := openPostgresForTest(t)

	// Clean slate on the scratch DB: drop then recreate the settings table.
	if err := conn.Migrator().DropTable("settings"); err != nil {
		t.Fatalf("drop settings: %v", err)
	}
	if err := conn.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatalf("automigrate settings: %v", err)
	}

	legacy := `["home","checkin","log","home","ops"]`
	if err := conn.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, "nav_order", legacy).Error; err != nil {
		t.Fatalf("insert nav_order: %v", err)
	}

	if err := migrateNavOrderCleanup(conn); err != nil {
		t.Fatalf("migrateNavOrderCleanup on postgres: %v", err)
	}

	var got string
	if err := conn.Raw(`SELECT value FROM settings WHERE key = ?`, "nav_order").Scan(&got).Error; err != nil {
		t.Fatalf("read nav_order: %v", err)
	}
	if strings.Contains(got, `"checkin"`) {
		t.Fatalf("nav_order still contains filtered item: %s", got)
	}
	if strings.Count(got, `"home"`) != 1 {
		t.Fatalf("nav_order should dedupe home: %s", got)
	}
	for _, item := range defaultNavOrder {
		if !strings.Contains(got, `"`+item+`"`) {
			t.Fatalf("nav_order missing default item %s: %s", item, got)
		}
	}
}

// TestPostgresMigration002DropColumn exercises migration 002's per-dialect
// ALTER TABLE DROP COLUMN against PostgreSQL, where both "key" and "base_url"
// must be double-quoted (backticks are a syntax error on PG).
func TestPostgresMigration002DropColumn(t *testing.T) {
	conn := openPostgresForTest(t)

	if err := conn.Migrator().DropTable("channels"); err != nil {
		t.Fatalf("drop channels: %v", err)
	}
	// Legacy channels table with the columns migration 002 removes. "key" is
	// quoted in DDL because it is a reserved word in some dialects.
	if err := conn.Exec(`
CREATE TABLE channels (
	id BIGSERIAL PRIMARY KEY,
	"key" TEXT,
	base_url TEXT,
	base_urls TEXT
)`).Error; err != nil {
		t.Fatalf("create legacy channels: %v", err)
	}

	if err := dropLegacyChannelColumns(conn); err != nil {
		t.Fatalf("dropLegacyChannelColumns on postgres: %v", err)
	}

	if conn.Migrator().HasColumn("channels", "key") {
		t.Fatal("channels.key should be dropped on postgres")
	}
	if conn.Migrator().HasColumn("channels", "base_url") {
		t.Fatal("channels.base_url should be dropped on postgres")
	}
}

// TestPostgresAfterAutoMigrateOnEmptyDB runs the full AfterAutoMigrate chain
// (the migration dispatch layer + migration_records bookkeeping) against an
// empty PostgreSQL database. Every migration guards on HasTable and skips
// when its target is absent, so this is a no-op for table content — but it
// proves the dispatch/record path (including upsertMigrationRecord's
// clause.OnConflict on the "version" column) is PostgreSQL-safe.
func TestPostgresAfterAutoMigrateOnEmptyDB(t *testing.T) {
	conn := openPostgresForTest(t)

	// Ensure a clean slate: drop the migration_records table if present so
	// the dispatch runs end-to-end.
	_ = conn.Migrator().DropTable("migration_records")

	if err := AfterAutoMigrate(conn); err != nil {
		t.Fatalf("AfterAutoMigrate on empty postgres: %v", err)
	}

	// The migration_records table must now exist with success rows for the
	// registered after-migrations.
	if !conn.Migrator().HasTable("migration_records") {
		t.Fatal("migration_records table not created on postgres")
	}
	var n int64
	if err := conn.Raw(`SELECT COUNT(*) FROM migration_records`).Scan(&n).Error; err != nil {
		t.Fatalf("count migration_records: %v", err)
	}
	if n == 0 {
		t.Fatal("expected migration_records rows after AfterAutoMigrate on postgres")
	}
}
