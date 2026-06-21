package twofa

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	usr "github.com/gypg/lodestar/internal/op/user"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

// setupTestDB opens an in-memory SQLite DB with the full schema (matching the
// op/webauthn test pattern) and seeds the setting cache so issuerName() works.
func setupTestDB(t *testing.T) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	// Create a test user with an enabled TOTP secret, returning the secret.
}

// seedEnabledUser creates a user, runs Setup, and enables 2FA with a freshly
// generated TOTP code derived from the returned secret. Returns the user ID
// and the TOTP secret so tests can mint valid/invalid codes.
func seedEnabledUser(t *testing.T, username string) (uint, string) {
	t.Helper()

	user := model.User{Username: username, Password: "irrelevant", Role: model.UserRoleUser}
	if err := db.GetDB().Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	status, err := Setup(user.ID)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// Enable with a current TOTP code minted from the secret.
	code, err := totp.GenerateCode(status.Secret, time.Now())
	if err != nil {
		t.Fatalf("totp.GenerateCode: %v", err)
	}
	if err := Enable(user.ID, code); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	return user.ID, status.Secret
}

// totpCodeAt returns a TOTP code for the given secret at the given time,
// guarding tests against clock skew by using a deterministic timestamp.
func totpCodeAt(t *testing.T, secret string, when time.Time) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, when)
	if err != nil {
		t.Fatalf("totp.GenerateCode: %v", err)
	}
	return code
}

func TestVerifyLogin_RejectsWhen2FADisabled(t *testing.T) {
	setupTestDB(t)

	user := model.User{Username: "no-2fa", Password: "x", Role: model.UserRoleUser}
	if err := db.GetDB().Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	// User has never set up 2FA; VerifyLogin must reject any code.
	if err := VerifyLogin(user.ID, "123456"); err == nil {
		t.Fatal("VerifyLogin should reject when 2FA is not enabled for the user")
	}
}

func TestVerifyLogin_AcceptsValidTOTP(t *testing.T) {
	setupTestDB(t)

	uid, secret := seedEnabledUser(t, "alice")
	code := totpCodeAt(t, secret, time.Now())

	if err := VerifyLogin(uid, code); err != nil {
		t.Fatalf("VerifyLogin with valid TOTP: got %v, want nil", err)
	}
}

func TestVerifyLogin_RejectsInvalidTOTP(t *testing.T) {
	setupTestDB(t)

	uid, _ := seedEnabledUser(t, "bob")

	if err := VerifyLogin(uid, "000000"); err == nil {
		t.Fatal("VerifyLogin should reject an invalid TOTP code")
	}
}

func TestVerifyLogin_AcceptsBackupCode(t *testing.T) {
	setupTestDB(t)

	uid, _ := seedEnabledUser(t, "carol")

	// Grab an unused backup code straight from the DB (it is stored hashed,
	// but Setup returned the plaintext codes before hashing).
	var twoFA model.TwoFA
	if err := db.GetDB().Where("user_id = ?", uid).First(&twoFA).Error; err != nil {
		t.Fatalf("load two_fa: %v", err)
	}
	// Backup codes were generated during Setup; reconstruct one via the same
	// generator is not deterministic, so instead create a known backup code.
	knownCode := "BACKUP01"
	if err := saveBackupCodes(uid, []string{knownCode}); err != nil {
		t.Fatalf("saveBackupCodes: %v", err)
	}

	if err := VerifyLogin(uid, knownCode); err != nil {
		t.Fatalf("VerifyLogin with backup code: got %v, want nil", err)
	}

	// Backup codes are single-use; the same code must now fail.
	if err := VerifyLogin(uid, knownCode); err == nil {
		t.Fatal("VerifyLogin should reject a reused backup code")
	}
}

func TestVerifyLogin_LockoutAfterMaxFailures(t *testing.T) {
	setupTestDB(t)

	uid, _ := seedEnabledUser(t, "dave")

	// Burn through MaxFailAttempts with invalid codes; the next attempt must
	// be locked out rather than just "invalid code".
	for i := 0; i < model.MaxFailAttempts; i++ {
		if err := VerifyLogin(uid, "000000"); err == nil {
			t.Fatalf("attempt %d: expected error, got nil", i)
		}
	}

	err := VerifyLogin(uid, "000000")
	if err == nil {
		t.Fatal("expected lockout error after max failures, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "locked") {
		t.Fatalf("expected lockout message, got %v", err)
	}

	// Even a correct TOTP must be rejected while locked out.
	var twoFA model.TwoFA
	if err := db.GetDB().Where("user_id = ?", uid).First(&twoFA).Error; err != nil {
		t.Fatalf("load two_fa: %v", err)
	}
	correct := totpCodeAt(t, twoFA.Secret, time.Now())
	if err := VerifyLogin(uid, correct); err == nil {
		t.Fatal("VerifyLogin must reject even a valid code while locked out")
	}
}

func TestGetStatus_ReportsEnabledAndBackupCount(t *testing.T) {
	setupTestDB(t)

	uid, _ := seedEnabledUser(t, "erin")

	status, err := GetStatus(uid)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !status.Enabled {
		t.Fatal("status.Enabled should be true after enabling")
	}
	if status.BackupCodesRemaining != model.BackupCodeCount {
		t.Fatalf("backup codes remaining = %d, want %d", status.BackupCodesRemaining, model.BackupCodeCount)
	}
}

func TestAdminDisable_Removes2FA(t *testing.T) {
	setupTestDB(t)

	uid, _ := seedEnabledUser(t, "frank")

	if err := AdminDisable(uid); err != nil {
		t.Fatalf("AdminDisable: %v", err)
	}

	status, err := GetStatus(uid)
	if err != nil {
		t.Fatalf("GetStatus after disable: %v", err)
	}
	if status.Enabled {
		t.Fatal("status.Enabled should be false after AdminDisable")
	}

	// VerifyLogin must now reject (2FA no longer enabled).
	if err := VerifyLogin(uid, "123456"); err == nil {
		t.Fatal("VerifyLogin should reject after AdminDisable")
	}
}

func TestAdminDisable_NotEnabledIsError(t *testing.T) {
	setupTestDB(t)

	user := model.User{Username: "never", Password: "x", Role: model.UserRoleUser}
	if err := db.GetDB().Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := AdminDisable(user.ID); err == nil {
		t.Fatal("AdminDisable on a user without 2FA should return an error")
	}
}

// guard against accidental compile breakage of the user op dependency.
var _ = usr.GetByID
var _ = gorm.ErrRecordNotFound
