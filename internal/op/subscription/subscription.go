package subscription

/*
Lodestar — subscription business logic.

Ported from GGGZERO's subscription model/controller layer, simplified:
- No Stripe/Creem/Waffo payment providers (balance-only for now)
- No upgrade_group / user group change logic
- No quota reset period (can add later)
- Core lifecycle: plan CRUD, order creation/completion, balance purchase,
  subscription expiry (background job), admin bind
*/

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"

	"gorm.io/gorm"
)

var (
	ErrPlanNotFound        = errors.New("subscription plan not found")
	ErrPlanDisabled        = errors.New("subscription plan is disabled")
	ErrOrderNotFound       = errors.New("subscription order not found")
	ErrOrderStatusInvalid  = errors.New("subscription order status invalid")
	ErrInsufficientBalance = errors.New("insufficient balance")

	// ErrSalesSuspended blocks the two paid entry points into a subscription
	// (CompleteOrder, PurchaseWithBalance) because a purchased plan currently
	// grants the buyer nothing.
	//
	// UserSubscription carries AmountTotal/AmountUsed: the design is that a plan
	// grants a USD quota pool that requests draw down. But AmountUsed is only ever
	// written as 0 at creation (all three sites are in this file) and nothing ever
	// increments it, because the relay never reads UserSubscription — internal/relay
	// bills exclusively against User.Quota. So buying a plan spends balance and
	// changes nothing else, which is strictly worse for the customer than not buying.
	//
	// This is a code-level block rather than a setting so that sales cannot be
	// switched back on before the pool is actually consumed by billing. Remove it in
	// the same change that wires the pool into the relay's charge path.
	// AdminBindSubscription is deliberately NOT blocked: it takes no money, so an
	// admin granting a (currently inert) subscription cannot short-change anyone.
	ErrSalesSuspended = errors.New("subscription sales are suspended: a purchased plan does not yet grant usage quota")
)

// subscriptionQuotaPoolWired reports whether a purchased plan's AmountTotal pool
// is actually drawn down by request billing.
//
// While false, the two paid entry points refuse (see ErrSalesSuspended) so nobody
// can buy a plan that grants nothing. Flip it to true in the same change that
// wires UserSubscription.AmountUsed into the relay's charge path, then delete this
// variable together with the two guards — a flag that is never false is noise.
//
// It is a var rather than a const only so that the tests covering the purchase
// implementation (affordability guard, concurrent no-oversell) can keep running
// against it; production never writes it. See withQuotaPoolWired in the tests.
var subscriptionQuotaPoolWired = false

// --- Plan CRUD ---

// ListPlans returns all enabled subscription plans, ordered by sort_order.
func ListPlans(ctx context.Context) ([]model.SubscriptionPlan, error) {
	var plans []model.SubscriptionPlan
	err := db.GetDB().WithContext(ctx).
		Where("enabled = ?", true).
		Order("sort_order DESC, id DESC").
		Find(&plans).Error
	return plans, err
}

// ListAllPlans returns all subscription plans (including disabled) for admin.
func ListAllPlans(ctx context.Context) ([]model.SubscriptionPlan, error) {
	var plans []model.SubscriptionPlan
	err := db.GetDB().WithContext(ctx).
		Order("sort_order DESC, id DESC").
		Find(&plans).Error
	return plans, err
}

// GetPlan returns a plan by ID.
func GetPlan(id int, ctx context.Context) (*model.SubscriptionPlan, error) {
	var plan model.SubscriptionPlan
	if err := db.GetDB().WithContext(ctx).First(&plan, id).Error; err != nil {
		return nil, ErrPlanNotFound
	}
	return &plan, nil
}

// CreatePlan inserts a new plan.
func CreatePlan(plan *model.SubscriptionPlan, ctx context.Context) error {
	now := time.Now().Unix()
	plan.CreatedAt = now
	plan.UpdatedAt = now
	return db.GetDB().WithContext(ctx).Create(plan).Error
}

// UpdatePlan updates an existing plan by ID using a map to preserve zero values.
func UpdatePlan(id int, updates map[string]any, ctx context.Context) error {
	updates["updated_at"] = time.Now().Unix()
	res := db.GetDB().WithContext(ctx).
		Model(&model.SubscriptionPlan{}).
		Where("id = ?", id).
		Updates(updates)
	if res.RowsAffected == 0 {
		return ErrPlanNotFound
	}
	return res.Error
}

