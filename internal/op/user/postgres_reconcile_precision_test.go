package user

/*
对账在 Postgres 上必须不受浮点噪声干扰 —— 这是 float4 缺陷的**用户可见后果**。

ReconcileTolerance 是 1e-9。在 float4（Postgres 的 real）下，quota / used_quota /
ledger.delta 三个量各自被截断到约 7 位有效十进制位，误差落在 1e-5 量级 —— 比容差大四个
数量级。后果是对账接口把**每个活跃用户**都报成漂移，真正的漂移淹没在噪音里，
而容差存在的理由恰恰就是防这件事。

这里在真 Postgres 上跑两遍同一段账：一遍 float4、一遍 float8。float4 那遍必须报漂移
（证明缺陷真实存在，不是纸上推演），float8 那遍必须干净。
*/

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func pgReconcileDSN(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("LODESTAR_TEST_POSTGRES_DSN"); v != "" {
		return v
	}
	if v := os.Getenv("DATABASE_URL"); strings.HasPrefix(v, "postgres") {
		return v
	}
	t.Skip("set LODESTAR_TEST_POSTGRES_DSN to run PostgreSQL reconcile-precision tests")
	return ""
}

// initReconcilePrecisionDB 在专属 schema 里备好 users + quota_ledgers。
//
// 专属 schema 而不是 public：CI 和本地都用同一个测试库，internal/db 与
// internal/db/migrate 的 TestPostgres* 就在 public 里 drop/create 这两张表，
// 包与包默认并行跑会互相拆台（实测过 `relation ... does not exist`）。
//
// search_path 走 DSN 而不是 `SET search_path`：后者只作用于单条连接，池里其它连接仍指
// 向 public。
func initReconcilePrecisionDB(t *testing.T, columnType string) {
	t.Helper()
	dsn := pgReconcileDSN(t)
	schema := "lodestar_recon_" + strings.ToLower(strings.NewReplacer(" ", "", "-", "").Replace(columnType))

	// 引导连接只负责重建 schema —— 刻意**不用** db.InitDB：那会在 public 上跑一遍全量
	// Migrate，既白花几秒，又把这个包也拖进跨包竞态（internal/db 与 internal/db/migrate
	// 的 TestPostgres* 就在 public 里 drop/create 同名表）。
	boot, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open bootstrap connection: %v", err)
	}
	if err := boot.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`).Error; err != nil {
		t.Fatalf("drop schema %s: %v", schema, err)
	}
	if err := boot.Exec(`CREATE SCHEMA ` + schema).Error; err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}
	if bootSQL, bootErr := boot.DB(); bootErr == nil {
		_ = bootSQL.Close()
	}

	if err := db.InitDB("postgres", dsn+" search_path="+schema, false); err != nil {
		t.Fatalf("InitDB on schema %s: %v", schema, err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var cs string
	if err := db.GetDB().Raw(`SELECT current_schema()`).Scan(&cs).Error; err != nil {
		t.Fatalf("read current_schema: %v", err)
	}
	if cs != schema {
		t.Fatalf("search_path 未生效：current_schema()=%q want %q", cs, schema)
	}

	// InitDB 已经跑过 Migrate，两张表就位。把金额列改成本轮要测的类型。
	//
	// 注意不能靠"再跑一次 Migrate 把它加宽回来"—— Migrate 末尾的 DEALLOCATE/DISCARD ALL
	// 会让同连接的下一次 HasTable 静默返回 false，第二次 Migrate 会去 CREATE 已存在的表
	// （见 db.go 里 Migrate 的注释）。所以这里直接 ALTER。
	for _, c := range []struct{ table, column string }{
		{"users", "quota"},
		{"users", "used_quota"},
		{"quota_ledgers", "delta"},
	} {
		stmt := `ALTER TABLE "` + c.table + `" ALTER COLUMN "` + c.column + `" TYPE ` + columnType
		if err := db.GetDB().Exec(stmt).Error; err != nil {
			t.Fatalf("set %s.%s to %s: %v", c.table, c.column, columnType, err)
		}
	}
}

// seedRealisticBooks 走真实漏斗记一段普通用户的账：充值 $100，再扣 50 笔 $0.000123。
// 每一步都走 MutateQuota / SettleUsage，所以这段账**按定义**是对得上的 ——
// 之后报出来的任何漂移都只能是列类型带来的浮点噪声。
func seedRealisticBooks(t *testing.T, ctx context.Context) model.User {
	t.Helper()
	u := model.User{Username: "precision-probe", Password: "x"}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := MutateQuota(nil, u.ID, 100, LedgerEntry{Kind: model.LedgerKindTopupEpay}, ctx); err != nil {
		t.Fatalf("topup: %v", err)
	}
	// 单笔金额刻意取到 float4 的分辨率之下：余额在 $100 量级时 float4 的 ULP 约 7.6e-6，
	// 而每笔 1.23e-4 只有它的 16 倍 —— 累加 50 次，截断误差稳定地攒到 1e-5 量级。
	for i := 0; i < 50; i++ {
		if err := SettleUsage(u.ID, 0.000123, ctx); err != nil {
			t.Fatalf("settle %d: %v", i, err)
		}
	}
	return u
}

// TestPostgresReconcileIsCleanOnFloat8 —— 修复后的行为，也是这次修复的**目的**。
func TestPostgresReconcileIsCleanOnFloat8(t *testing.T) {
	initReconcilePrecisionDB(t, "double precision")
	ctx := context.Background()
	seedRealisticBooks(t, ctx)

	drifts, err := ReconcileDrifts(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(drifts) != 0 {
		t.Fatalf("float8 下这段账应当对得上，却报了 %d 条漂移：%+v\n"+
			"（每一步都走漏斗，所以这里的漂移只能来自浮点噪声或不变式本身被破坏）",
			len(drifts), drifts)
	}
}

// TestPostgresReconcileFalsePositiveOnFloat4 —— 缺陷本身。
//
// 断言方向是"必须报出漂移"：这不是在保护一个期望行为，而是钉住"float4 会让对账失效"
// 这个前提。哪天它变绿了，说明前提已变（换了 DB / 改了容差），该重新评估这条修复。
func TestPostgresReconcileFalsePositiveOnFloat4(t *testing.T) {
	initReconcilePrecisionDB(t, "real")
	ctx := context.Background()
	u := seedRealisticBooks(t, ctx)

	drifts, err := ReconcileDrifts(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(drifts) == 0 {
		t.Fatalf("float4 下这段账竟然对上了 —— 迁移 020 的前提（real=float4 会让容差 %g 失效）"+
			"已不成立，请重新评估", ReconcileTolerance)
	}
	// 钉住量级：误差要比容差大好几个数量级，否则"活跃用户全被误报"这个说法就站不住。
	d := drifts[0]
	if d.UserID != u.ID {
		t.Errorf("报出的用户 id=%d，want %d", d.UserID, u.ID)
	}
	absDrift := d.Drift
	if absDrift < 0 {
		absDrift = -absDrift
	}
	if absDrift <= ReconcileTolerance*1000 {
		t.Errorf("float4 漂移只有 %g，不到容差 %g 的一千倍 —— 与实测量级（1e-5，约四万倍）不符，"+
			"说明这个场景没能复现缺陷", absDrift, ReconcileTolerance)
	}
	t.Logf("float4 下一个账目正常的用户被报成漂移：quota=%v used_quota=%v ledger_sum=%v drift=%g（容差 %g）",
		d.Quota, d.UsedQuota, d.LedgerSum, d.Drift, ReconcileTolerance)
}
