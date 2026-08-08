package dbmigration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/model"
)

// S-6 guards: a caller-supplied SQLite Path must not create directories or
// database files outside the instance data directory, and must not repoint the
// instance's own database configuration there.
//
// Entry points are TestConnection and Migrate — the two exported functions the
// HTTP handlers call (handlers/setting.go:260, :274) — rather than
// validateSQLitePath directly, so that removing the ValidateRequest call from
// either function is also caught. Assertions observe the filesystem, because
// "returned an error" alone would still pass if the directory got created first.

// escapeTarget returns a path outside dataDir, plus the directory whose absence
// proves nothing was created.
func escapeTarget(t *testing.T, dataDir string) (target, wantAbsentDir string) {
	t.Helper()
	outside := filepath.Join(filepath.Dir(dataDir), "s6-escaped")
	return filepath.Join(outside, "planted.db"), outside
}

func setupDataDir(t *testing.T) string {
	t.Helper()
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	t.Setenv("LODESTAR_DATA_DIR", dataDir)
	return dataDir
}

func TestTestConnectionRejectsPathOutsideDataDirAndCreatesNothing(t *testing.T) {
	dataDir := setupDataDir(t)
	target, wantAbsentDir := escapeTarget(t, dataDir)

	err := TestConnection(context.Background(), model.DatabaseMigrationRequest{
		Type: "sqlite",
		Path: target,
	})
	if err == nil {
		t.Fatal("TestConnection() error = nil, want rejection for path outside data dir")
	}
	if !strings.Contains(err.Error(), "data directory") {
		t.Fatalf("TestConnection() error = %v, want it to name the data directory", err)
	}
	// The point of the fix: db.ensureSQLiteDir must never run.
	if _, statErr := os.Stat(wantAbsentDir); statErr == nil {
		t.Fatalf("directory %s was created outside the data directory", wantAbsentDir)
	}
	if _, statErr := os.Stat(target); statErr == nil {
		t.Fatalf("database file %s was created outside the data directory", target)
	}
}

func TestMigrateRejectsPathOutsideDataDirAndLeavesConfigAlone(t *testing.T) {
	dataDir := setupDataDir(t)
	target, wantAbsentDir := escapeTarget(t, dataDir)

	saveCalled := false
	restore := SetSaveDatabaseConfigFuncForTest(func(string, string) error {
		saveCalled = true
		return nil
	})
	defer restore()

	result, err := Migrate(context.Background(), model.DatabaseMigrationRequest{
		Type: "sqlite",
		Path: target,
	})
	if err == nil {
		t.Fatal("Migrate() error = nil, want rejection for path outside data dir")
	}
	if result != nil {
		t.Fatalf("Migrate() result = %+v, want nil on rejection", result)
	}
	if saveCalled {
		t.Fatal("saveDatabaseConfig was called: instance DB config repointed outside the data directory")
	}
	if _, statErr := os.Stat(wantAbsentDir); statErr == nil {
		t.Fatalf("directory %s was created outside the data directory", wantAbsentDir)
	}
}

// Traversal must be judged by where the path lands, not by how it is spelled.
func TestValidateRequestRejectsTraversalEscapingDataDir(t *testing.T) {
	dataDir := setupDataDir(t)

	// Concatenated rather than filepath.Join'd so the ".." segments survive to
	// the validator: Join would Clean them into a plain escaping path, testing a
	// different (easier) case than a real traversal payload.
	sep := string(filepath.Separator)
	traversal := dataDir + sep + "sub" + sep + ".." + sep + ".." + sep + "s6-traversed" + sep + "out.db"
	if !strings.Contains(traversal, "..") {
		t.Fatalf("test setup lost the %q segments: %q", "..", traversal)
	}
	if _, err := ValidateRequest(model.DatabaseMigrationRequest{Type: "sqlite", Path: traversal}); err == nil {
		t.Fatalf("ValidateRequest(%q) error = nil, want rejection", traversal)
	}
}

