package subscription

import (
	"context"
	"errors"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/user"
)

// 这个文件取代了原先钉死「订阅全面停售」的 sales_suspended_test.go。
//
// 停售的理由是：UserSubscription 有 AmountTotal/AmountUsed，设计意图是"套餐授予一个
// USD 额度池、请求从池里扣"，但 AmountUsed 从来只在创建时被写成 0，没有任何地方递增，
// 因为 relay 只对 User.Quota 计费。于是买套餐 = 花掉余额、什么都不多得。
//
// 现在池子已经接线（pool.go + billing.ChargeKey），所以停售解除。但"不能卖不给东西的
// 套餐"这条不变量必须留下，只是判据从"全部停售"收窄成"QuotaAmount <= 0 的套餐停售"。

// TestPurchaseRefusedWhenPlanGrantsNoQuota 钉死收窄后的拦截。
//
// 断言不止"返回了错误"：还要断言**余额分文未动、订阅行没建出来**。只断言错误的话，
// 一个"先扣钱再报错"的实现也能过 —— 那正是最坏的情况。
func TestPurchaseRefusedWhenPlanGrantsNoQuota(t *testing.T) {
	initSubTestDB(t)
	ctx := context.Background()

	const startingBalance = 100.0
	u := model.User{Username: "buyer", Password: "x", Quota: startingBalance}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	// QuotaAmount 0：套餐不授予任何额度池。
	plan := model.SubscriptionPlan{Name: "empty", Price: 20, QuotaAmount: 0, DurationDays: 30, Enabled: true}
	if err := CreatePlan(&plan, ctx); err != nil {
		t.Fatal(err)
	}

	t.Run("PurchaseWithBalance", func(t *testing.T) {
		if err := PurchaseWithBalance(u.ID, plan.ID, ctx); !errors.Is(err, ErrPlanGrantsNoQuota) {
			t.Fatalf("error = %v, want ErrPlanGrantsNoQuota", err)
		}
		assertNothingCharged(t, ctx, u.ID, startingBalance)
	})

	t.Run("CompleteOrder", func(t *testing.T) {
		// 先造一张 pending 单，证明拦截发生在"把单据兑换成订阅"之前，
		// 而不是仅仅因为找不到单据才失败。
		order := model.SubscriptionOrder{
			UserID: u.ID, PlanID: plan.ID, TradeNo: "SUBTEST-1",
			Money: plan.Price, PaymentMethod: "epay", Status: model.SubOrderStatusPending,
		}
		if err := db.GetDB().Create(&order).Error; err != nil {
			t.Fatal(err)
		}
		if err := CompleteOrder(order.TradeNo, ctx); !errors.Is(err, ErrPlanGrantsNoQuota) {
			t.Fatalf("error = %v, want ErrPlanGrantsNoQuota", err)
		}
		assertNothingCharged(t, ctx, u.ID, startingBalance)

		// 单据必须仍是 pending：拒绝不能把它标成 success，否则支付回调重试时
		// 会被幂等分支当成"已完成"而永远不再建订阅，钱收了东西没给。
		var after model.SubscriptionOrder
		if err := db.GetDB().First(&after, order.ID).Error; err != nil {
			t.Fatal(err)
		}
		if after.Status != model.SubOrderStatusPending {
			t.Errorf("order status = %q, want %q (a refusal must not consume the order)",
				after.Status, model.SubOrderStatusPending)
		}
	})

	// 管理员授予刻意不受影响：它不收钱，所以授予一个没有额度池的订阅不会少收谁的钱。
	t.Run("AdminBindSubscription stays available", func(t *testing.T) {
		if err := AdminBindSubscription(u.ID, plan.ID, ctx); err != nil {
			t.Fatalf("AdminBindSubscription must remain usable, got %v", err)
		}
		var count int64
		if err := db.GetDB().Model(&model.UserSubscription{}).Where("user_id = ?", u.ID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("admin-bound subscription count = %d, want 1", count)
		}
		assertNothingCharged(t, ctx, u.ID, startingBalance)
	})
}

// TestPurchaseGrantsConsumablePool 是停售解除的正面证据：一个真的授予额度的套餐
// 现在可以买，并且买到的订阅**能被扣**。
//
// 这条是"卖了不给东西"那个缺陷的直接回归防线：只断言购买成功是不够的，
// 必须证明买到的池子真的能出账。
func TestPurchaseGrantsConsumablePool(t *testing.T) {
	initSubTestDB(t)
	ctx := context.Background()

	u := model.User{Username: "buyer2", Password: "x", Quota: 100}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.SubscriptionPlan{Name: "pro", Price: 20, QuotaAmount: 50, DurationDays: 30, Enabled: true}
	if err := CreatePlan(&plan, ctx); err != nil {
		t.Fatal(err)
	}

	if err := PurchaseWithBalance(u.ID, plan.ID, ctx); err != nil {
		t.Fatalf("PurchaseWithBalance: %v", err)
	}

	rem, _, err := user.GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rem != 80 {
		t.Fatalf("balance after purchase = %v, want 80 (100 - price 20)", rem)
	}

	// 买到的池子必须是 plan.QuotaAmount，且立刻可用。
	pool, err := PoolRemaining(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pool != 50 {
		t.Fatalf("pool remaining = %v, want 50 (plan quota_amount)", pool)
	}

	drawn, err := DrawFromPool(u.ID, 12.5, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if drawn != 12.5 {
		t.Fatalf("drawn = %v, want 12.5 — a purchased pool that cannot be drawn is the defect this guards", drawn)
	}
	if pool, err = PoolRemaining(u.ID, ctx); err != nil || pool != 37.5 {
		t.Fatalf("pool remaining after draw = %v, %v; want 37.5, nil", pool, err)
	}

	// 池子扣款不动钱包：两套账各自独立（见 pool.go 顶部说明）。
	rem2, used, err := user.GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rem2 != 80 {
		t.Errorf("balance = %v, want 80 (a pool-funded draw must not touch the wallet)", rem2)
	}
	if used != 0 {
		t.Errorf("used_quota = %v, want 0 (pool spend is tracked in amount_used, not used_quota)", used)
	}
}

// assertNothingCharged 断言余额未动，且没有任何**付费产生的**订阅行。
func assertNothingCharged(t *testing.T, ctx context.Context, userID uint, want float64) {
	t.Helper()

	rem, used, err := user.GetQuota(userID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rem != want {
		t.Errorf("balance = %v, want %v (a refused sale must not touch the balance)", rem, want)
	}
	if used != 0 {
		t.Errorf("used_quota = %v, want 0", used)
	}

	var paid int64
	if err := db.GetDB().Model(&model.UserSubscription{}).
		Where("user_id = ? AND source = ?", userID, "order").Count(&paid).Error; err != nil {
		t.Fatal(err)
	}
	if paid != 0 {
		t.Errorf("purchase-sourced subscription rows = %d, want 0", paid)
	}
}