// DeletePlan hard-deletes a plan by ID.
func DeletePlan(id int, ctx context.Context) error {
	res := db.GetDB().WithContext(ctx).
		Where("id = ?", id).
		Delete(&model.SubscriptionPlan{})
	if res.RowsAffected == 0 {
		return ErrPlanNotFound
	}
	return res.Error
}

// --- Order lifecycle ---

func genTradeNo(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s%d%x", prefix, time.Now().UnixNano(), b)
}

// CreateOrder creates a pending subscription order for the given user and plan.
func CreateOrder(userID uint, planID int, method string, ctx context.Context) (*model.SubscriptionOrder, error) {
	plan, err := GetPlan(planID, ctx)
	if err != nil {
		return nil, err
	}
	if !plan.Enabled {
		return nil, ErrPlanDisabled
	}
	order := &model.SubscriptionOrder{
		UserID:        userID,
		PlanID:        plan.ID,
		TradeNo:       genTradeNo("SUB"),
		Money:         plan.Price,
		PaymentMethod: method,
		Status:        model.SubOrderStatusPending,
		CreatedAt:     time.Now().Unix(),
	}
	if err := db.GetDB().WithContext(ctx).Create(order).Error; err != nil {
		return nil, err
	}
	return order, nil
}

// CompleteOrder idempotently completes a pending order and creates a UserSubscription.
func CompleteOrder(tradeNo string, ctx context.Context) error {
	if !subscriptionQuotaPoolWired {
		return ErrSalesSuspended
	}
	if tradeNo == "" {
		return errors.New("trade_no is empty")
	}
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var order model.SubscriptionOrder
		if err := tx.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
			return ErrOrderNotFound
		}
		// Idempotent: already completed
		if order.Status == model.SubOrderStatusSuccess {
			return nil
		}
		if order.Status != model.SubOrderStatusPending {
			return ErrOrderStatusInvalid
		}

		var plan model.SubscriptionPlan
		if err := tx.First(&plan, order.PlanID).Error; err != nil {
			return ErrPlanNotFound
		}

		now := time.Now()
		endTime := calcEndTime(now, &plan)
		sub := &model.UserSubscription{
			UserID:      order.UserID,
			PlanID:      order.PlanID,
			OrderID:     order.ID,
			AmountTotal: plan.QuotaAmount,
			AmountUsed:  0,
			StartsAt:    now.Unix(),
			ExpiresAt:   endTime,
			Status:      model.SubStatusActive,
			Source:      "order",
			CreatedAt:   now.Unix(),
		}
		if err := tx.Create(sub).Error; err != nil {
			return err
		}

		nowUnix := time.Now().Unix()
		order.Status = model.SubOrderStatusSuccess
		order.CompletedAt = nowUnix
		return tx.Save(&order).Error
	})
}

// --- Balance purchase ---

// PurchaseWithBalance deducts the plan price from the user's balance and creates
// a completed order + active subscription in a single transaction.
func PurchaseWithBalance(userID uint, planID int, ctx context.Context) error {
	if !subscriptionQuotaPoolWired {
		return ErrSalesSuspended
	}
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var plan model.SubscriptionPlan
		if err := tx.First(&plan, planID).Error; err != nil {
			return ErrPlanNotFound
		}
		if !plan.Enabled {
			return ErrPlanDisabled
		}

		// Deduct balance with an atomic WHERE guard (quota >= price). The
		// WHERE guard, not a read-then-check, is what makes concurrent
		// purchases safe: two requests for price 8 on a balance of 10 can
		// only one succeed, so balance never goes negative. RowsAffected
		// being 0 means insufficient balance (or zero-price plans, which
		// skip the deduction entirely below).
		if plan.Price > 0 {
			res := tx.Model(&model.User{}).
				Where("id = ? AND quota >= ?", userID, plan.Price).
				Update("quota", gorm.Expr("quota - ?", plan.Price))
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return ErrInsufficientBalance
			}
		}

		now := time.Now()
		nowUnix := now.Unix()
		tradeNo := genTradeNo("SUBBAL")

		// Create completed order
		order := &model.SubscriptionOrder{
			UserID:        userID,
			PlanID:        plan.ID,
			TradeNo:       tradeNo,
			Money:         plan.Price,
			PaymentMethod: "balance",
			Status:        model.SubOrderStatusSuccess,
			CreatedAt:     nowUnix,
			CompletedAt:   nowUnix,
		}
		if err := tx.Create(order).Error; err != nil {
			return err
		}

		// Create subscription
		endTime := calcEndTime(now, &plan)
		sub := &model.UserSubscription{
			UserID:      userID,
			PlanID:      plan.ID,
			OrderID:     order.ID,
			AmountTotal: plan.QuotaAmount,
			AmountUsed:  0,
			StartsAt:    nowUnix,
			ExpiresAt:   endTime,
			Status:      model.SubStatusActive,
			Source:      "order",
			CreatedAt:   nowUnix,
		}
		return tx.Create(sub).Error
	})
}

