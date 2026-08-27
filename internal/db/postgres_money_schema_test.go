package db

/*
金额列在 Postgres 上必须是 float8。

背景：这些列的 struct tag 曾是 `gorm:"type:real"`。在 SQLite 上 REAL 恒为 8 字节，
在 Postgres 上 real = float4（4 字节，约 7 位有效十进制位）。于是本地 SQLite 测试全绿
而生产是单精度 —— 对账接口（user.ReconcileDrifts，容差 1e-9）会把每个活跃用户误报成
漂移，金额本身也丢亚分精度。修复见 internal/db/migrate/020.go。

# 为什么这里不调 db.Migrate

`migrate.BeforeAutoMigrate` 跑完会把注册表置 nil（每进程只跑一次）。本包 db_test.go 的
`InitDB("sqlite", …)` 按文件名排在前面、会先把注册表吃掉，所以在 CI 的
`go test ./...`（无 -run 过滤）下，通过 db.Migrate 验证升级路径的测试会**空转绿**：
断言"没有 float4"照样成立，因为新建库靠 struct tag 本来就是 float8，迁移根本没跑。
实测踩过这个假绿，所以这里直接用 autoMigrateModels 建 schema、直接调
migrate.WidenMoneyColumnsToFloat8 —— 与测试执行顺序无关。

（顺带：db.Migrate 末尾的 DEALLOCATE/DISCARD ALL 会让同连接的下一次 HasTable 静默返回
false，第二次调用 Migrate 会去 CREATE 已存在的表。详见 db.go 里 Migrate 的注释。）
*/

import (
	"os"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db/migrate"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// pgMoneyTestDSN 与 migrate 包的 pgTestDSN 同一约定（同一个 CI 服务容器）。
func pgMoneyTestDSN() string {
	if v := os.Getenv("LODESTAR_TEST_POSTGRES_DSN"); v != "" {
		return v
	}
	if v := os.Getenv("DATABASE_URL"); strings.HasPrefix(v, "postgres") {
		return v
	}
	return ""
}

// openMoneySchemaPostgres 在一个**专属 schema** 里给出干净的连接。
//
// 为什么不用 public：CI 与本地都用同一个测试库，而 internal/db/migrate 的 TestPostgres*
// 就在 public 里 drop/create users、stats_* 等表。`go test ./internal/db/...` 默认并行跑
// 这两个包，于是两边互相拆台 —— 实测 3/3 复现 `relation "subscription_plans" does not
// exist`（我 DROP SCHEMA 时对方正在 AutoMigrate，或反过来）。加 `-p 1` 能压住，但那要求
// 每个人每次都记得加参数。换成专属 schema 是结构性隔离，不依赖调用方式。
//
// search_path 走 DSN 而不是 `SET search_path`：后者只作用于当前那条连接，池里其它连接
// 仍指向 public。pgx 会把 DSN 里不认识的键当作服务端运行时参数下发，search_path 正是其一。
func openMoneySchemaPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := pgMoneyTestDSN()
	if dsn == "" {
		t.Skip("set LODESTAR_TEST_POSTGRES_DSN to run PostgreSQL money-schema tests")
	}

	const schema = "lodestar_money_test"

	// 引导连接：只负责重建 schema，随后关掉。
	// 不复用它跑测试，是因为 DROP/CREATE SCHEMA 会让本连接上已缓存的执行计划失效。
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
	if bootSQL, err := boot.DB(); err == nil {
		_ = bootSQL.Close()
	}

	conn, err := gorm.Open(postgres.Open(dsn+" search_path="+schema),
		&gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	// 前置条件：search_path 真的生效了。若没生效，测试会跑到 public 上去，
	// 既污染别的包又让断言变得不可解释 —— 必须在这里就失败，而不是在断言处才发现。
	var cs string
	if err := conn.Raw(`SELECT current_schema()`).Scan(&cs).Error; err != nil {
		t.Fatalf("read current_schema: %v", err)
	}
	if cs != schema {
		t.Fatalf("search_path 未生效：current_schema()=%q，want %q（DSN 参数被忽略了？）", cs, schema)
	}
	return conn
}

// float4Columns 返回当前 schema 里全部 float4 列，格式 "table.column"。
func float4Columns(t *testing.T, conn *gorm.DB) []string {
	t.Helper()
	var rows []struct {
		TableName  string
		ColumnName string
	}
	err := conn.Raw(`
SELECT table_name, column_name
FROM information_schema.columns
WHERE table_schema = current_schema() AND data_type = 'real'
ORDER BY table_name, column_name`).Scan(&rows).Error
	if err != nil {
		t.Fatalf("query float4 columns: %v", err)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.TableName+"."+r.ColumnName)
	}
	return out
}

const float4FixHint = "\n\n修法：把该列的 struct tag 从 `type:real` 改成 `type:double precision`。" +
	"\n若这一列**已经发版**（线上库里已是 float4），还要把 (表,列) 加进" +
	"\ninternal/db/migrate/020.go 的 MoneyFloat4Columns，否则老库不会被加宽。"

