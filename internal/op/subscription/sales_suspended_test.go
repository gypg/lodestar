package subscription

import (
	"context"
	"errors"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/user"
)

// TestPaidEntryPointsRefuseWhileQuotaPoolUnwired 钉死停售。
//
// 为什么停：UserSubscription 有 AmountTotal/AmountUsed，设计意图是"套餐授予一个 USD
// 额度池、请求从池里扣"。但 AmountUsed 全仓库只在创建时被写成 0（三处都在
// subscription.go），没有任何地方递增它，因为 relay 从不读 UserSubscription ——
// internal/relay 只对 User.Quota 计费。于是买套餐 = 花掉余额、什么都不多得，
// 对客户严格劣于不买。
//
// 断言不止"返回了错误"：还要断言**余额分文未动、订阅行没建出来**。只断言错误的话，
// 一个"先扣钱再报错"的实现也能过 —— 那正是最坏的情况。
func TestPaidEntryPointsRefuseWhileQuotaPoolUnwired(t *testing.T) {
	initSubTestDB(t)
	ctx := context.Background()

	const startingBalance = 100.0
	u := model.User{Username: "buyer", Password: "x", Quota: startingBalance}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.SubscriptionPlan{Name: "pro", Price: 20, QuotaAmount: 50, DurationDays: 30, Enabled: true}
	if err := CreatePlan(&plan, ctx); err != nil {
		t.Fatal(err)
	}

	t.Run("PurchaseWithBalance", func(t *testing.T) {
		if err := PurchaseWithBalance(u.ID, plan.ID, ctx); !errors.Is(err, ErrSalesSuspended) {
			t.Fatalf("error = %v, want ErrSalesSuspended", err)
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
		if err := CompleteOrder(order.TradeNo, ctx); !errors.Is(err, ErrSalesSuspended) {
			t.Fatalf("error = %v, want ErrSalesSuspended", err)
		}
		assertNothingCharged(t, ctx, u.ID, startingBalance)
	})

	// 管理员授予刻意不受影响：它不收钱，所以授予一个（暂时无效的）订阅不会少收谁的钱。
	// 若这条开始失败，说明拦截拦错了地方。
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

// withQuotaPoolWired 临时打开 subscriptionQuotaPoolWired，供覆盖购买**实现**的测试使用
// （可负担性守卫、并发不超卖 —— 后者是 BUG-002 的回归防线）。停售是产品决定，
// 那些不变量在接线之后仍然必须成立，所以测试不能因为停售而删掉。
//
// 只有测试写这个变量；生产代码从不写。
func withQuotaPoolWired(t *testing.T) {
	t.Helper()
	prev := subscriptionQuotaPoolWired
	subscriptionQuotaPoolWired = true
	t.Cleanup(func() { subscriptionQuotaPoolWired = prev })
}
