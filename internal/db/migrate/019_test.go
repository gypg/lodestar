package migrate

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gypg/lodestar/internal/model"
	"gorm.io/gorm"
)

// WO-017 T7 — 开账。
//
// 不变式 `quota == Σ(ledger.delta) - used_quota` 对存量用户不成立：他们的余额是在流水表
// 存在之前攒起来的，一行流水都没有。开账行的 delta 必须是 `quota + used_quota`，
// 不是 quota —— 已消耗的部分同样属于历史入账。

func openLedgerOpeningDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ledger-opening.db")
	legacyDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// 必须关掉底层 sql.DB，否则 Windows 上 TempDir 清理会因文件仍被占用而报错
	// （与 003_test.go / 016_test.go / 018_test.go 同一处理）。
	t.Cleanup(func() {
		if sqlDB, err := legacyDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := legacyDB.AutoMigrate(&model.User{}, &model.QuotaLedger{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return legacyDB
}

func seedLegacyUser(t *testing.T, db *gorm.DB, name string, quota, used float64) uint {
	t.Helper()
	u := model.User{Username: name, Password: "x", Quota: quota, UsedQuota: used}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed user %q: %v", name, err)
	}
	return u.ID
}

func readLedgerRows(t *testing.T, db *gorm.DB, userID uint) []model.QuotaLedger {
	t.Helper()
	var rows []model.QuotaLedger
	if err := db.Where("user_id = ?", userID).Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("read ledger for user %d: %v", userID, err)
	}
	return rows
}

// 工单 §5 T7 点名的场景：quota=45, used_quota=20 → 开账 delta == 65，且不变式成立。
func TestOpeningBalanceUsesQuotaPlusUsedQuota(t *testing.T) {
	db := openLedgerOpeningDB(t)
	uid := seedLegacyUser(t, db, "legacy-spender", 45, 20)

	if err := migrateQuotaLedgerOpeningBalance(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rows := readLedgerRows(t, db, uid)
	if len(rows) != 1 {
		t.Fatalf("开账行数 = %d, want 1", len(rows))
	}
	got := rows[0]
	if math.Abs(got.Delta-65) > 1e-9 {
		t.Fatalf("开账 delta = %.17g, want 65（= quota 45 + used_quota 20，写成 45 就漏了已消耗部分）", got.Delta)
	}
	if got.Kind != model.LedgerKindOpeningBalance {
		t.Errorf("kind = %q, want %q", got.Kind, model.LedgerKindOpeningBalance)
	}
	if got.ActorID != 0 {
		t.Errorf("actor_id = %d, want 0（系统开账没有操作者）", got.ActorID)
	}
	if got.Reason == "" {
		t.Error("reason 为空 —— 开账行必须能解释自己是从哪来的")
	}
	if got.CreatedAt <= 0 {
		t.Errorf("created_at = %d, want > 0", got.CreatedAt)
	}

	// 不变式：quota == Σdelta - used_quota。
	var sum float64
	if err := db.Model(&model.QuotaLedger{}).Where("user_id = ?", uid).
		Select("COALESCE(SUM(delta), 0)").Scan(&sum).Error; err != nil {
		t.Fatalf("sum: %v", err)
	}
	if want := sum - 20; math.Abs(want-45) > 1e-9 {
		t.Fatalf("不变式破了：Σdelta(%.17g) - used_quota(20) = %.17g, quota = 45", sum, want)
	}
}

// 余额花光的用户（quota=0, used_quota=50）必须开账 —— 判据是**合计**不为零，
// 不是 quota 不为零。漏了这类用户，他们第一天就会被对账接口报成 -50 的漂移。
func TestOpeningBalanceCoversFullySpentUsers(t *testing.T) {
	db := openLedgerOpeningDB(t)
	uid := seedLegacyUser(t, db, "spent-out", 0, 50)

	if err := migrateQuotaLedgerOpeningBalance(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	rows := readLedgerRows(t, db, uid)
	if len(rows) != 1 {
		t.Fatalf("开账行数 = %d, want 1（quota=0 但 used_quota=50 的用户必须开账）", len(rows))
	}
	if math.Abs(rows[0].Delta-50) > 1e-9 {
		t.Fatalf("开账 delta = %.17g, want 50", rows[0].Delta)
	}
}

// 合计为零的用户不开账：Σdelta 本就该是 0，记一行 delta=0 只是噪音。
// 覆盖两种：全新零余额用户，以及恰好透支等额的用户（quota=-50, used_quota=50）。
func TestOpeningBalanceSkipsZeroNetUsers(t *testing.T) {
	db := openLedgerOpeningDB(t)
	fresh := seedLegacyUser(t, db, "fresh", 0, 0)
	evened := seedLegacyUser(t, db, "evened-out", -50, 50)

	if err := migrateQuotaLedgerOpeningBalance(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for label, uid := range map[string]uint{"fresh": fresh, "evened-out": evened} {
		if rows := readLedgerRows(t, db, uid); len(rows) != 0 {
			t.Errorf("%s 开账行数 = %d, want 0", label, len(rows))
		}
	}

	// 反向确认不变式对这两位仍成立（Σdelta=0）：
	// fresh    → 0 == 0 - 0 ✓
	// evened   → -50 == 0 - 50 ✓
	var evenedQuota, evenedUsed float64
	if err := db.Model(&model.User{}).Select("quota", "used_quota").
		Where("id = ?", evened).Row().Scan(&evenedQuota, &evenedUsed); err != nil {
		t.Fatalf("read evened: %v", err)
	}
	if math.Abs(evenedQuota-(0-evenedUsed)) > 1e-9 {
		t.Fatalf("不变式对透支等额用户不成立：quota=%.17g, 0-used=%.17g", evenedQuota, 0-evenedUsed)
	}
}

// 幂等 + 不碰已有流水的用户：重跑三次不得追加行，且已经有流水的用户一行都不补
// （否则会把他们的余额来源凭空翻倍）。
func TestOpeningBalanceIsIdempotentAndSkipsUsersWithLedger(t *testing.T) {
	db := openLedgerOpeningDB(t)
	legacy := seedLegacyUser(t, db, "legacy", 30, 0)
	active := seedLegacyUser(t, db, "active", 70, 0)

	// active 已经有一条真实充值流水（流水表上线后注册的用户）。
	if err := db.Create(&model.QuotaLedger{
		UserID: active, Delta: 70, Kind: model.LedgerKindTopupEpay, CreatedAt: 1,
	}).Error; err != nil {
		t.Fatalf("seed active ledger: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := migrateQuotaLedgerOpeningBalance(db); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}

	if rows := readLedgerRows(t, db, legacy); len(rows) != 1 {
		t.Errorf("legacy 用户流水行数 = %d, want 1（重跑追加了行）", len(rows))
	}

	activeRows := readLedgerRows(t, db, active)
	if len(activeRows) != 1 {
		t.Fatalf("active 用户流水行数 = %d, want 1（已有流水的用户不该被开账）", len(activeRows))
	}
	if activeRows[0].Kind != model.LedgerKindTopupEpay {
		t.Errorf("active 用户的流水 kind = %q, want %q（原有流水被改动）",
			activeRows[0].Kind, model.LedgerKindTopupEpay)
	}
}

// 开账**不得改动余额** —— 它只补历史记录。
func TestOpeningBalanceLeavesBalancesUntouched(t *testing.T) {
	db := openLedgerOpeningDB(t)
	uid := seedLegacyUser(t, db, "untouched", 45, 20)

	if err := migrateQuotaLedgerOpeningBalance(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var quota, used float64
	if err := db.Model(&model.User{}).Select("quota", "used_quota").
		Where("id = ?", uid).Row().Scan(&quota, &used); err != nil {
		t.Fatalf("read: %v", err)
	}
	if quota != 45 || used != 20 {
		t.Fatalf("开账改动了余额：quota=%.17g used=%.17g, want 45/20", quota, used)
	}
}

// 表缺失时静默跳过，nil db 报错。
func TestOpeningBalanceSkipsWhenTablesMissing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "empty.db")
	emptyDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := emptyDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	if err := migrateQuotaLedgerOpeningBalance(emptyDB); err != nil {
		t.Errorf("空库上 migrate = %v, want nil", err)
	}
	if err := migrateQuotaLedgerOpeningBalance(nil); err == nil {
		t.Error("migrateQuotaLedgerOpeningBalance(nil) = nil, want error")
	}
}
