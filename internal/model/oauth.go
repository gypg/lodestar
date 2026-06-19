package model

import "time"

// OAuthBinding stores the link between a local user and an external OAuth provider
// (e.g. GitHub).  Using a separate table keeps the User model clean and is easily
// extensible to other providers later.
type OAuthBinding struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	UserID           uint      `gorm:"index;not null" json:"user_id"`
	Provider         string    `gorm:"type:varchar(32);index;not null" json:"provider"`          // e.g. "github"
	ProviderUserID   string    `gorm:"type:varchar(128);not null" json:"provider_user_id"`       // stable numeric ID from provider
	ProviderUsername string    `gorm:"type:varchar(128)" json:"provider_username"`                // display-only login name
	CreatedAt        time.Time `json:"created_at"`
}

func (OAuthBinding) TableName() string { return "oauth_bindings" }
