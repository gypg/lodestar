package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

var ErrTwoFANotEnabled = errors.New("2FA is not enabled")

// TwoFA stores a user's TOTP-based two-factor authentication settings.
type TwoFA struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	UserID         uint       `gorm:"uniqueIndex;not null" json:"user_id"`
	Secret         string     `gorm:"type:varchar(255);not null" json:"-"` // TOTP secret, never sent to frontend
	IsEnabled      bool       `gorm:"default:false" json:"is_enabled"`
	FailedAttempts int        `gorm:"default:0" json:"failed_attempts"`
	LockedUntil    *time.Time `json:"locked_until,omitempty"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (TwoFA) TableName() string { return "two_fas" }

// TwoFABackupCode stores hashed backup codes for TOTP 2FA recovery.
type TwoFABackupCode struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	UserID    uint       `gorm:"index;not null" json:"user_id"`
	CodeHash  string     `gorm:"type:varchar(255);not null" json:"-"` // SHA-256 hash of the backup code
	IsUsed    bool       `gorm:"default:false" json:"is_used"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (TwoFABackupCode) TableName() string { return "two_fa_backup_codes" }

// IsLocked reports whether the 2FA record is currently in a locked-out state.
func (t *TwoFA) IsLocked() bool {
	if t.LockedUntil == nil {
		return false
	}
	return time.Now().Before(*t.LockedUntil)
}

const (
	MaxFailAttempts  = 5
	LockoutDuration  = 15 * time.Minute
	BackupCodeCount  = 8
	BackupCodeLength = 8
	TOTPPeriod       = 30
	TOTPSkew         = 1
)
