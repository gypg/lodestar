package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/conf"
)

// startTestConfigInvalidDB returns a config whose database type is unsupported,
// so runStart() always aborts at the db.InitDB step. That keeps a test focused
// on what happens before the database is reached without standing up a server.
func startTestConfigInvalidDB(encryptionKey string) conf.Config {
	return conf.Config{
		Server: conf.Server{
			Host: "127.0.0.1",
			Port: 0,
		},
		Database: conf.Database{
			Type: "invalid",
			Path: "ignored",
		},
		Security: conf.Security{
			EncryptionKey: encryptionKey,
		},
	}
}

func TestRunStartReturnsStartupError(t *testing.T) {
	originalConfig := conf.AppConfig
	t.Cleanup(func() {
		conf.AppConfig = originalConfig
	})

	// An encryption key is now required: runStart() fail-closes on a missing key
	// before it reaches db.InitDB, which would mask the database error this test
	// is about. See TestRunStartRefusesToStartWithoutEncryptionKey.
	conf.AppConfig = startTestConfigInvalidDB("test-encryption-key")

	err := runStart()
	if err == nil {
		t.Fatal("runStart() error = nil, want startup error")
	}
	if !strings.Contains(err.Error(), "unsupported database type: invalid") {
		t.Fatalf("runStart() error = %q, want unsupported database type", err)
	}
}

func TestRunStartRefusesToStartWithoutEncryptionKey(t *testing.T) {
	originalConfig := conf.AppConfig
	originalFlag := allowEphemeralEncryptionKey
	t.Cleanup(func() {
		conf.AppConfig = originalConfig
		allowEphemeralEncryptionKey = originalFlag
	})

	// The main database is a *valid* sqlite path under a directory that does not
	// exist yet, so db.InitDB would create it (ensureSQLiteDir). That directory is
	// the observable side effect: asserting only on the returned error would still
	// pass if the guard were moved below db.InitDB.
	//
	// The log database is deliberately unopenable so that startup aborts one step
	// after the main database instead of running to completion and blocking in
	// shutdown.Listen() — a mutation that removes the guard must fail on an
	// assertion, not by hanging until the test timeout.
	dbDir := filepath.Join(t.TempDir(), "created-by-db-init")
	conf.AppConfig = conf.Config{
		Server: conf.Server{Host: "127.0.0.1", Port: 0},
		Database: conf.Database{
			Type:    "sqlite",
			Path:    filepath.Join(dbDir, "lodestar.db"),
			LogType: "invalid",
			LogPath: "ignored",
		},
		Security: conf.Security{EncryptionKey: ""},
	}
	allowEphemeralEncryptionKey = false

	err := runStart()
	if err == nil {
		t.Fatal("runStart() error = nil, want refusal to start without an encryption key")
	}
	if !strings.Contains(err.Error(), "security.encryption_key is not set") {
		t.Fatalf("runStart() error = %q, want security.encryption_key refusal", err)
	}
	if _, statErr := os.Stat(dbDir); !os.IsNotExist(statErr) {
		t.Fatalf("os.Stat(%q) err = %v, want IsNotExist: the encryption guard must run before the database is opened", dbDir, statErr)
	}
}

func TestRunStartAllowsEphemeralKeyBehindExplicitFlag(t *testing.T) {
	originalConfig := conf.AppConfig
	originalFlag := allowEphemeralEncryptionKey
	t.Cleanup(func() {
		conf.AppConfig = originalConfig
		allowEphemeralEncryptionKey = originalFlag
	})

	conf.AppConfig = startTestConfigInvalidDB("")
	allowEphemeralEncryptionKey = true

	// Reverse guard: with the escape hatch explicitly enabled, startup must get
	// *past* the encryption step and fail on the database instead. Without this
	// case, a "fix" that rejects every empty key unconditionally — breaking the
	// documented local-development flow — would still pass the test above.
	err := runStart()
	if err == nil {
		t.Fatal("runStart() error = nil, want startup to proceed to database init")
	}
	if !strings.Contains(err.Error(), "unsupported database type: invalid") {
		t.Fatalf("runStart() error = %q, want startup to reach database init", err)
	}
}

// TestEphemeralEncryptionKeyFlagDefaultsToOff guards the flag *registration*,
// not the package variable the tests above assign directly. Registering the flag
// with a default of true would reinstate the original defect for every real
// invocation while leaving those tests green (verified: a binary built that way
// boots with a generated key and only logs a warning).
func TestEphemeralEncryptionKeyFlagDefaultsToOff(t *testing.T) {
	flag := startCmd.PersistentFlags().Lookup(allowEphemeralEncryptionKeyFlag)
	if flag == nil {
		t.Fatalf("flag --%s is not registered: the documented local-development escape hatch is unreachable", allowEphemeralEncryptionKeyFlag)
	}
	if flag.DefValue != "false" {
		t.Fatalf("flag --%s default = %q, want \"false\": startup must fail closed unless the operator opts in", allowEphemeralEncryptionKeyFlag, flag.DefValue)
	}
}

func TestRunStartAcceptsConfiguredKeyWithFlagUnset(t *testing.T) {
	originalConfig := conf.AppConfig
	originalFlag := allowEphemeralEncryptionKey
	t.Cleanup(func() {
		conf.AppConfig = originalConfig
		allowEphemeralEncryptionKey = originalFlag
	})

	// Reverse guard for the common production shape: key configured, flag off.
	// Startup must not be blocked by the new check.
	conf.AppConfig = startTestConfigInvalidDB("test-encryption-key")
	allowEphemeralEncryptionKey = false

	err := runStart()
	if err == nil {
		t.Fatal("runStart() error = nil, want startup to proceed to database init")
	}
	if strings.Contains(err.Error(), "security.encryption_key") {
		t.Fatalf("runStart() error = %q, want the configured key to be accepted", err)
	}
	if !strings.Contains(err.Error(), "unsupported database type: invalid") {
		t.Fatalf("runStart() error = %q, want startup to reach database init", err)
	}
}
