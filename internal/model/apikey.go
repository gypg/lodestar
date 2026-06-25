package model

type APIKey struct {
	ID                int     `json:"id" gorm:"primaryKey"`
	UserID            uint    `json:"user_id" gorm:"index;default:0;constraint:OnDelete:CASCADE"` // Lodestar: owning user (commercial multi-tenant); 0 = admin/legacy unowned
	Name              string  `json:"name" gorm:"not null"`
	APIKey            string  `json:"api_key" gorm:"not null;uniqueIndex"`
	Enabled           bool    `json:"enabled" gorm:"default:true"`
	ExpireAt          int64   `json:"expire_at,omitempty"`
	MaxCost           float64 `json:"max_cost,omitempty"`
	MaxTokens         int64   `json:"max_tokens,omitempty" gorm:"default:0"` // Token 用量上限（0=不限制）
	SupportedModels   string  `json:"supported_models,omitempty"`
	RateLimitRPM      int     `json:"rate_limit_rpm,omitempty" gorm:"default:0"`
	RateLimitTPM      int     `json:"rate_limit_tpm,omitempty" gorm:"default:0"`
	PerModelQuotaJSON string  `json:"per_model_quota_json,omitempty" gorm:"column:per_model_quota_json"`
	AllowedIPs        string  `json:"allowed_ips,omitempty" gorm:"column:allowed_ips"`             // 逗号分隔的允许 IP/CIDR 列表
	Tags              string  `json:"tags,omitempty" gorm:"column:tags"`                           // 逗号分隔的标签，用于分类与快速检索
	ExcludedChannels  string  `json:"excluded_channels,omitempty" gorm:"column:excluded_channels"` // 逗号分隔的被排除渠道 ID，该 Key 不会命中这些渠道（issue #55）
}
