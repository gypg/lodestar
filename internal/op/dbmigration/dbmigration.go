package dbmigration

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/backup"
)

type SaveDatabaseConfigFunc func(dbType, path string) error

var saveDatabaseConfig SaveDatabaseConfigFunc = conf.SaveDatabaseConfig

func SetSaveDatabaseConfigFuncForTest(fn SaveDatabaseConfigFunc) func() {
	old := saveDatabaseConfig
	saveDatabaseConfig = fn
	return func() { saveDatabaseConfig = old }
}

func ValidateRequest(req model.DatabaseMigrationRequest) (model.DatabaseMigrationRequest, error) {
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	req.Path = strings.TrimSpace(req.Path)
	if req.Type == "postgresql" {
		req.Type = "postgres"
	}
	if req.Type != "sqlite" && req.Type != "mysql" && req.Type != "postgres" {
		return req, fmt.Errorf("unsupported database type: %s", req.Type)
	}
	if req.Path == "" {
		return req, fmt.Errorf("database path is required")
	}
	if req.Type == "sqlite" {
		if err := validateSQLitePath(req.Path); err != nil {
			return req, err
		}
	}
	return req, nil
}

// validateSQLitePath confines a caller-supplied SQLite path to the instance data
// directory.
//
// Without this, both /api/v1/setting/database/test and /database/migrate reach
// db.ensureSQLiteDir (db.go:466), whose os.MkdirAll(dir, 0755) creates
// directories at any depth the process can write, and GORM then creates a
// database file there. Migrate additionally persists the path into config.json
// via saveDatabaseConfig (:83), repointing this instance's own database on the
// next restart. Both routes require auth.PermSettingsWrite (handlers/setting.go
// :62, :68), which the editor role also holds (auth/permissions.go:43) — so this
// is not anonymously reachable, but it does let a non-admin role write outside
// the data directory and reconfigure the instance.
//
// Only sqlite is checked: mysql/postgres paths are network DSNs handed to their
// drivers and never resolve to a local file.
func validateSQLitePath(path string) error {
	// Resolved by the same function the sink uses, so validation cannot disagree
	// with what actually gets created. In-memory DSNs touch no files at all.
	filePath, onDisk := db.SQLiteFilePath(path)
	if !onDisk {
		return nil
	}
	if strings.TrimSpace(filePath) == "" {
		return fmt.Errorf("database path is required")
	}

	dataDir, err := filepath.Abs(conf.DataDir())
	if err != nil {
		return fmt.Errorf("resolve data directory: %w", err)
	}
	// Abs also applies filepath.Clean, collapsing any "..", so a traversal is
	// judged by where it lands rather than by how it is spelled. Relative inputs
	// resolve against the process working directory, which is how the sink reads
	// them too.
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("resolve database path: %w", err)
	}

	rel, err := filepath.Rel(dataDir, absPath)
	if err != nil || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("database path must stay inside the data directory (%s)", dataDir)
	}
	return nil
}

func TestConnection(ctx context.Context, req model.DatabaseMigrationRequest) error {
	req, err := ValidateRequest(req)
	if err != nil {
		return err
	}
	target, err := db.OpenStandalone(req.Type, req.Path, false)
	if err != nil {
		return err
	}
	sqlDB, err := target.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	return sqlDB.PingContext(ctx)
}

func Migrate(ctx context.Context, req model.DatabaseMigrationRequest) (*model.DatabaseMigrationResult, error) {
	req, err := ValidateRequest(req)
	if err != nil {
		return nil, err
	}
	dump, err := backup.ExportAll(ctx, req.IncludeLogs, req.IncludeStats)
	if err != nil {
		return nil, fmt.Errorf("export current database: %w", err)
	}

	target, err := db.OpenStandalone(req.Type, req.Path, false)
	if err != nil {
		return nil, fmt.Errorf("open target database: %w", err)
	}
	sqlDB, err := target.DB()
	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()

	if err := db.Migrate(target); err != nil {
		return nil, fmt.Errorf("migrate target schema: %w", err)
	}
	importResult, err := backup.ImportWithModeToDB(ctx, target, dump, model.ImportModeFull)
	if err != nil {
		return nil, fmt.Errorf("import target database: %w", err)
	}
	if err := saveDatabaseConfig(req.Type, req.Path); err != nil {
		return nil, err
	}
	return &model.DatabaseMigrationResult{
		Type:          req.Type,
		Path:          req.Path,
		IncludeLogs:   req.IncludeLogs,
		IncludeStats:  req.IncludeStats,
		RestartNeeded: true,
		ImportResult:  *importResult,
	}, nil
}
