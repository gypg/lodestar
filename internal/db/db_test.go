package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestConfigureConnectionPoolLimitsSQLiteConnections(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer sqlDB.Close()

	configureConnectionPool(sqlDB, "sqlite")

	stats := sqlDB.Stats()
	if stats.MaxOpenConnections != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", stats.MaxOpenConnections)
	}
}

func TestInitSQLiteCreatesParentDir(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nested", "lodestar.db")

	gdb, err := initSQLite(dbPath, &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("initSQLite() error = %v", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("gdb.DB() error = %v", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("sqlDB.Ping() error = %v", err)
	}
	if _, err := os.Stat(filepath.Dir(dbPath)); err != nil {
		t.Fatalf("os.Stat(parent dir) error = %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("os.Stat(db file) error = %v", err)
	}
}

func TestSQLiteDSNAppendsParamsWithExistingQuery(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "lodestar.db") + "?_txlock=immediate"

	dsn, err := sqliteDSN(dbPath)
	if err != nil {
		t.Fatalf("sqliteDSN() error = %v", err)
	}
	got := dsn + sqliteDSNSeparator(dsn) + "_journal_mode=WAL"
	if !strings.Contains(got, "?_txlock=immediate&_journal_mode=WAL") {
		t.Fatalf("combined DSN = %q, want query parameters appended with '&'", got)
	}
}

func TestInitSQLiteCreatesParentDirForFileURI(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nested", "lodestar.db")
	dsn := "file:" + filepath.ToSlash(dbPath) + "?_txlock=immediate"

	gdb, err := initSQLite(dsn, &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("initSQLite() error = %v", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("gdb.DB() error = %v", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("sqlDB.Ping() error = %v", err)
	}
	if _, err := os.Stat(filepath.Dir(dbPath)); err != nil {
		t.Fatalf("os.Stat(parent dir) error = %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("os.Stat(db file) error = %v", err)
	}
}

func TestSQLiteDSNSkipsDirCreationForMemoryFileURI(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nested", "lodestar.db")
	dsn := "file:" + filepath.ToSlash(dbPath) + "?mode=memory&cache=shared"

	got, err := sqliteDSN(dsn)
	if err != nil {
		t.Fatalf("sqliteDSN() error = %v", err)
	}
	if got != dsn {
		t.Fatalf("sqliteDSN() = %q, want %q", got, dsn)
	}
	if _, err := os.Stat(filepath.Dir(dbPath)); !os.IsNotExist(err) {
		t.Fatalf("expected memory sqlite DSN not to create parent dir, stat error = %v", err)
	}
}

func resetLogDBStateForTest(t *testing.T) {
	t.Helper()
	logDBLock.Lock()
	if logDB != nil {
		_ = closeConn(logDB)
	}
	logDB = nil
	logDBType = ""
	logDBPath = ""
	currentLogDBType = ""
	logDBDebug = false
	logDBLock.Unlock()
}

func TestInitLogDBSharedFallsBackToMainDB(t *testing.T) {
	resetLogDBStateForTest(t)
	mainPath := filepath.Join(t.TempDir(), "main.db")
	if err := InitDB("sqlite", mainPath, false); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(func() { _ = Close(); resetLogDBStateForTest(t) })

	// Empty log config => shared with main DB.
	if err := InitLogDB("", "", false); err != nil {
		t.Fatalf("InitLogDB() error = %v", err)
	}
	if IsLogDBSeparate() {
		t.Fatalf("IsLogDBSeparate() = true, want false for empty config")
	}
	if GetLogDB() != GetDB() {
		t.Fatalf("GetLogDB() should return the main DB in shared mode")
	}
	// Close/reopen are no-ops in shared mode and must never nil out the main DB.
	if err := CloseLogDB(); err != nil {
		t.Fatalf("CloseLogDB() shared no-op error = %v", err)
	}
	if GetLogDB() != GetDB() {
		t.Fatalf("GetLogDB() should still return main DB after no-op CloseLogDB")
	}
}

func TestInitLogDBSeparateRoutesAndLifecycle(t *testing.T) {
	resetLogDBStateForTest(t)
	mainPath := filepath.Join(t.TempDir(), "main.db")
	if err := InitDB("sqlite", mainPath, false); err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "logs.db")
	if err := InitLogDB("sqlite", logPath, false); err != nil {
		t.Fatalf("InitLogDB() error = %v", err)
	}
	t.Cleanup(func() { _ = Close(); resetLogDBStateForTest(t) })

	if !IsLogDBSeparate() {
		t.Fatalf("IsLogDBSeparate() = false, want true for separate config")
	}
	logConn := GetLogDB()
	if logConn == nil {
		t.Fatalf("GetLogDB() = nil, want separate connection")
	}
	if logConn == GetDB() {
		t.Fatalf("GetLogDB() must differ from main DB in separate mode")
	}

	// relay_logs lives on the log DB; the log file must exist after migration.
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("log DB file not created: %v", err)
	}

	// Close drops the connection; GetLogDB returns nil (callers must guard).
	if err := CloseLogDB(); err != nil {
		t.Fatalf("CloseLogDB() error = %v", err)
	}
	if GetLogDB() != nil {
		t.Fatalf("GetLogDB() should be nil after CloseLogDB in separate mode")
	}
	if !IsLogDBSeparate() {
		t.Fatalf("IsLogDBSeparate() should remain true after close (config retained)")
	}

	// Reopen restores a working connection.
	if err := ReopenLogDB(); err != nil {
		t.Fatalf("ReopenLogDB() error = %v", err)
	}
	if GetLogDB() == nil {
		t.Fatalf("GetLogDB() = nil after ReopenLogDB, want connection")
	}
	// Main DB must remain usable throughout.
	if GetDB() == nil {
		t.Fatalf("main DB nil after log DB lifecycle")
	}
}