// TestPostgresFreshSchemaHasNoFloat4Columns —— 新建库路径（防回流主守卫）。
//
// 断言"整个 schema 零 float4"而不是"清单那 25 列是 float8"，是为了把**新增**的列一起
// 管住：只查清单的话，明天新加一个 `type:real` 的钱列照样全绿。
func TestPostgresFreshSchemaHasNoFloat4Columns(t *testing.T) {
	conn := openMoneySchemaPostgres(t)

	if err := conn.AutoMigrate(autoMigrateModels...); err != nil {
		t.Fatalf("AutoMigrate on fresh postgres: %v", err)
	}

	// 前置条件：schema 真建出来了。否则"零 float4"是空集意义上的成立 —— 假绿。
	var nCols int64
	if err := conn.Raw(`SELECT count(*) FROM information_schema.columns
WHERE table_schema = current_schema()`).Scan(&nCols).Error; err != nil {
		t.Fatalf("count columns: %v", err)
	}
	if nCols < 100 {
		t.Fatalf("schema 只建出 %d 列，AutoMigrate 没真跑起来 —— 下面的断言会是空转", nCols)
	}

	if got := float4Columns(t, conn); len(got) > 0 {
		t.Fatalf("新建库里出现 float4 列（Postgres 上 real=4 字节，约 7 位有效位，"+
			"会丢亚分精度并让对账接口把活跃用户误报成漂移）：\n  %s%s",
			strings.Join(got, "\n  "), float4FixHint)
	}
}

// TestPostgresLegacyFloat4SchemaGetsWidened —— 升级路径，打在**真实全量 schema** 上。
//
// 做法：建出真 schema，把清单里每一列窄回 float4 假装老库，跑加宽，断言全库零 float4。
//
// 这是清单正确性的唯一守卫，抓两类缺陷：
//   - 清单里的表名/列名打字错误 —— 窄化那一步直接失败（列不存在）。
//     真实升级时这种条目只会静默跳过，什么信号都没有；
//   - 加宽漏了某一列 —— 跑完它仍是 float4。
func TestPostgresLegacyFloat4SchemaGetsWidened(t *testing.T) {
	conn := openMoneySchemaPostgres(t)

	if err := conn.AutoMigrate(autoMigrateModels...); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	if len(migrate.MoneyFloat4Columns) == 0 {
		t.Fatal("MoneyFloat4Columns 是空的 —— 清单被清空后本测试会退化成永远绿")
	}

	for _, c := range migrate.MoneyFloat4Columns {
		if !conn.Migrator().HasTable(c.Table) {
			t.Errorf("清单里的表 %s 在真实 schema 里不存在（表名写错？）", c.Table)
			continue
		}
		if !conn.Migrator().HasColumn(c.Table, c.Column) {
			t.Errorf("清单里的列 %s.%s 不存在（列名写错？真实升级时这条会被静默跳过）",
				c.Table, c.Column)
			continue
		}
		stmt := `ALTER TABLE "` + c.Table + `" ALTER COLUMN "` + c.Column + `" TYPE real`
		if err := conn.Exec(stmt).Error; err != nil {
			t.Fatalf("窄化 %s.%s 到 real 失败：%v", c.Table, c.Column, err)
		}
	}
	if t.Failed() {
		t.FailNow() // 清单本身错了，后面的断言没有意义
	}

	// 前置条件：窄化确实生效，且**恰好**是清单那些列。数量不等说明清单与真实 schema
	// 对不上（例如同名列出现在别的表里），后面的"零 float4"就不再是对加宽的检验。
	//
	// ★ 已知盲区：这个测试是**自指**的 —— 只窄化清单里的列，所以清单**漏项**它抓不到
	// （漏掉的那列不会被窄化，"零 float4" 照样成立；实测删掉 quota_ledgers.delta 后本测试
	// 仍绿）。清单完备性由 migrate 包的 TestMoneyFloat4ColumnsMatchesProductionCatalog
	// 守，那里的期望值取自生产目录实测，不随被测代码变化。
	narrowed := float4Columns(t, conn)
	if len(narrowed) != len(migrate.MoneyFloat4Columns) {
		t.Fatalf("窄化后应有 %d 列 float4，实际 %d 列：\n  %s",
			len(migrate.MoneyFloat4Columns), len(narrowed), strings.Join(narrowed, "\n  "))
	}

	if err := migrate.WidenMoneyColumnsToFloat8(conn); err != nil {
		t.Fatalf("WidenMoneyColumnsToFloat8: %v", err)
	}

	if got := float4Columns(t, conn); len(got) > 0 {
		t.Fatalf("升级路径没把这些列加宽回 float8：\n  %s%s",
			strings.Join(got, "\n  "), float4FixHint)
	}
}

// 「迁移 020 是否真的接进了启动链」由 migrate 包的
// TestMigration020RegisteredAsBeforeAutoMigration 守 —— 上面两个测试都直接调加宽函数，
// 都不会发现"函数写对了但没注册"。
