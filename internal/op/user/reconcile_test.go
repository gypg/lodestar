package user

/*
WO-017 T8 — 只读对账。

漂移 = quota - (Σdelta - used_quota)。0 表示对上账。这里的断言都钉死数值，
并覆盖两个容易写错的点：
  - 容差：不能用 `drift != 0`（浮点噪声会把每个活跃用户都报成漂移），
    也不能大到把真实漂移吃掉。
  - LEFT JOIN：一行流水都没有的用户必须被报出来，INNER JOIN 会整个漏掉他们。
*/

import (
	"context"
	"math"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

func driftByUser(t *testing.T, drifts []QuotaDrift) map[uint]QuotaDrift {
	t.Helper()
	out := make(map[uint]QuotaDrift, len(drifts))
	for _, d := range drifts {
		out[d.UserID] = d
	}
	return out
}

// 走漏斗的用户全都对得上账 —— 对账接口返回空切片（不是 nil，调用方要序列化成 `[]`）。
func TestReconcileDrifts_cleanBooksReturnEmpty(t *testing.T) {
	initLedgerTestDB(t)
	ctx := context.Background()

	u := mustUser(t, "clean", 0, 0)
	if err := MutateQuota(nil, u.ID, 100, LedgerEntry{Kind: model.LedgerKindTopupEpay}, ctx); err != nil {
		t.Fatalf("topup: %v", err)
	}
	if err := MutateQuota(nil, u.ID, -30, LedgerEntry{
		Kind: model.LedgerKindSubscriptionPurchase, RequireAffordable: true,
	}, ctx); err != nil {
		t.Fatalf("purchase: %v", err)
	}
	// 用量结算刻意不进流水（quota -= 20 且 used_quota += 20），不变式仍闭合。
	if err := SettleUsage(u.ID, 20, ctx); err != nil {
		t.Fatalf("settle: %v", err)
	}

	drifts, err := ReconcileDrifts(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(drifts) != 0 {
		t.Fatalf("对上账却报了 %d 条漂移：%+v", len(drifts), drifts)
	}
	if drifts == nil {
		t.Error("返回 nil，want 空切片（nil 会被序列化成 JSON null 而不是 []）")
	}
}

// 绕过漏斗直接改余额 —— 正是流水表要防的那件事 —— 必须被报出来，且数值精确。
func TestReconcileDrifts_reportsBypassedWriteWithExactNumbers(t *testing.T) {
	initLedgerTestDB(t)
	ctx := context.Background()

	honest := mustUser(t, "honest", 0, 0)
	if err := MutateQuota(nil, honest.ID, 50, LedgerEntry{Kind: model.LedgerKindTopupEpay}, ctx); err != nil {
		t.Fatalf("topup: %v", err)
	}

	// 绕过漏斗：模拟"有人又写了一处裸 SQL"。刻意用 Raw SQL —— 门 A 抓不到这种写法，
	// 只有对账能发现。
	sneaky := mustUser(t, "sneaky", 0, 0)
	if err := db.GetDB().Exec("UPDATE users SET quota = 77 WHERE id = ?", sneaky.ID).Error; err != nil {
		t.Fatalf("bypass write: %v", err)
	}

	drifts, err := ReconcileDrifts(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	byUser := driftByUser(t, drifts)

	if _, reported := byUser[honest.ID]; reported {
		t.Errorf("走漏斗的用户被误报成漂移：%+v", byUser[honest.ID])
	}

	got, reported := byUser[sneaky.ID]
	if !reported {
		t.Fatalf("绕过漏斗的 +77 没被报出来（漂移共 %d 条：%+v）", len(drifts), drifts)
	}
	if math.Abs(got.Quota-77) > 1e-9 {
		t.Errorf("quota = %.17g, want 77", got.Quota)
	}
	if math.Abs(got.UsedQuota) > 1e-9 {
		t.Errorf("used_quota = %.17g, want 0", got.UsedQuota)
	}
	if math.Abs(got.LedgerSum) > 1e-9 {
		t.Errorf("ledger_sum = %.17g, want 0（这个用户一行流水都没有）", got.LedgerSum)
	}
	// drift = 77 - (0 - 0) = 77
	if math.Abs(got.Drift-77) > 1e-9 {
		t.Errorf("drift = %.17g, want 77", got.Drift)
	}
	if got.Username != sneaky.Username {
		t.Errorf("username = %q, want %q（报了 ID 却没名字，管理员没法处置）", got.Username, sneaky.Username)
	}
}

// 反向漂移（流水记了但余额没落地）也要报，且 drift 为负。
// 只测正向的话，把 drift 写成 ABS(...) 或调反减数都不会被发现。
func TestReconcileDrifts_reportsNegativeDrift(t *testing.T) {
	initLedgerTestDB(t)
	ctx := context.Background()

	u := mustUser(t, "phantom", 10, 0)
	// 只写流水行，不改余额：模拟"有痕但钱没到"。
	if err := db.GetDB().Create(&model.QuotaLedger{
		UserID: u.ID, Delta: 40, Kind: model.LedgerKindTopupStripe, CreatedAt: 1,
	}).Error; err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	drifts, err := ReconcileDrifts(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got, reported := driftByUser(t, drifts)[u.ID]
	if !reported {
		t.Fatalf("流水多出 40 却没报漂移：%+v", drifts)
	}
	// drift = 10 - (40 - 0) = -30
	if math.Abs(got.Drift-(-30)) > 1e-9 {
		t.Fatalf("drift = %.17g, want -30（负号丢了就分不清是多钱还是少钱）", got.Drift)
	}
	if math.Abs(got.LedgerSum-40) > 1e-9 {
		t.Errorf("ledger_sum = %.17g, want 40", got.LedgerSum)
	}
}

// 容差：浮点累加噪声不得被报成漂移，但刚过容差的真实漂移必须报。
// 这两条一起把容差钉在一个区间里 —— 改成 `> 0` 会让前半段红，
// 放大到 1e-6 会让后半段红。
func TestReconcileDrifts_toleranceIgnoresFloatNoiseButCatchesRealDrift(t *testing.T) {
	initLedgerTestDB(t)
	ctx := context.Background()

	// 噪声用户：0.1 加十次，浮点上不精确等于 1.0。
	noisy := mustUser(t, "noisy", 0, 0)
	for i := 0; i < 10; i++ {
		if err := MutateQuota(nil, noisy.ID, 0.1, LedgerEntry{Kind: model.LedgerKindTopupEpay}, ctx); err != nil {
			t.Fatalf("topup %d: %v", i, err)
		}
	}

	// 真实漂移用户：绕过漏斗多出 1e-7，比容差 1e-9 大两个数量级。
	real := mustUser(t, "real", 0, 0)
	if err := MutateQuota(nil, real.ID, 1, LedgerEntry{Kind: model.LedgerKindTopupEpay}, ctx); err != nil {
		t.Fatalf("topup: %v", err)
	}
	if err := db.GetDB().Exec(
		"UPDATE users SET quota = quota + 0.0000001 WHERE id = ?", real.ID,
	).Error; err != nil {
		t.Fatalf("bypass write: %v", err)
	}

	drifts, err := ReconcileDrifts(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	byUser := driftByUser(t, drifts)

	if d, reported := byUser[noisy.ID]; reported {
		t.Errorf("浮点噪声被报成漂移（drift=%.17g）—— 容差太小，真漂移会淹没在噪音里", d.Drift)
	}
	got, reported := byUser[real.ID]
	if !reported {
		t.Fatalf("1e-7 的真实漂移没被报出来 —— 容差太大，吃掉了真漂移")
	}
	if math.Abs(got.Drift-1e-7) > 1e-12 {
		t.Errorf("drift = %.17g, want 1e-7", got.Drift)
	}
}

// 开账后存量用户即刻对上账 —— T7 与 T8 的接缝。
// 迁移本体的测试在 db/migrate/019_test.go；这里验的是"开账行确实能让对账归零"。
func TestReconcileDrifts_openingBalanceClosesLegacyGap(t *testing.T) {
	initLedgerTestDB(t)
	ctx := context.Background()

	// 存量用户：quota=45 / used_quota=20，一行流水都没有。
	legacy := mustUser(t, "legacy", 45, 20)

	drifts, err := ReconcileDrifts(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	before, reported := driftByUser(t, drifts)[legacy.ID]
	if !reported {
		t.Fatal("开账前存量用户必须被报成漂移")
	}
	// drift = 45 - (0 - 20) = 65
	if math.Abs(before.Drift-65) > 1e-9 {
		t.Fatalf("开账前 drift = %.17g, want 65", before.Drift)
	}

	// 补开账行：delta = quota + used_quota = 65（不是 45）。
	if err := db.GetDB().Create(&model.QuotaLedger{
		UserID: legacy.ID, Delta: 65, Kind: model.LedgerKindOpeningBalance, CreatedAt: 1,
	}).Error; err != nil {
		t.Fatalf("seed opening balance: %v", err)
	}

	after, err := ReconcileDrifts(ctx)
	if err != nil {
		t.Fatalf("reconcile after: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("开账后仍有 %d 条漂移：%+v", len(after), after)
	}
}
