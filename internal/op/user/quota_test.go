package user

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

func initQuotaTestDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// QuotaLedger：WO-017 起入账走漏斗，漏斗在同一事务里写流水行，缺表会直接报错。
	if err := db.GetDB().AutoMigrate(&model.User{}, &model.QuotaLedger{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}

// TestSettleUsage_overspendBecomesDebt 钉死结算的核心语义：**已交付的用量一定记账**，
// 即使超出余额 —— 余额允许为负。
//
// 这条测试取代了旧的 TestDeductQuota_neverNegative。旧契约（WHERE quota >= amount，
// 不够就整笔丢弃）读起来像是在保护用户不透支，实际后果相反：上游的钱我们已经付了，
// 却既不扣余额也不涨 used_quota，闸门（remaining > 0）于是继续放行，账户在余额跌破
// 单次成本后可以无限白嫖。拦截的职责归闸门，不归结算。
func TestSettleUsage_overspendBecomesDebt(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "u1", Password: "x", Quota: 1.0, UsedQuota: 0}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	// 第一次结算在余额内。
	if err := SettleUsage(u.ID, 0.6, ctx); err != nil {
		t.Fatal(err)
	}
	rem, used, err := GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(rem-0.4) > 1e-9 || math.Abs(used-0.6) > 1e-9 {
		t.Fatalf("after first settle: quota=%v used=%v, want 0.4 / 0.6", rem, used)
	}

	// 第二次超出剩余（0.4 < 0.6）：必须记成欠款，不得丢弃。
	if err := SettleUsage(u.ID, 0.6, ctx); err != nil {
		t.Fatalf("settle must not fail on overspend: %v", err)
	}
	rem, used, err = GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(rem-(-0.2)) > 1e-9 {
		t.Fatalf("quota = %v, want -0.2 (delivered usage must be owed, not forgiven)", rem)
	}
	if math.Abs(used-1.2) > 1e-9 {
		t.Fatalf("used_quota = %v, want 1.2 (revenue reporting must see both charges)", used)
	}

	// 充值净掉欠款。WO-017 起入账走漏斗（AddQuota 已删）。
	if err := MutateQuota(nil, u.ID, 1.0, LedgerEntry{Kind: model.LedgerKindTopupEpay}, ctx); err != nil {
		t.Fatal(err)
	}
	rem, _, err = GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(rem-0.8) > 1e-9 {
		t.Fatalf("quota after top-up = %v, want 0.8 (debt nets off)", rem)
	}
}

// TestSettleUsage_missingUserIsReported 结算目标不存在必须报错，不能静默成功 ——
// 静默会让"用量已交付但没人付钱"这件事无迹可寻。
func TestSettleUsage_missingUserIsReported(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()

	err := SettleUsage(4242, 1.0, ctx)
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("SettleUsage on a missing user = %v, want ErrUserNotFound", err)
	}
}
