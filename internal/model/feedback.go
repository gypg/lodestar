package model

// Lodestar — 用户意见反馈（消费级平台的互动闭环）。
type Feedback struct {
	ID        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID    uint   `json:"user_id" gorm:"index"`
	Content   string `json:"content" gorm:"type:text;not null"`
	Contact   string `json:"contact" gorm:"type:varchar(128)"`
	Status    string `json:"status" gorm:"type:varchar(16);default:'new'"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
}

func (Feedback) TableName() string { return "feedbacks" }
