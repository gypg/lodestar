package model

// ImageRecord stores one generated image for a user (portal image playground history).
//
// URL holds either a remote URL or a "data:image/...;base64,..." data URI. We
// store whichever the upstream returned so playback works without re-fetching;
// the trade-off (large base64 payloads inflating the DB) is acceptable for a
// self-hosted single-user deployment and is bounded by maxRecordsPerUser.
type ImageRecord struct {
	ID        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID    uint   `json:"user_id" gorm:"index;not null"`
	Prompt    string `json:"prompt" gorm:"type:text;not null;default:''"`
	Model     string `json:"model" gorm:"type:varchar(128);not null;default:''"`
	Size      string `json:"size" gorm:"type:varchar(32);not null;default:''"`
	APIKeyID  int    `json:"api_key_id" gorm:"not null;default:0"`
	URL       string `json:"url" gorm:"type:text;not null;default:''"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index"`
}

func (ImageRecord) TableName() string { return "image_records" }
