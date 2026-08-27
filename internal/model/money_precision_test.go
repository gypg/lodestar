package model_test

/*
金额列在**每个方言**上都必须是 8 字节浮点。

Postgres 侧由 internal/db 与 internal/db/migrate 的 TestPostgres* 守。
这里守 SQLite：`double precision` 不是 SQLite 的原生类型名，它靠**类型亲和**规则生效
（声明名里含 "DOUB" → REAL 亲和 → 8 字节 IEEE 双精度）。这条规则是 migrate/020.go
"SQLite 上无需迁移" 那个判断的前提，所以要有测试钉住，而不是只写在注释里。
*/

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gypg/lodestar/internal/model"
	"gorm.io/gorm"
)

func openSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	conn, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "money.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := conn.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return conn
}

// TestSQLiteMoneyColumnsRoundTripFullFloat64 —— 取一个 float32 **存不下**的金额，
// 断言它在 SQLite 上精确往返。若 tag 被改回 `type:real`，SQLite 仍然是 8 字节，
// 所以这个测试**不会**红 —— 它守的不是 tag，而是"SQLite 侧不需要迁移"这个前提本身。
func TestSQLiteMoneyColumnsRoundTripFullFloat64(t *testing.T) {
	conn := openSQLite(t)
	if err := conn.AutoMigrate(&model.User{}, &model.QuotaLedger{}, &model.TopupCode{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	// float32(99999.99) == 99999.9921875，与 float64 值不同 —— 存得下就说明是 8 字节。
	const balance = 99999.99
	u := model.User{Username: "money", Password: "x", Quota: balance, UsedQuota: 1234.56}
	if err := conn.Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	var got model.User
	if err := conn.First(&got, u.ID).Error; err != nil {
		t.Fatalf("read user: %v", err)
	}
	if got.Quota != balance {
		t.Errorf("users.quota 未精确往返：写 %v 读回 %v", balance, got.Quota)
	}
	if got.UsedQuota != 1234.56 {
		t.Errorf("users.used_quota 未精确往返：写 %v 读回 %v", 1234.56, got.UsedQuota)
	}

	led := model.QuotaLedger{UserID: u.ID, Delta: balance, Kind: model.LedgerKindTopupEpay}
	if err := conn.Create(&led).Error; err != nil {
		t.Fatalf("create ledger: %v", err)
	}
	var gotLed model.QuotaLedger
	if err := conn.First(&gotLed, led.ID).Error; err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if gotLed.Delta != balance {
		t.Errorf("quota_ledgers.delta 未精确往返：写 %v 读回 %v", balance, gotLed.Delta)
	}

	code := model.TopupCode{Code: "c1", Quota: balance}
	if err := conn.Create(&code).Error; err != nil {
		t.Fatalf("create topup code: %v", err)
	}
	var gotCode model.TopupCode
	if err := conn.First(&gotCode, code.ID).Error; err != nil {
		t.Fatalf("read topup code: %v", err)
	}
	if gotCode.Quota != balance {
		t.Errorf("topup_codes.quota 未精确往返：写 %v 读回 %v", balance, gotCode.Quota)
	}
}

// TestSQLiteDeclaredTypeIsRealAffinity 把亲和规则本身钉死：SQLite 的 CREATE TABLE 里
// 声明名是 "double precision"，而 SQLite 按 "DOUB" 子串判给 REAL 亲和。
// 若哪天换了驱动或 SQLite 版本改了规则，这里会红，而不是悄悄退化成 4 字节。
func TestSQLiteDeclaredTypeIsRealAffinity(t *testing.T) {
	conn := openSQLite(t)
	if err := conn.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	var declType string
	if err := conn.Raw(
		`SELECT type FROM pragma_table_info('users') WHERE name = 'quota'`).
		Scan(&declType).Error; err != nil {
		t.Fatalf("pragma_table_info: %v", err)
	}
	if !strings.Contains(strings.ToUpper(declType), "DOUB") {
		t.Fatalf("users.quota 的声明类型是 %q，不含 \"DOUB\" —— REAL 亲和的判定前提已不成立", declType)
	}

	// 存储类别是"8 字节"的直接证据，比断言声明名更贴近后果。先塞一行再查 typeof()。
	if err := conn.Create(&model.User{Username: "aff", Password: "x", Quota: 1.5}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	var storageClass string
	if err := conn.Raw(`SELECT typeof(quota) FROM users LIMIT 1`).Scan(&storageClass).Error; err != nil {
		t.Fatalf("typeof: %v", err)
	}
	if storageClass != "real" {
		t.Errorf("users.quota 声明为 %q，SQLite 存储类别 typeof()=%q，want \"real\"（8 字节）",
			declType, storageClass)
	}
}
