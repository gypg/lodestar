package model

/*
Lodestar — subscription system data models.

Ported from GGGZERO's subscription system, simplified for Lodestar:
- No Stripe/Creem/Waffo price IDs (will add later)
- No upgrade_group logic (keep simple)
- No quota reset period (can add later)
- Core lifecycle: plan -> order -> subscription
- Balance is float64 USD (matching Lodestar's quota model)
*/

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
	Price           float64 `json:"price" gorm:"type:real;not null;default:0"` // USD
	Currency        string  `json:"currency" gorm:"type:varchar(8);not null;default:'USD'"`
	DurationType    string  `json:"duration_type" gorm:"type:varchar(16);not null;default:'month'"` // month/day/hour/custom
	DurationDays    int     `json:"duration_days" gorm:"type:int;not null;default:30"`
	CustomDurationS int64   `json:"custom_duration_s" gorm:"type:bigint;not null;default:0"` // seconds, for custom
	QuotaAmount     float64 `json:"quota_amount" gorm:"type:real;not null;default:0"`          // USD quota granted (0 = unlimited)
	Enabled         bool    `json:"enabled" gorm:"default:true"`
	SortOrder       int     `json:"sort_order" gorm:"type:int;default:0"`
	CreatedAt       int64   `json:"created_at" gorm:"bigint"`
	UpdatedAt       int64   `json:"updated_at" gorm:"bigint"`
}

func (SubscriptionPlan) TableName() string { return "subscription_plans" }

// SubscriptionOrder represents a purchase attempt for a plan.
type SubscriptionOrder struct {
	ID            int     `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID        uint    `json:"user_id" gorm:"index;not null"`
	PlanID        int     `json:"plan_id" gorm:"index;not null"`
	TradeNo       string  `json:"trade_no" gorm:"type:varchar(128);uniqueIndex;not null"`
	Money         float64 `json:"money" gorm:"type:real;not null"`
	PaymentMethod string  `json:"payment_method" gorm:"type:varchar(32);default:'balance'"`
	Status        string  `json:"status" gorm:"type:varchar(16);default:'pending'"`
	CreatedAt     int64   `json:"created_at" gorm:"bigint"`
	CompletedAt   int64   `json:"completed_at" gorm:"bigint;default:0"`
}

func (SubscriptionOrder) TableName() string { return "subscription_orders" }

// UserSubscription represents an active or historical subscription for a user.
type UserSubscription struct {
	ID          int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID      uint   `json:"user_id" gorm:"index;not null"`
	PlanID      int    `json:"plan_id" gorm:"index;not null"`
	OrderID     int    `json:"order_id" gorm:"index;default:0"`
	AmountTotal float64 `json:"amount_total" gorm:"type:real;not null;default:0"` // USD quota total
	AmountUsed  float64 `json:"amount_used" gorm:"type:real;not null;default:0"` // USD quota used
	StartsAt    int64  `json:"starts_at" gorm:"bigint"`
	ExpiresAt   int64  `json:"expires_at" gorm:"bigint;index"`
	Status      string `json:"status" gorm:"type:varchar(16);index;default:'active'"` // active/expired/cancelled
	Source      string `json:"source" gorm:"type:varchar(16);default:'order'"`        // order/admin
	CreatedAt   int64  `json:"created_at" gorm:"bigint"`
}

func (UserSubscription) TableName() string { return "user_subscriptions" }
