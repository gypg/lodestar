package twofa

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/user"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

// issuerName returns the TOTP issuer name from settings, falling back to app name.
func issuerName() string {
	s, err := setting.GetString(model.SettingKeyTOTPIssuer)
	if err != nil || s == "" {
		return conf.APP_NAME
	}
	return s
}

// SetupStatus is the result returned by Setup.
type SetupStatus struct {
	Secret      string   `json:"secret"`
	QRCodeURL   string   `json:"qr_code_url"`
	BackupCodes []string `json:"backup_codes"`
}

// StatusResult is the result returned by GetStatus.
type StatusResult struct {
	Enabled             bool `json:"enabled"`
	Locked              bool `json:"locked"`
	BackupCodesRemaining int  `json:"backup_codes_remaining,omitempty"`
}

// Setup generates a TOTP secret, QR provisioning URL, and backup codes.
// The 2FA record is created in a disabled state; the user must call Enable
// with a valid TOTP code to activate it.
func Setup(userID uint) (*SetupStatus, error) {
	// Check if user already has an enabled 2FA.
	existing, err := getByUserID(userID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.IsEnabled {
		return nil, fmt.Errorf("2FA is already enabled; disable it first")
	}

	// Security: do NOT allow re-setup while locked out (prevents lockout bypass).
	if existing != nil && !existing.IsEnabled && existing.IsLocked() {
		return nil, fmt.Errorf("too many failed attempts; try again after %s", existing.LockedUntil.Format("2006-01-02 15:04:05"))
	}

	// Remove a prior disabled record so we can start fresh.
	if existing != nil && !existing.IsEnabled {
		if err := deleteByUserID(userID); err != nil {
			return nil, err
		}
	}

	// Look up username for the provisioning URI.
	usr, err := user.GetByID(userID, context.Background())
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuerName(),
		AccountName: usr.Username,
	})
	if err != nil {
		return nil, fmt.Errorf("generate TOTP secret: %w", err)
	}

	backupCodes, err := generateBackupCodes()
	if err != nil {
		return nil, fmt.Errorf("generate backup codes: %w", err)
	}

	twoFA := &model.TwoFA{
		UserID:    userID,
		Secret:    key.Secret(),
		IsEnabled: false,
	}
	if err := create(twoFA); err != nil {
		return nil, err
	}

	if err := saveBackupCodes(userID, backupCodes); err != nil {
		return nil, err
	}

	return &SetupStatus{
		Secret:      key.Secret(),
		QRCodeURL:   key.URL(),
		BackupCodes: backupCodes,
	}, nil
}

// Enable verifies the TOTP code and activates 2FA.
func Enable(userID uint, code string) error {
	code = sanitizeCode(code)

	twoFA, err := getByUserID(userID)
	if err != nil {
		return err
	}
	if twoFA == nil {
		return fmt.Errorf("please complete 2FA setup first")
	}
	if twoFA.IsEnabled {
		return fmt.Errorf("2FA is already enabled")
	}

	// Security: check lockout before attempting verification.
	if twoFA.IsLocked() {
		return fmt.Errorf("too many failed attempts; try again after %s", twoFA.LockedUntil.Format("2006-01-02 15:04:05"))
	}

	if !totp.Validate(code, twoFA.Secret) {
		// Track failed attempts and apply lockout.
		twoFA.FailedAttempts++
		if twoFA.FailedAttempts >= model.MaxFailAttempts {
			lockUntil := time.Now().Add(model.LockoutDuration)
			twoFA.LockedUntil = &lockUntil
		}
		_ = update(twoFA) // best-effort persist
		return fmt.Errorf("invalid verification code")
	}

	twoFA.IsEnabled = true
	twoFA.FailedAttempts = 0
	twoFA.LockedUntil = nil
	return update(twoFA)
}

// Disable verifies a TOTP or backup code and then disables 2FA.
func Disable(userID uint, code string) error {
	code = sanitizeCode(code)

	twoFA, err := getByUserID(userID)
	if err != nil {
		return err
	}
	if twoFA == nil || !twoFA.IsEnabled {
		return model.ErrTwoFANotEnabled
	}

	if twoFA.IsLocked() {
		return fmt.Errorf("account is locked; try again after %s", twoFA.LockedUntil.Format("2006-01-02 15:04:05"))
	}

	valid, err := validateTOTPOrBackup(twoFA, code)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("invalid verification or backup code")
	}

	return deleteByUserID(userID)
}

// VerifyLogin verifies a TOTP or backup code during login. This is a
// standalone verification (not tied to the login session yet — that
// integration is deferred).
func VerifyLogin(userID uint, code string) error {
	code = sanitizeCode(code)

	twoFA, err := getByUserID(userID)
	if err != nil {
		return err
	}
	if twoFA == nil || !twoFA.IsEnabled {
		return fmt.Errorf("2FA is not enabled for this user")
	}

	if twoFA.IsLocked() {
		return fmt.Errorf("account is locked; try again after %s", twoFA.LockedUntil.Format("2006-01-02 15:04:05"))
	}

	valid, err := validateTOTPOrBackup(twoFA, code)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("invalid verification or backup code")
	}
	return nil
}

// GetStatus returns the 2FA status for a user.
func GetStatus(userID uint) (*StatusResult, error) {
	twoFA, err := getByUserID(userID)
	if err != nil {
		return nil, err
	}

	result := &StatusResult{}
	if twoFA == nil {
		return result, nil
	}

	result.Enabled = twoFA.IsEnabled
	result.Locked = twoFA.IsLocked()
	if twoFA.IsEnabled {
		count, err := unusedBackupCodeCount(userID)
		if err != nil {
			return nil, err
		}
		result.BackupCodesRemaining = count
	}
	return result, nil
}

