package model

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const (
	UserRoleAdmin  = "admin"
	UserRoleEditor = "editor"
	UserRoleViewer = "viewer"
	// Lodestar commercial: minimal-privilege end-customer role (manage own API keys
	// + read public settings/stats only). Registered users get this, NOT viewer
	// (which is read-only STAFF and can see channels/logs/sites).
	UserRoleUser = "user"
)

type User struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Username string `gorm:"unique" json:"username"`
	Password string `gorm:"not null" json:"-"`
	Role     string `gorm:"default:'admin'" json:"role"`
	// Lodestar: per-user UI preferences (JSON), e.g. {"themePreset":"winter"}.
	// Lets a user's chosen theme follow their account across devices.
	Preferences string `gorm:"type:text" json:"preferences,omitempty"`
	// Lodestar commercial layer (ported from new-api's prepaid-quota model, adapted
	// to Lodestar's float-dollar cost): Quota = remaining balance (USD), UsedQuota
	// = cumulative spent. Only enforced when commercial_mode is on.
	Quota     float64 `gorm:"type:real;default:0" json:"quota"`
	UsedQuota float64 `gorm:"type:real;default:0;column:used_quota" json:"used_quota"`
	// Lodestar commercial: optional email (verified at registration when required).
	Email string `gorm:"type:varchar(256)" json:"email,omitempty"`
}

type UserLogin struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code,omitempty"` // required only when the user has 2FA enabled
	Expire   int    `json:"expire"`
}

type UserChangePassword struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

type UserChangeUsername struct {
	NewUsername string `json:"new_username"`
}

type UserBootstrapCreate struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type UserCreateRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type UserLoginResponse struct {
	Token             string `json:"token"`
	ExpireAt          string `json:"expire_at"`
	RequiresTwoFactor bool   `json:"requires_two_factor,omitempty"` // true when 2FA is on and totp_code was not supplied
}

// TableName explicitly returns "-" to prevent GORM from treating these DTOs as database tables.
func (UserLogin) TableName() string            { return "-" }
func (UserChangePassword) TableName() string   { return "-" }
func (UserChangeUsername) TableName() string   { return "-" }
func (UserBootstrapCreate) TableName() string  { return "-" }
func (UserCreateRequest) TableName() string    { return "-" }
func (UserLoginResponse) TableName() string    { return "-" }

func (u *User) HashPassword() error {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}
	u.Password = string(hashedPassword)
	return nil
}

func (u *User) ComparePassword(password string) error {
	return bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password))
}
