package model

// ChatSession stores a user's portal chat thread (messages as JSON array).
type ChatSession struct {
	ID        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID    uint   `json:"user_id" gorm:"index;not null"`
	Title     string `json:"title" gorm:"type:varchar(128);not null;default:''"`
	Model     string `json:"model" gorm:"type:varchar(128);not null;default:''"`
	APIKeyID  int    `json:"api_key_id" gorm:"not null;default:0"`
	Messages  string `json:"messages" gorm:"type:text;not null;default:'[]'"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint;index"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
}

func (ChatSession) TableName() string { return "chat_sessions" }