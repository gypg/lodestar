package model

/*
Lodestar — subscription system data models.

Ported from New API's subscription system (github.com/QuantumNous/new-api,
AGPL-3.0 — see NOTICE.md): model/subscription.go, controller/subscription.go.
Simplified for Lodestar:
- No Stripe/Creem/Waffo price IDs (will add later)
- No upgrade_group logic (keep simple)
- No quota reset period (can add later)
- Core lifecycle: plan -> order -> subscription
- Balance is float64 USD (matching Lodestar's quota model)
*/

import "encoding/json"

const (
	// Subscription duration types
	SubDurationMonth  = "month"
	SubDurationDay    = "day"
	SubDurationHour   = "hour"
	SubDurationCustom = "custom"

	// Subscription order statuses
	SubOrderStatusPending   = "pending"
	SubOrderStatusSuccess   = "success"
	SubOrderStatusExpired   = "expired"
	SubOrderStatusCancelled = "cancelled"

	// Subscription statuses
	SubStatusActive    = "active"
	SubStatusExpired   = "expired"
	SubStatusCancelled = "cancelled"
)

// SubscriptionPlan defines a purchasable subscription plan.
type SubscriptionPlan struct {
	ID              int     `json:"id" gorm:"primaryKey;autoIncrement"`
	Name            string  `json:"name" gorm:"type:varchar(128);not null"`
	Description     string  `json:"description" gorm:"type:varchar(512);default:''"`
	Price           float64 `json:"price" gorm:"type:double precision;not null;default:0"` // USD
	Currency        string  `json:"currency" gorm:"type:varchar(8);not null;default:'USD'"`
	DurationType    string  `json:"duration_type" gorm:"type:varchar(16);not null;default:'month'"` // month/day/hour/custom
	DurationDays    int     `json:"duration_days" gorm:"type:int;not null;default:30"`
	CustomDurationS int64   `json:"custom_duration_s" gorm:"type:bigint;not null;default:0"`      // seconds, for custom
	QuotaAmount     float64 `json:"quota_amount" gorm:"type:double precision;not null;default:0"` // USD quota pool granted; must be > 0 to be sellable
	// Rolling spend windows (WO-054). 0 disables the window — plans created
	// before this field existed keep their pool-only semantics. When set,
	// spend inside the window is capped on top of the total pool: exhausting
	// a window does NOT fall through to the wallet, it rate-limits until the
	// window resets.
	Window4hLimit     float64 `json:"window_4h_limit" gorm:"type:double precision;not null;default:0"`
	WindowWeeklyLimit float64 `json:"window_weekly_limit" gorm:"type:double precision;not null;default:0"`
	Enabled           bool    `json:"enabled" gorm:"default:true"`
	SortOrder         int     `json:"sort_order" gorm:"type:int;default:0"`
	CreatedAt         int64   `json:"created_at" gorm:"bigint"`
	UpdatedAt         int64   `json:"updated_at" gorm:"bigint"`
	// EnabledSet 标记请求里是否显式给出了 enabled（仿 SiteAccount 的 *Set 模式）。
	// 只在内存里传递，不落库：Enabled 的 default:true 标签会让 struct Create
	// 把显式的 false 吞成 true（停用套餐创建即上架），CreatePlan 靠它事后补偿。
	EnabledSet bool `json:"-" gorm:"-"`
}

func (SubscriptionPlan) TableName() string { return "subscription_plans" }

// UnmarshalJSON 记录 "enabled" 键是否出现：裸 bool 下"字段缺失"（默认启用）
// 与"显式 false（管理员要求下架）"到达 Go 时都是 false，必须可区分。
func (p *SubscriptionPlan) UnmarshalJSON(data []byte) error {
	type alias SubscriptionPlan
	aux := struct {
		*alias
	}{alias: (*alias)(p)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	_, p.EnabledSet = raw["enabled"]
	return nil
}

// SubscriptionOrder represents a purchase attempt for a plan.
type SubscriptionOrder struct {
	ID            int     `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID        uint    `json:"user_id" gorm:"index;not null"`
	PlanID        int     `json:"plan_id" gorm:"index;not null"`
	TradeNo       string  `json:"trade_no" gorm:"type:varchar(128);uniqueIndex;not null"`
	Money         float64 `json:"money" gorm:"type:double precision;not null"`
	PaymentMethod string  `json:"payment_method" gorm:"type:varchar(32);default:'balance'"`
	Status        string  `json:"status" gorm:"type:varchar(16);default:'pending'"`
	CreatedAt     int64   `json:"created_at" gorm:"bigint"`
	CompletedAt   int64   `json:"completed_at" gorm:"bigint;default:0"`
}

func (SubscriptionOrder) TableName() string { return "subscription_orders" }

// UserSubscription represents an active or historical subscription for a user.
type UserSubscription struct {
	ID          int     `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID      uint    `json:"user_id" gorm:"index;not null"`
	PlanID      int     `json:"plan_id" gorm:"index;not null"`
	OrderID     int     `json:"order_id" gorm:"index;default:0"`
	AmountTotal float64 `json:"amount_total" gorm:"type:double precision;not null;default:0"` // USD quota total
	AmountUsed  float64 `json:"amount_used" gorm:"type:double precision;not null;default:0"`  // USD quota used
	StartsAt    int64   `json:"starts_at" gorm:"bigint"`
	ExpiresAt   int64   `json:"expires_at" gorm:"bigint;index"`
	Status      string  `json:"status" gorm:"type:varchar(16);index;default:'active'"` // active/expired/cancelled
	Source      string  `json:"source" gorm:"type:varchar(16);default:'order'"`        // order/admin
	CreatedAt   int64   `json:"created_at" gorm:"bigint"`
}

func (UserSubscription) TableName() string { return "user_subscriptions" }

// Subscription window kinds. A window row tracks spend inside one rolling
// period anchored at the subscription's start (NOT calendar-aligned — the
// operator-facing spec explicitly says "from when your plan started").
const (
	SubWindow4h     = "4h"
	SubWindowWeekly = "weekly"
)

// SubscriptionWindow is the per-subscription rolling-window spend state.
//
// One row per (subscription, kind). WindowStart is the current period's
// anchor; when now >= WindowStart + period, the window rolls: used resets and
// the anchor advances (possibly several periods at once after idle). Updates
// are compare-and-swap on (window_start, used) — the pool's CAS pattern, for
// the same reason: read-then-write lets two parallel requests double-spend.
type SubscriptionWindow struct {
	ID             int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	SubscriptionID int     `json:"subscription_id" gorm:"uniqueIndex:idx_sub_window_kind;not null"`
	Kind           string  `json:"kind" gorm:"uniqueIndex:idx_sub_window_kind;type:varchar(16);not null"`
	LimitUSD       float64 `json:"limit_usd" gorm:"type:double precision;not null;default:0"`
	WindowStart    int64   `json:"window_start" gorm:"bigint;not null"`
	Used           float64 `json:"used" gorm:"type:double precision;not null;default:0"`
	UpdatedAt      int64   `json:"updated_at" gorm:"bigint"`
}

func (SubscriptionWindow) TableName() string { return "subscription_windows" }
