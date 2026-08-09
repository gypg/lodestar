package setting

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/utils/crypto"
)

// initSettingTestDB brings up an in-memory SQLite database plus a populated
// setting cache, mirroring the production path (RefreshCache loads the DB rows
// into the cache verbatim, ciphertext included).
func initSettingTestDB(t *testing.T) context.Context {
	t.Helper()

	ctx := context.Background()
	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", testName)

	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// One key for the whole process: crypto.Init is a sync.Once with no exported
	// reset seam, so a rotated key cannot be simulated by calling Init twice.
	crypto.Init("setting-selflock-test-key")

	if err := RefreshCache(ctx); err != nil {
		t.Fatalf("refresh cache: %v", err)
	}
	return ctx
}

// undecryptableCiphertext returns a well-formed "enc:" payload that this process
// cannot decrypt. It reproduces the exact failure a lost or rotated encryption
// key produces: crypto.Decrypt reaches gcm.Open and gets an authentication
// failure, returning crypto.ErrDecryptFailed. Random bytes and real ciphertext
// written under a different key are indistinguishable at that point.
func undecryptableCiphertext(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 64)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	value := "enc:" + base64.StdEncoding.EncodeToString(buf)
	if _, err := crypto.Decrypt(value); err != crypto.ErrDecryptFailed {
		t.Fatalf("crypto.Decrypt(fixture) error = %v, want ErrDecryptFailed", err)
	}
	return value
}

// writeRowDirect writes a setting value straight to the database, bypassing the
// cache. Used both to plant undecryptable ciphertext and to leave a marker that
// proves whether a later SetString actually wrote.
func writeRowDirect(t *testing.T, key model.SettingKey, value string) {
	t.Helper()
	result := db.GetDB().Model(&model.Setting{Key: key}).Update("Value", value)
	if result.Error != nil {
		t.Fatalf("seed setting row: %v", result.Error)
	}
	if result.RowsAffected != 1 {
		t.Fatalf("seed setting row affected %d rows, want 1", result.RowsAffected)
	}
}

func readRowDirect(t *testing.T, key model.SettingKey) string {
	t.Helper()
	var row model.Setting
	if err := db.GetDB().Where("key = ?", key).First(&row).Error; err != nil {
		t.Fatalf("read setting row: %v", err)
	}
	return row.Value
}

// TestSetStringOverwritesUndecryptableValue is the core self-lock regression.
// Entry point is SetString (the guarded call site), not crypto.Decrypt, and the
// assertion lands on the database row rather than on the returned error: a fix
// that returned nil while skipping the write would leave the setting just as
// stuck as before.
func TestSetStringOverwritesUndecryptableValue(t *testing.T) {
	initSettingTestDB(t)

	const key = model.SettingKeyStripeAPIKey
	stale := undecryptableCiphertext(t)
	writeRowDirect(t, key, stale)
	settingCache.Set(key, stale)

	if err := SetString(key, "sk_live_repaired"); err != nil {
		t.Fatalf("SetString() error = %v, want nil so the operator can repair the value", err)
	}

	stored := readRowDirect(t, key)
	if stored == stale {
		t.Fatal("setting row still holds the undecryptable value: SetString did not write")
	}
	// Sensitive keys must still be persisted encrypted. This also pins the order
	// of operations: encrypting after the write would store plaintext here.
	if !strings.HasPrefix(stored, "enc:") {
		t.Fatalf("stored value = %q, want an enc: prefixed ciphertext", stored)
	}
	got, err := GetString(key)
	if err != nil {
		t.Fatalf("GetString() error = %v, want the repaired value to be readable", err)
	}
	if got != "sk_live_repaired" {
		t.Fatalf("GetString() = %q, want sk_live_repaired", got)
	}
}

// TestSetStringClearsUndecryptableValue covers clearing a credential rather than
// replacing it. Comparing an ignored-error decrypt result ("" on failure)
// against an empty new value would report "unchanged" and silently skip the
// write, leaving the row locked.
func TestSetStringClearsUndecryptableValue(t *testing.T) {
	initSettingTestDB(t)

	const key = model.SettingKeyStripeWebhookSecret
	stale := undecryptableCiphertext(t)
	writeRowDirect(t, key, stale)
	settingCache.Set(key, stale)

	if err := SetString(key, ""); err != nil {
		t.Fatalf("SetString(empty) error = %v, want nil", err)
	}

	if stored := readRowDirect(t, key); stored != "" {
		t.Fatalf("stored value = %q, want empty: clearing an undecryptable credential must reach the database", stored)
	}
}

// TestSetStringKeepsNoOpForUnchangedValue is the reverse guard. Without it, a
// "fix" that dropped the equality check entirely — writing on every call and
// re-encrypting with a fresh nonce each time — would pass the tests above.
// The marker row makes the no-op observable as a side effect.
func TestSetStringKeepsNoOpForUnchangedValue(t *testing.T) {
	initSettingTestDB(t)

	const key = model.SettingKeyStripeAPIKey
	if err := SetString(key, "sk_live_unchanged"); err != nil {
		t.Fatalf("SetString() error = %v", err)
	}

	// Plant a marker behind the cache's back. A genuine no-op leaves it intact.
	writeRowDirect(t, key, "marker-must-survive")

	if err := SetString(key, "sk_live_unchanged"); err != nil {
		t.Fatalf("SetString(same value) error = %v, want nil", err)
	}
	if stored := readRowDirect(t, key); stored != "marker-must-survive" {
		t.Fatalf("stored value = %q, want marker-must-survive: writing an unchanged value is a needless key rotation", stored)
	}
}

// TestSetStringStillRejectsUnknownKey pins the one error path that must survive:
// a key absent from the cache is a caller bug, not a decryption problem.
func TestSetStringStillRejectsUnknownKey(t *testing.T) {
	initSettingTestDB(t)

	err := SetString(model.SettingKey("no-such-setting-key"), "value")
	if err == nil {
		t.Fatal("SetString(unknown key) error = nil, want not-found error")
	}
	if !strings.Contains(err.Error(), "setting not found") {
		t.Fatalf("SetString(unknown key) error = %q, want setting not found", err)
	}
}

// TestGetStringStillReportsDecryptFailure documents the deliberate asymmetry:
// reads must keep failing loudly on an undecryptable value. Only the write path
// was unlocked, so callers cannot mistake a corrupt credential for an empty one.
func TestGetStringStillReportsDecryptFailure(t *testing.T) {
	initSettingTestDB(t)

	const key = model.SettingKeyStripeAPIKey
	stale := undecryptableCiphertext(t)
	writeRowDirect(t, key, stale)
	settingCache.Set(key, stale)

	got, err := GetString(key)
	if err == nil {
		t.Fatalf("GetString() error = nil (value %q), want decrypt failure", got)
	}
	if got != "" {
		t.Fatalf("GetString() = %q, want empty on decrypt failure", got)
	}
}