// --- User subscription queries ---

// GetUserSubscription returns the user's most recent active subscription.
func GetUserSubscription(userID uint, ctx context.Context) (*model.UserSubscription, error) {
	now := time.Now().Unix()
	var sub model.UserSubscription
	err := db.GetDB().WithContext(ctx).
		Where("user_id = ? AND status = ? AND expires_at > ?", userID, model.SubStatusActive, now).
		Order("expires_at DESC, id DESC").
		First(&sub).Error
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// ListUserSubscriptions returns all subscriptions for a user (active and expired).
func ListUserSubscriptions(userID uint, ctx context.Context) ([]model.UserSubscription, error) {
	var subs []model.UserSubscription
	err := db.GetDB().WithContext(ctx).
		Where("user_id = ?", userID).
		Order("expires_at DESC, id DESC").
		Find(&subs).Error
	return subs, err
}

// ListAllUserSubscriptions returns all subscriptions across all users (admin).
func ListAllUserSubscriptions(ctx context.Context) ([]model.UserSubscription, error) {
	var subs []model.UserSubscription
	err := db.GetDB().WithContext(ctx).
		Order("id DESC").
		Limit(500).
		Find(&subs).Error
	return subs, err
}

// --- Admin operations ---

// AdminBindSubscription creates a subscription for a user without payment.
func AdminBindSubscription(userID uint, planID int, ctx context.Context) error {
	plan, err := GetPlan(planID, ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	endTime := calcEndTime(now, plan)
	sub := &model.UserSubscription{
		UserID:      userID,
		PlanID:      plan.ID,
		AmountTotal: plan.QuotaAmount,
		AmountUsed:  0,
		StartsAt:    now.Unix(),
		ExpiresAt:   endTime,
		Status:      model.SubStatusActive,
		Source:      "admin",
		CreatedAt:   now.Unix(),
	}
	return db.GetDB().WithContext(ctx).Create(sub).Error
}

// --- Background jobs ---

// ExpireDueSubscriptions marks expired subscriptions in batches.
// Returns the number of subscriptions expired. Intended for periodic background invocation.
func ExpireDueSubscriptions(ctx context.Context) (int, error) {
	now := time.Now().Unix()
	res := db.GetDB().WithContext(ctx).
		Model(&model.UserSubscription{}).
		Where("status = ? AND expires_at > 0 AND expires_at <= ?", model.SubStatusActive, now).
		Update("status", model.SubStatusExpired)
	return int(res.RowsAffected), res.Error
}

// --- Helpers ---

func calcEndTime(start time.Time, plan *model.SubscriptionPlan) int64 {
	switch plan.DurationType {
	case model.SubDurationMonth:
		months := plan.DurationDays / 30
		if months < 1 {
			months = 1
		}
		return start.AddDate(0, months, 0).Unix()
	case model.SubDurationDay:
		return start.Add(time.Duration(plan.DurationDays) * 24 * time.Hour).Unix()
	case model.SubDurationHour:
		hours := plan.DurationDays
		if hours < 1 {
			hours = 1
		}
		return start.Add(time.Duration(hours) * time.Hour).Unix()
	case model.SubDurationCustom:
		if plan.CustomDurationS > 0 {
			return start.Add(time.Duration(plan.CustomDurationS) * time.Second).Unix()
		}
		// fallback to days
		return start.Add(time.Duration(plan.DurationDays) * 24 * time.Hour).Unix()
	default:
		return start.Add(time.Duration(plan.DurationDays) * 24 * time.Hour).Unix()
	}
}
