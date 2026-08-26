package user

/*
WO-017 门 B：对账不变式 + 漏斗守卫的行为测试。

核心断言是这条等式，它是整个流水表设计的地基：

	users.quota == Σ(quota_ledger.delta) - users.used_quota

期望值一律写死（quota == 45 这种），不用 `> 0` / `!= nil` 这类宽松断言 —— 宽松断言
在 100% 覆盖率下依然守不住任何东西。
*/

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"

	"gorm.io/gorm"
)

func initLedgerTestDB(t *testing.T) {
	t.Helper()
	dsn := "file:" + sanitizeDSN(t.Name()) + "?mode=memory&cache=shared"
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(&model.User{}, &model.QuotaLedger{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}

func sanitizeDSN(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch r {
		case '/', '\\', ' ':
			out = append(out, '-')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

// mustUser 建一个测试用户。label 用来区分同一个测试里的多个用户 —— users.username
// 上有唯一索引，光用 t.Name() 做用户名在建第二个用户时会撞约束。
func mustUser(t *testing.T, label string, quota, used float64) model.User {
	t.Helper()
	u := model.User{Username: "led-" + label + "-" + t.Name(), Password: "x", Quota: quota, UsedQuota: used}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatalf("create user %q: %v", label, err)
	}
	return u
}

// readBalance 直接从库里读，不走缓存 —— 断言必须看落库的值。
func readBalance(t *testing.T, userID uint) (quota, used float64) {
	t.Helper()
	var u model.User
	if err := db.GetDB().Select("quota", "used_quota").First(&u, userID).Error; err != nil {
		t.Fatalf("read user %d: %v", userID, err)
	}
	return u.Quota, u.UsedQuota
}

// TestGateB_reconciliationInvariantHolds 跑一条完整的事件序列，钉死每一步之后的余额
// 与最终的对账等式。
//
//	充值 +100 → 订阅购买 −30 → 管理员纠错 −5 → 用量结算 20
//	quota      100        70              65        45
//	used_quota   0          0               0        20
//	Σdelta     100         70              65        65
func TestGateB_reconciliationInvariantHolds(t *testing.T) {
	initLedgerTestDB(t)
	ctx := context.Background()
	u := mustUser(t, "u", 0, 0)

	if err := MutateQuota(nil, u.ID, 100, LedgerEntry{
		Kind:    model.LedgerKindTopupEpay,
		RefType: model.LedgerRefPaymentOrder,
		RefID:   "T-1",
	}, ctx); err != nil {
		t.Fatalf("topup: %v", err)
	}
	if q, _ := readBalance(t, u.ID); q != 100 {
		t.Fatalf("after topup quota = %.17g, want 100", q)
	}

	if err := MutateQuota(nil, u.ID, -30, LedgerEntry{
		Kind:              model.LedgerKindSubscriptionPurchase,
		RefType:           model.LedgerRefSubscriptionPlan,
		RefID:             "7",
		RequireAffordable: true,
	}, ctx); err != nil {
		t.Fatalf("purchase: %v", err)
	}
	if q, _ := readBalance(t, u.ID); q != 70 {
		t.Fatalf("after purchase quota = %.17g, want 70", q)
	}

	if err := MutateQuota(nil, u.ID, -5, LedgerEntry{
		Kind:    model.LedgerKindAdminAdjust,
		ActorID: 999,
		Reason:  "重复到账，收回 5",
	}, ctx); err != nil {
		t.Fatalf("admin adjust: %v", err)
	}
	if q, _ := readBalance(t, u.ID); q != 65 {
		t.Fatalf("after admin adjust quota = %.17g, want 65", q)
	}

	// 用量结算刻意不走漏斗（热路径），但必须仍然满足不变式。
	if err := SettleUsage(u.ID, 20, ctx); err != nil {
		t.Fatalf("settle: %v", err)
	}

	quota, used := readBalance(t, u.ID)
	sum, err := ledgerSum(u.ID, ctx)
	if err != nil {
		t.Fatalf("ledger sum: %v", err)
	}

	// 全部为整数且远小于 2^53，float64 下这些运算是精确的，可以用等号。
	if quota != 45 {
		t.Fatalf("quota = %.17g, want 45", quota)
	}
	if used != 20 {
		t.Fatalf("used_quota = %.17g, want 20", used)
	}
	if sum != 65 {
		t.Fatalf("ledger sum = %.17g, want 65", sum)
	}
	if got := sum - used; got != quota {
		t.Fatalf("不变式破了：Σdelta - used_quota = %.17g，quota = %.17g", got, quota)
	}

	// 流水行数与分类也要钉死 —— 只断言合计的话，把三笔记成一笔也能通过。
	var kinds []string
	if err := db.GetDB().Model(&model.QuotaLedger{}).
		Where("user_id = ?", u.ID).Order("id").Pluck("kind", &kinds).Error; err != nil {
		t.Fatalf("pluck kinds: %v", err)
	}
	want := []string{
		model.LedgerKindTopupEpay,
		model.LedgerKindSubscriptionPurchase,
		model.LedgerKindAdminAdjust,
	}
	if len(kinds) != len(want) {
		t.Fatalf("流水行数 = %d，want %d（%v）", len(kinds), len(want), kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("流水[%d].kind = %q，want %q", i, kinds[i], want[i])
		}
	}
}

// TestMutateQuota_rejectsNonFinite 钉死非有限值在漏斗处被拦掉。
// 放过去的后果是 `quota + NaN` 永久毒化该列：之后每个 remaining > 0 都是 false，
// 账户锁死且任何充值都无法在算术上修复。
func TestMutateQuota_rejectsNonFinite(t *testing.T) {
	initLedgerTestDB(t)
	ctx := context.Background()
	u := mustUser(t, "u", 10, 0)

	for _, bad := range []float64{
		math.NaN(),
		math.Inf(1),
		math.Inf(-1),
	} {
		err := MutateQuota(nil, u.ID, bad, LedgerEntry{Kind: model.LedgerKindAdminAdjust}, ctx)
		if !errors.Is(err, ErrNonFiniteAmount) {
			t.Fatalf("MutateQuota(%v) err = %v, want ErrNonFiniteAmount", bad, err)
		}
	}

	// 余额分毫未动，且没有留下任何流水行。
	if q, _ := readBalance(t, u.ID); q != 10 {
		t.Fatalf("quota = %.17g, want 10（非有限值不得改动余额）", q)
	}
	var n int64
	if err := db.GetDB().Model(&model.QuotaLedger{}).Where("user_id = ?", u.ID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("流水行数 = %d, want 0", n)
	}
}

// TestMutateQuota_affordableGuardBoundary 钉死 CAS 守卫的边界：余额恰好等于价格
// 必须成功（`>=` 不是 `>`），差一分钱必须失败且分毫不动。
func TestMutateQuota_affordableGuardBoundary(t *testing.T) {
	initLedgerTestDB(t)
	ctx := context.Background()

	// 恰好够：10 买 10。
	u1 := mustUser(t, "u", 10, 0)
	if err := MutateQuota(nil, u1.ID, -10, LedgerEntry{
		Kind:              model.LedgerKindSubscriptionPurchase,
		RequireAffordable: true,
	}, ctx); err != nil {
		t.Fatalf("余额恰好等于价格时应成功，got %v", err)
	}
	if q, _ := readBalance(t, u1.ID); q != 0 {
		t.Fatalf("quota = %.17g, want 0", q)
	}

	// 差一点：9.99 买 10。
	u2 := mustUser(t, "short", 9.99, 0)
	err := MutateQuota(nil, u2.ID, -10, LedgerEntry{
		Kind:              model.LedgerKindSubscriptionPurchase,
		RequireAffordable: true,
	}, ctx)
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("余额不足时 err = %v, want ErrInsufficientBalance", err)
	}
	if q, _ := readBalance(t, u2.ID); q != 9.99 {
		t.Fatalf("quota = %.17g, want 9.99（失败的扣款不得改动余额）", q)
	}
	var n int64
	if err := db.GetDB().Model(&model.QuotaLedger{}).Where("user_id = ?", u2.ID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("失败的扣款留下了 %d 行流水, want 0", n)
	}
}

// TestMutateQuota_adminAdjustMayGoNegative 管理员纠错不带 CAS 守卫：必须允许把余额
// 压到负数。收回一笔已被消耗掉的错误入账就是这个场景 —— 若被守卫拒绝，错账无法平。
func TestMutateQuota_adminAdjustMayGoNegative(t *testing.T) {
	initLedgerTestDB(t)
	ctx := context.Background()
	u := mustUser(t, "u", 3, 0)

	if err := MutateQuota(nil, u.ID, -10, LedgerEntry{
		Kind:    model.LedgerKindAdminAdjust,
		ActorID: 42,
		Reason:  "收回错误入账",
	}, ctx); err != nil {
		t.Fatalf("admin adjust: %v", err)
	}
	if q, _ := readBalance(t, u.ID); q != -7 {
		t.Fatalf("quota = %.17g, want -7", q)
	}
}

// TestMutateQuota_recordsActorNotBeneficiary 钉死 ActorID 存的是操作者。
// 填成受益人 ID 时审计完全失效 —— 查不出是谁动的手，而这正是本工单的目的。
func TestMutateQuota_recordsActorNotBeneficiary(t *testing.T) {
	initLedgerTestDB(t)
	ctx := context.Background()
	u := mustUser(t, "u", 0, 0)
	const adminID = 7777

	if err := MutateQuota(nil, u.ID, 50, LedgerEntry{
		Kind:    model.LedgerKindAdminAdjust,
		ActorID: adminID,
		Reason:  "补偿",
	}, ctx); err != nil {
		t.Fatalf("admin adjust: %v", err)
	}

	var entry model.QuotaLedger
	if err := db.GetDB().Where("user_id = ?", u.ID).First(&entry).Error; err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if entry.ActorID != adminID {
		t.Fatalf("ActorID = %d, want %d（操作者，不是受益人 %d）", entry.ActorID, adminID, u.ID)
	}
	if entry.UserID != u.ID {
		t.Fatalf("UserID = %d, want %d（受益人）", entry.UserID, u.ID)
	}
	if entry.Reason != "补偿" {
		t.Fatalf("Reason = %q, want %q", entry.Reason, "补偿")
	}
	if entry.Delta != 50 {
		t.Fatalf("Delta = %.17g, want 50（有符号，不许取绝对值）", entry.Delta)
	}
}

// TestMutateQuota_requiresKind 空 kind 必须被拒：一条无法归类的流水在对账时等于没记。
func TestMutateQuota_requiresKind(t *testing.T) {
	initLedgerTestDB(t)
	ctx := context.Background()
	u := mustUser(t, "u", 10, 0)

	err := MutateQuota(nil, u.ID, 5, LedgerEntry{Reason: "忘了填 kind"}, ctx)
	if !errors.Is(err, ErrMissingLedgerKind) {
		t.Fatalf("err = %v, want ErrMissingLedgerKind", err)
	}
	if q, _ := readBalance(t, u.ID); q != 10 {
		t.Fatalf("quota = %.17g, want 10", q)
	}
}

// TestMutateQuota_zeroDeltaIsNoop delta 为 0 时不改余额也不留流水。
func TestMutateQuota_zeroDeltaIsNoop(t *testing.T) {
	initLedgerTestDB(t)
	ctx := context.Background()
	u := mustUser(t, "u", 10, 0)

	if err := MutateQuota(nil, u.ID, 0, LedgerEntry{Kind: model.LedgerKindAdminAdjust}, ctx); err != nil {
		t.Fatalf("zero delta: %v", err)
	}
	var n int64
	if err := db.GetDB().Model(&model.QuotaLedger{}).Where("user_id = ?", u.ID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("流水行数 = %d, want 0", n)
	}
}

// TestMutateQuota_missingUser 用户行不存在时报 ErrUserNotFound，且不留孤儿流水。
func TestMutateQuota_missingUser(t *testing.T) {
	initLedgerTestDB(t)
	ctx := context.Background()

	err := MutateQuota(nil, 424242, 10, LedgerEntry{Kind: model.LedgerKindTopupEpay}, ctx)
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
	var n int64
	if err := db.GetDB().Model(&model.QuotaLedger{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("给不存在的用户留下了 %d 行流水, want 0", n)
	}
}

// TestMutateQuota_ledgerAndBalanceShareTransaction 钉死"同事务"：调用方事务回滚时，
// 余额与流水必须一起消失。拆开的后果是"钱到账了但无痕"或"有痕但钱没到"。
func TestMutateQuota_ledgerAndBalanceShareTransaction(t *testing.T) {
	initLedgerTestDB(t)
	ctx := context.Background()
	u := mustUser(t, "u", 10, 0)

	boom := errors.New("调用方在入账之后失败")
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		if mErr := MutateQuota(tx, u.ID, 100, LedgerEntry{
			Kind:    model.LedgerKindTopupEpay,
			RefType: model.LedgerRefPaymentOrder,
			RefID:   "T-rollback",
		}, ctx); mErr != nil {
			return mErr
		}
		// 模拟调用方在余额入账之后才失败（例如支付订单状态更新失败）。
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}

	if q, _ := readBalance(t, u.ID); q != 10 {
		t.Fatalf("quota = %.17g, want 10（事务回滚后余额必须复原）", q)
	}
	var n int64
	if err := db.GetDB().Model(&model.QuotaLedger{}).Where("user_id = ?", u.ID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("回滚后仍留下 %d 行流水, want 0", n)
	}
}