// RegenerateBackupCodes verifies a TOTP code and replaces all backup codes.
func RegenerateBackupCodes(userID uint, code string) ([]string, error) {
	code = sanitizeCode(code)

	twoFA, err := getByUserID(userID)
	if err != nil {
		return nil, err
	}
	if twoFA == nil || !twoFA.IsEnabled {
		return nil, fmt.Errorf("2FA is not enabled")
	}

	if twoFA.IsLocked() {
		return nil, fmt.Errorf("account is locked; try again after %s", twoFA.LockedUntil.Format("2006-01-02 15:04:05"))
	}

	if !totp.Validate(code, twoFA.Secret) {
		if err := incrementFailed(twoFA); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("invalid verification code")
	}

	// Success — reset lockout state.
	resetFailed(twoFA)

	codes, err := generateBackupCodes()
	if err != nil {
		return nil, err
	}
	if err := saveBackupCodes(userID, codes); err != nil {
		return nil, err
	}
	return codes, nil
}

// AdminDisable force-disables 2FA for a target user (admin action).
func AdminDisable(targetUserID uint) error {
	twoFA, err := getByUserID(targetUserID)
	if err != nil {
		return err
	}
	if twoFA == nil || !twoFA.IsEnabled {
		return model.ErrTwoFANotEnabled
	}
	return deleteByUserID(targetUserID)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func getByUserID(userID uint) (*model.TwoFA, error) {
	var twoFA model.TwoFA
	err := db.GetDB().Where("user_id = ?", userID).First(&twoFA).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &twoFA, nil
}

func create(twoFA *model.TwoFA) error {
	return db.GetDB().Create(twoFA).Error
}

func update(twoFA *model.TwoFA) error {
	return db.GetDB().Save(twoFA).Error
}

func deleteByUserID(userID uint) error {
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(&model.TwoFABackupCode{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Where("user_id = ?", userID).Delete(&model.TwoFA{}).Error
	})
}

func incrementFailed(twoFA *model.TwoFA) error {
	twoFA.FailedAttempts++
	if twoFA.FailedAttempts >= model.MaxFailAttempts {
		lockUntil := time.Now().Add(model.LockoutDuration)
		twoFA.LockedUntil = &lockUntil
	}
	return update(twoFA)
}

func resetFailed(twoFA *model.TwoFA) {
	twoFA.FailedAttempts = 0
	twoFA.LockedUntil = nil
	now := time.Now()
	twoFA.LastUsedAt = &now
}

func validateTOTPOrBackup(twoFA *model.TwoFA, code string) (bool, error) {
	// Try TOTP first.
	if totp.Validate(code, twoFA.Secret) {
		resetFailed(twoFA)
		if err := update(twoFA); err != nil {
			return true, err // validation succeeded; log-level error
		}
		return true, nil
	}

	// Try backup codes.
	valid, err := validateBackupCode(twoFA.UserID, code)
	if err != nil {
		return false, err
	}
	if valid {
		resetFailed(twoFA)
		if err := update(twoFA); err != nil {
			return true, err
		}
		return true, nil
	}

	// Both failed — increment attempts.
	if err := incrementFailed(twoFA); err != nil {
		return false, err
	}
	return false, nil
}

// sanitizeCode strips spaces and ensures a clean input.
func sanitizeCode(code string) string {
	return strings.TrimSpace(code)
}

// ---------------------------------------------------------------------------
// Backup code helpers
// ---------------------------------------------------------------------------

func generateBackupCodes() ([]string, error) {
	codes := make([]string, 0, model.BackupCodeCount)
	for i := 0; i < model.BackupCodeCount; i++ {
		code, err := randomCode(model.BackupCodeLength)
		if err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

func randomCode(length int) (string, error) {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no I/O/0/1 to avoid confusion
	result := make([]byte, length)
	for i := range result {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		result[i] = charset[n.Int64()]
	}
	return string(result), nil
}

func hashBackupCode(code string) string {
	h := sha256.Sum256([]byte(strings.ToUpper(code)))
	return hex.EncodeToString(h[:])
}

func saveBackupCodes(userID uint, codes []string) error {
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&model.TwoFABackupCode{}).Error; err != nil {
			return err
		}
		for _, code := range codes {
			bc := model.TwoFABackupCode{
				UserID:   userID,
				CodeHash: hashBackupCode(code),
				IsUsed:   false,
			}
			if err := tx.Create(&bc).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func validateBackupCode(userID uint, code string) (bool, error) {
	normalized := strings.ToUpper(strings.TrimSpace(code))

	var backupCodes []model.TwoFABackupCode
	if err := db.GetDB().Where("user_id = ? AND is_used = false", userID).Find(&backupCodes).Error; err != nil {
		return false, err
	}

	hashedInput := hashBackupCode(normalized)
	for _, bc := range backupCodes {
		if subtle.ConstantTimeCompare([]byte(hashedInput), []byte(bc.CodeHash)) == 1 {
			now := time.Now()
			bc.IsUsed = true
			bc.UsedAt = &now
			if err := db.GetDB().Save(&bc).Error; err != nil {
				return false, err
			}
			return true, nil
		}
	}
	return false, nil
}

func unusedBackupCodeCount(userID uint) (int, error) {
	var count int64
	err := db.GetDB().Model(&model.TwoFABackupCode{}).
		Where("user_id = ? AND is_used = false", userID).
		Count(&count).Error
	return int(count), err
}