// Reverse guard: a ".." that resolves back inside the data directory is a
// legitimate path, and rejecting it would be over-strict. A naive
// strings.Contains(path, "..") check fails this test.
//
// Built by string concatenation, not filepath.Join: Join calls Clean and would
// collapse the ".." before the validator ever saw it, leaving nothing to test.
func TestValidateRequestAcceptsTraversalLandingInsideDataDir(t *testing.T) {
	dataDir := setupDataDir(t)

	sep := string(filepath.Separator)
	inside := dataDir + sep + "sub" + sep + ".." + sep + "kept.db"
	if !strings.Contains(inside, "..") {
		t.Fatalf("test setup lost the %q segment: %q", "..", inside)
	}
	if _, err := ValidateRequest(model.DatabaseMigrationRequest{Type: "sqlite", Path: inside}); err != nil {
		t.Fatalf("ValidateRequest(%q) error = %v, want accepted (resolves inside data dir)", inside, err)
	}
}

// Reverse guard: the ordinary case must keep working, including nested
// subdirectories of the data directory.
func TestValidateRequestAcceptsPathsInsideDataDir(t *testing.T) {
	dataDir := setupDataDir(t)

	for _, path := range []string{
		filepath.Join(dataDir, "data.db"),
		filepath.Join(dataDir, "nested", "deeper", "data.db"),
	} {
		if _, err := ValidateRequest(model.DatabaseMigrationRequest{Type: "sqlite", Path: path}); err != nil {
			t.Fatalf("ValidateRequest(%q) error = %v, want accepted", path, err)
		}
	}
}

// The validator must resolve DSNs exactly as internal/db does. These two cases
// fail if db.SQLiteFilePath is swapped for naive string handling of the raw
// Path: the escaping URI would be mis-resolved, and the legitimate one rejected.
func TestValidateRequestHandlesFileURIForms(t *testing.T) {
	dataDir := setupDataDir(t)

	escaping := "file:" + filepath.ToSlash(filepath.Join(filepath.Dir(dataDir), "s6-uri", "out.db"))
	if _, err := ValidateRequest(model.DatabaseMigrationRequest{Type: "sqlite", Path: escaping}); err == nil {
		t.Fatalf("ValidateRequest(%q) error = nil, want rejection", escaping)
	}

	allowed := "file:" + filepath.ToSlash(filepath.Join(dataDir, "uri.db"))
	if _, err := ValidateRequest(model.DatabaseMigrationRequest{Type: "sqlite", Path: allowed}); err != nil {
		t.Fatalf("ValidateRequest(%q) error = %v, want accepted (inside data dir)", allowed, err)
	}
}

// Reverse guard: in-memory DSNs touch no filesystem, so they must not be
// confined to the data directory. Tests rely on these.
func TestValidateRequestAcceptsInMemoryDSNs(t *testing.T) {
	setupDataDir(t)

	for _, dsn := range []string{
		":memory:",
		"file::memory:",
		"file:some-name?mode=memory&cache=shared",
	} {
		if _, err := ValidateRequest(model.DatabaseMigrationRequest{Type: "sqlite", Path: dsn}); err != nil {
			t.Fatalf("ValidateRequest(%q) error = %v, want accepted (in-memory)", dsn, err)
		}
	}
}

// Reverse guard: mysql/postgres DSNs are network addresses, not local paths, and
// must not be run through the data-directory check.
func TestValidateRequestDoesNotConfineNetworkDSNs(t *testing.T) {
	setupDataDir(t)

	cases := []model.DatabaseMigrationRequest{
		{Type: "mysql", Path: "user:pass@tcp(127.0.0.1:3306)/lodestar"},
		{Type: "postgres", Path: "host=127.0.0.1 user=lodestar dbname=lodestar port=5432"},
		{Type: "postgresql", Path: "host=127.0.0.1 user=lodestar dbname=lodestar port=5432"},
	}
	for _, req := range cases {
		if _, err := ValidateRequest(req); err != nil {
			t.Fatalf("ValidateRequest(%+v) error = %v, want accepted", req, err)
		}
	}
}

func TestValidateRequestStillRejectsEmptyAndUnsupported(t *testing.T) {
	setupDataDir(t)

	if _, err := ValidateRequest(model.DatabaseMigrationRequest{Type: "sqlite", Path: "   "}); err == nil {
		t.Fatal("ValidateRequest(blank path) error = nil, want rejection")
	}
	if _, err := ValidateRequest(model.DatabaseMigrationRequest{Type: "mongodb", Path: "x"}); err == nil {
		t.Fatal("ValidateRequest(unsupported type) error = nil, want rejection")
	}
}
