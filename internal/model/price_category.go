package model

import "time"

// PriceCategoryRule 分类规则类型。
// 命中语义（modelName 由 price.GetLLMPrice 统一小写后传入）：
//   - exact:    模型名与 rule_value 完全相等（忽略大小写）
//   - prefix:   模型名以 rule_value 为前缀
//   - contains: 模型名包含 rule_value 子串
type PriceCategoryRule string

const (
	PriceCategoryRuleExact    PriceCategoryRule = "exact"
	PriceCategoryRulePrefix   PriceCategoryRule = "prefix"
	PriceCategoryRuleContains PriceCategoryRule = "contains"
)

// ModelPriceCategory 模型价格分类：为未匹配到精确价格的模型提供按规则的兜底定价。
// 当模型名在 DB(LLMInfo) 和内置 presets 都未命中时，price.GetLLMPrice 会按
// sort_order 升序遍历启用的分类，命中第一条规则的分类用其 LLMPrice 作为兜底价
// （优先于原有的整词子串启发式兜底）。
type ModelPriceCategory struct {
	ID        uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	Name      string `json:"name" gorm:"size:191;not null;uniqueIndex"`
	RuleType  string `json:"rule_type" gorm:"size:32;not null;default:'contains'"`
	RuleValue string `json:"rule_value" gorm:"size:191;not null"`
	LLMPrice
	// SortOrder 越小越优先匹配。
	SortOrder int       `json:"sort_order" gorm:"not null;default:0;index"`
	Enabled   bool      `json:"enabled" gorm:"not null"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
