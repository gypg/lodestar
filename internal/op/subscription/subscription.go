package subscription

/*
Lodestar — subscription business logic.

Ported from New API's subscription model/controller layer
(github.com/QuantumNous/new-api, AGPL-3.0 — see NOTICE.md). Simplified:
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
	"strconv"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/user"

	"gorm.io/gorm"
)

var (
	ErrPlanNotFound        = errors.New("subscription plan not found")
	ErrPlanDisabled        = errors.New("subscription plan is disabled")
	ErrOrderNotFound       = errors.New("subscription order not found")
	ErrOrderStatusInvalid  = errors.New("subscription order status invalid")
	ErrInsufficientBalance = errors.New("insufficient balance")

	// ErrPlanGrantsNoQuota refuses to SELL a plan whose quota pool is not
	// positive. It replaced a blanket sales suspension that existed while nothing
	// drew the pool down at all (see internal/op/subscription/pool.go, now wired
	// into internal/op/billing.ChargeKey).
	//
	// Two reasons a zero pool must not be sold rather than being treated as
	// "unlimited":
	//   - Taking money for a plan that grants nothing is the exact defect the
	//     suspension was introduced for.
	//   - Genuinely unlimited usage at a fixed price puts no ceiling on the
	//     upstream cost this gateway pays, so one subscriber can outspend their
	//     own subscription without limit.
	//
	// AdminBindSubscription is deliberately NOT subject to this: it takes no
	// money, so an admin may grant a pool-less subscription if they want to.
	ErrPlanGrantsNoQuota = errors.New("subscription plan grants no usage quota, so it cannot be sold")
)

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
	// Enabled 的 default:true 标签会让 create 回调把显式的 false 吞成 true
	// （停用套餐创建即上架可售）。Create 前快照意图，之后按回填的 ID 用
	// UPDATE 补写停用态（更新路径不受该替换影响），并把结构体恢复为真实意图。
	// EnabledSet 区分"字段缺失（默认启用）"与"显式 false（管理员要求下架）"：
	// 只有后者需要补偿，否则缺失会被误当成停用——把一个 bug 换成另一个 bug。
	explicitlyDisabled := plan.EnabledSet && !plan.Enabled
	if err := db.GetDB().WithContext(ctx).Create(plan).Error; err != nil {
		return err
	}
	if explicitlyDisabled {
		if err := db.GetDB().WithContext(ctx).Model(&model.SubscriptionPlan{}).
			Where("id = ?", plan.ID).
			Update("enabled", false).Error; err != nil {
			return err
		}
		plan.Enabled = false
	}
	return nil
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
		// A plan with no pool grants nothing, so it must not be turned into a
		// subscription off the back of a payment. Checked here and not only at
		// order creation because the plan may have been edited in between.
		if plan.QuotaAmount <= 0 {
			return ErrPlanGrantsNoQuota
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
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var plan model.SubscriptionPlan
		if err := tx.First(&plan, planID).Error; err != nil {
			return ErrPlanNotFound
		}
		if !plan.Enabled {
			return ErrPlanDisabled
		}
		// Refuse before touching the balance: a plan with no quota pool would take
		// the customer's money and grant them nothing.
		if plan.QuotaAmount <= 0 {
			return ErrPlanGrantsNoQuota
		}

		// Deduct balance with an atomic WHERE guard (quota >= price). The
		// WHERE guard, not a read-then-check, is what makes concurrent
		// purchases safe: two requests for price 8 on a balance of 10 can
		// only one succeed, so balance never goes negative.
		//
		// WO-017：扣款走 user.MutateQuota 漏斗（同事务写一条流水），
		// RequireAffordable 保留的正是上面那个原子 WHERE 守卫 —— 不许退化成
		// read-then-check。余额不足时漏斗返回 user.ErrInsufficientBalance，
		// 这里翻译成本包的同名哨兵以保持对外 API 不变。
		if plan.Price > 0 {
			err := user.MutateQuota(tx, userID, -plan.Price, user.LedgerEntry{
				Kind:              model.LedgerKindSubscriptionPurchase,
				RefType:           model.LedgerRefSubscriptionPlan,
				RefID:             strconv.Itoa(plan.ID),
				RequireAffordable: true,
			}, ctx)
			if errors.Is(err, user.ErrInsufficientBalance) {
				return ErrInsufficientBalance
			}
			if err != nil {
				return err
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

// ListActiveUserSubscriptions returns ALL of a user's currently active
// subscriptions, soonest expiry first (WO-030 defect B). GetUserSubscription
// keeps its DESC ordering for the single-subscription UI view; alerting needs
// the full list because a user can legitimately hold several active
// subscriptions (purchase / admin bind / payment paths all Create unconditionally,
// user_id is a plain index), and only the soonest one is about to lapse.
// Callers must not rely on this ordering alone - re-derive the soonest row.
func ListActiveUserSubscriptions(userID uint, ctx context.Context) ([]model.UserSubscription, error) {
	now := time.Now().Unix()
	var subs []model.UserSubscription
	err := db.GetDB().WithContext(ctx).
		Where("user_id = ? AND status = ? AND expires_at > ?", userID, model.SubStatusActive, now).
		Order("expires_at ASC, id ASC").
		Find(&subs).Error
	return subs, err
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
