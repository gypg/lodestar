package migrate

/*
迁移 020 —— 金额列 float4 → float8。

这批测试**只在 Postgres 上有意义**：float4/float8 的区别是 Postgres 独有的，
SQLite 的 REAL 恒为 8 字节，MySQL 的 REAL 默认就是 DOUBLE。所以没有 SQLite 版本 ——
在 SQLite 上写这个测试会永远绿，恰好复刻了这个缺陷最初逃过测试的原因。
*/

import (
	"math"
	"sort"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// pgColumnType 返回一列在 Postgres 目录里的实际类型（"real" / "double precision"）。
func pgColumnType(t *testing.T, conn *gorm.DB, table, column string) string {
	t.Helper()
	var got string
	err := conn.Raw(`
SELECT data_type FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
		table, column).Scan(&got).Error
	if err != nil {
		t.Fatalf("read type of %s.%s: %v", table, column, err)
	}
	if got == "" {
		t.Fatalf("%s.%s 不存在", table, column)
	}
	return got
}

// TestPostgresFloat4LosesMoneyPrecision 是这条迁移存在的**理由**，不是对迁移的测试。
//
// 它证明 float4 会改变写进去的金额，也就是"本地 SQLite 全绿"背后被掩盖的事实。
// 如果哪天有人觉得这条迁移是多余的，先看这个测试。
func TestPostgresFloat4LosesMoneyPrecision(t *testing.T) {
	conn := openPostgresForTest(t)
	if err := conn.Exec(`DROP TABLE IF EXISTS money_precision_probe`).Error; err != nil {
		t.Fatalf("drop probe: %v", err)
	}
	if err := conn.Exec(`
CREATE TABLE money_precision_probe (
	as_real real,
	as_float8 double precision
)`).Error; err != nil {
		t.Fatalf("create probe: %v", err)
	}
	t.Cleanup(func() { _ = conn.Exec(`DROP TABLE IF EXISTS money_precision_probe`) })

	// 一个站长真会填的余额。float4 只有约 7 位有效十进制位，这个数需要 7 位整数 + 2 位小数。
	const balance = 99999.99
	if err := conn.Exec(`INSERT INTO money_precision_probe VALUES (?, ?)`, balance, balance).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}

	var gotReal, gotFloat8 float64
	if err := conn.Raw(`SELECT as_real, as_float8 FROM money_precision_probe`).Row().
		Scan(&gotReal, &gotFloat8); err != nil {
		t.Fatalf("read back: %v", err)
	}

	// float8 必须精确往返 —— 加宽之后金额不再被改写。
	if gotFloat8 != balance {
		t.Errorf("double precision 未精确往返：写 %v 读回 %v", balance, gotFloat8)
	}
	// float4 必须**不**精确往返。这条断言反过来写（要求它坏）是刻意的：
	// 若某天它开始精确了（换了 DB / 开了别的设置），说明前提变了，该重新评估这条迁移。
	if gotReal == balance {
		t.Fatalf("real 竟然精确往返了 %v —— 迁移 020 的前提已不成立，请重新评估", balance)
	}
	lost := math.Abs(gotReal - balance)
	if lost < 0.0001 {
		t.Errorf("real 的误差 %v 小于预期量级，前提可能已变", lost)
	}
	t.Logf("float4 存 %v 读回 %v（误差 %v，超过 0.2 分钱）", balance, gotReal, lost)
}

// TestPostgresMigration020WidensLegacyFloat4 走真实升级路径：先造一张 float4 的老表，
// 跑迁移，断言类型变了、存量值按位保留、且新写入的值不再被截断。
func TestPostgresMigration020WidensLegacyFloat4(t *testing.T) {
	conn := openPostgresForTest(t)

	// 造老库：users.quota / used_quota 是 float4，带 NOT NULL 与 DEFAULT ——
	// ALTER COLUMN TYPE 必须把这两个属性保住，否则加宽本身就成了新缺陷。
	if err := conn.Migrator().DropTable("users"); err != nil {
		t.Fatalf("drop users: %v", err)
	}
	if err := conn.Exec(`
CREATE TABLE users (
	id BIGSERIAL PRIMARY KEY,
	username text,
	quota real NOT NULL DEFAULT 0,
	used_quota real NOT NULL DEFAULT 0
)`).Error; err != nil {
		t.Fatalf("create legacy users: %v", err)
	}
	// 残表不能留给同包其它测试（详见 TestPostgresMigration020WidensEveryListedColumn
	// 里的说明：残缺 schema 会让别的迁移测试在 ALTER 时报列不存在）。
	t.Cleanup(func() { _ = conn.Migrator().DropTable("users") })

	// 存量值。取一个 float4 能精确表示的数（0.5 的幂之和），这样"按位保留"可以钉死断言：
	// 加宽是无损的，但它不会去修复写入时**已经**丢掉的精度，所以拿 99999.99 当存量值
	// 断言不出"无损"，只会断言出"仍然是坏的"。
	const exact = 1234.5
	if err := conn.Exec(`INSERT INTO users (username, quota, used_quota) VALUES (?, ?, ?)`,
		"legacy", exact, 0.25).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	if got := pgColumnType(t, conn, "users", "quota"); got != "real" {
		t.Fatalf("前置条件不成立：users.quota 应为 real，实际 %s", got)
	}

	if err := migrateWidenMoneyColumnsToFloat8(conn); err != nil {
		t.Fatalf("migrate 020: %v", err)
	}

	for _, col := range []string{"quota", "used_quota"} {
		if got := pgColumnType(t, conn, "users", col); got != "double precision" {
			t.Errorf("users.%s 加宽后应为 double precision，实际 %s", col, got)
		}
	}

	// 存量值按位保留。
	var gotQuota, gotUsed float64
	if err := conn.Raw(`SELECT quota, used_quota FROM users WHERE username = ?`, "legacy").
		Row().Scan(&gotQuota, &gotUsed); err != nil {
		t.Fatalf("read migrated row: %v", err)
	}
	if gotQuota != exact {
		t.Errorf("存量 quota 被改写：want %v got %v", exact, gotQuota)
	}
	if gotUsed != 0.25 {
		t.Errorf("存量 used_quota 被改写：want %v got %v", 0.25, gotUsed)
	}

	// NOT NULL 必须还在 —— ALTER COLUMN TYPE 不该顺手把约束丢了。
	var nullable string
	if err := conn.Raw(`
SELECT is_nullable FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = 'users' AND column_name = 'quota'`).
		Scan(&nullable).Error; err != nil {
		t.Fatalf("read is_nullable: %v", err)
	}
	if nullable != "NO" {
		t.Errorf("加宽后 users.quota 的 NOT NULL 丢了：is_nullable=%s", nullable)
	}

	// DEFAULT 必须还在。
	var def string
	if err := conn.Raw(`
SELECT COALESCE(column_default, '') FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = 'users' AND column_name = 'quota'`).
		Scan(&def).Error; err != nil {
		t.Fatalf("read column_default: %v", err)
	}
	if def == "" {
		t.Error("加宽后 users.quota 的 DEFAULT 丢了")
	}

	// 加宽后新写入的金额不再被截断 —— 这才是修复的用户可见效果。
	const precise = 99999.99
	if err := conn.Exec(`UPDATE users SET quota = ? WHERE username = ?`, precise, "legacy").Error; err != nil {
		t.Fatalf("update to precise value: %v", err)
	}
	if err := conn.Raw(`SELECT quota FROM users WHERE username = ?`, "legacy").
		Row().Scan(&gotQuota); err != nil {
		t.Fatalf("read precise value: %v", err)
	}
	if gotQuota != precise {
		t.Errorf("加宽后仍未精确往返：写 %v 读回 %v", precise, gotQuota)
	}
}

// TestPostgresMigration020IsIdempotent —— 迁移记录表理论上保证只跑一次，
// 但迁移失败后重启会重跑，所以重跑必须安全。
func TestPostgresMigration020IsIdempotent(t *testing.T) {
	conn := openPostgresForTest(t)
	if err := conn.Migrator().DropTable("topup_codes"); err != nil {
		t.Fatalf("drop topup_codes: %v", err)
	}
	if err := conn.Exec(`
CREATE TABLE topup_codes (id BIGSERIAL PRIMARY KEY, quota real NOT NULL)`).Error; err != nil {
		t.Fatalf("create legacy topup_codes: %v", err)
	}
	t.Cleanup(func() { _ = conn.Migrator().DropTable("topup_codes") })
	if err := conn.Exec(`INSERT INTO topup_codes (quota) VALUES (?)`, 8.5).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	for i := 1; i <= 3; i++ {
		if err := migrateWidenMoneyColumnsToFloat8(conn); err != nil {
			t.Fatalf("第 %d 次跑迁移失败：%v", i, err)
		}
		if got := pgColumnType(t, conn, "topup_codes", "quota"); got != "double precision" {
			t.Fatalf("第 %d 次跑完后类型是 %s", i, got)
		}
	}

	var got float64
	if err := conn.Raw(`SELECT quota FROM topup_codes`).Row().Scan(&got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != 8.5 {
		t.Errorf("重跑改写了值：want 8.5 got %v", got)
	}
}

// TestPostgresMigration020LeavesUnlistedFloat4Alone —— 清单外的 float4 列不属于
// 这条迁移的职责（可能是历史遗留的已废弃列）。顺手全改会让这条迁移的影响面不可预期。
func TestPostgresMigration020LeavesUnlistedFloat4Alone(t *testing.T) {
	conn := openPostgresForTest(t)
	if err := conn.Exec(`DROP TABLE IF EXISTS unlisted_float4_probe`).Error; err != nil {
		t.Fatalf("drop probe: %v", err)
	}
	if err := conn.Exec(`
CREATE TABLE unlisted_float4_probe (some_ratio real)`).Error; err != nil {
		t.Fatalf("create probe: %v", err)
	}
	t.Cleanup(func() { _ = conn.Exec(`DROP TABLE IF EXISTS unlisted_float4_probe`) })

	if err := migrateWidenMoneyColumnsToFloat8(conn); err != nil {
		t.Fatalf("migrate 020: %v", err)
	}
	if got := pgColumnType(t, conn, "unlisted_float4_probe", "some_ratio"); got != "real" {
		t.Errorf("清单外的列被动了：want real got %s", got)
	}
}

// 清单里的表名/列名是否真实存在（打字错误会让那一列在升级时被静默跳过），
// 由 internal/db 的 TestPostgresMoneyColumnsSurviveFullMigrate 验证 —— 那里能拿到
// 全量 schema，而本包的表状态被同包其它测试 drop 过，靠不住。

// TestPostgresMigration020WidensEveryListedColumn 是对**完备性**的正面验证：
// 把清单里的全部 25 列都造成 float4，跑一次迁移，断言无一残留。
//
// 与 TestPostgresNoFloat4AfterFullMigrate 的分工：那个查的是"跑完 db.Migrate 之后
// 库里没有 float4"（覆盖新建库路径），这个查的是"老库的每一列都真被这条迁移改到了"
// （覆盖升级路径）。少了这个，清单里某一列的 ALTER 静默失效不会被发现。
func TestPostgresMigration020WidensEveryListedColumn(t *testing.T) {
	conn := openPostgresForTest(t)

	// 每张表单独造，列名取自清单本身 —— 这样清单增删条目时测试自动跟随。
	byTable := map[string][]string{}
	for _, c := range MoneyFloat4Columns {
		byTable[c.Table] = append(byTable[c.Table], c.Column)
	}

	// 这些是**残表**（只有 id + 金额列），不是真 schema。跑完必须 drop 掉，否则同包的
	// TestPostgresAfterAutoMigrateOnEmptyDB 会在残缺的 stats_hourlies 上执行迁移 008
	// 的 `ALTER TABLE ... ADD PRIMARY KEY (hour, date)` 并报 column "hour" does not exist。
	// 实测过：不清理时那个测试确实红（每条迁移都守 HasTable，drop 回"不存在"才是干净起点）。
	t.Cleanup(func() {
		for table := range byTable {
			_ = conn.Migrator().DropTable(table)
		}
	})

	for table, cols := range byTable {
		if err := conn.Migrator().DropTable(table); err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
		ddl := "CREATE TABLE " + quoteIdent(conn, table) + " (id BIGSERIAL PRIMARY KEY"
		for _, col := range cols {
			ddl += ", " + quoteIdent(conn, col) + " real"
		}
		ddl += ")"
		if err := conn.Exec(ddl).Error; err != nil {
			t.Fatalf("create legacy %s: %v", table, err)
		}
	}

	if err := migrateWidenMoneyColumnsToFloat8(conn); err != nil {
		t.Fatalf("migrate 020: %v", err)
	}

	for _, c := range MoneyFloat4Columns {
		if got := pgColumnType(t, conn, c.Table, c.Column); got != "double precision" {
			t.Errorf("%s.%s 未被加宽：%s", c.Table, c.Column, got)
		}
	}
}

// productionFloat4ColumnsOn20260827 是 2026-08-27 在**生产库**上实测查出的全部 float4 列：
//
//	docker exec postgres psql -U admin -d lodestar -tAc "SELECT table_name, column_name
//	  FROM information_schema.columns
//	  WHERE table_schema='public' AND data_type='real'"
//
// 期望值刻意取自代码之外的观测。这一点是本测试全部强度的来源：若期望值改由遍历模型 tag
// 生成，它就会跟着被测代码一起变，漏项永远发现不了 —— 而漏项正是这次缺陷的原形。
var productionFloat4ColumnsOn20260827 = []string{
	"payment_orders.amount_usd",
	"payment_orders.money",
	"quota_ledgers.delta",
	"stats_api_keys.input_cost",
	"stats_api_keys.output_cost",
	"stats_channels.input_cost",
	"stats_channels.output_cost",
	"stats_dailies.input_cost",
	"stats_dailies.output_cost",
	"stats_hourlies.input_cost",
	"stats_hourlies.output_cost",
	"stats_models.input_cost",
	"stats_models.output_cost",
	"stats_site_model_hourlies.input_cost",
	"stats_site_model_hourlies.output_cost",
	"stats_totals.input_cost",
	"stats_totals.output_cost",
	"subscription_orders.money",
	"subscription_plans.price",
	"subscription_plans.quota_amount",
	"topup_codes.quota",
	"user_subscriptions.amount_total",
	"user_subscriptions.amount_used",
	"users.quota",
	"users.used_quota",
}

// TestMoneyFloat4ColumnsMatchesProductionCatalog —— 清单**完备性**守卫（不需要 Postgres）。
//
// 为什么单独要这一条：postgres_money_schema_test.go 的升级路径测试是**自指**的 ——
// 它只窄化清单里的列，所以清单少一项，那一项就不会被窄化，"零 float4" 照样成立。
// 实测验证过这个盲区：删掉 {"quota_ledgers","delta"} 后那个测试仍然绿。
// 漏项的后果是那一列在真实升级里被静默跳过，永远留在 float4。
//
// 反过来，"以后新增的钱列被写成 type:real" 不需要在这里管：
// TestPostgresFreshSchemaHasNoFloat4Columns 会在它发版之前就把 tag 拦下来，
// 于是它永远不会变成需要加宽的存量 float4 列。所以这份清单是**冻结**的。
func TestMoneyFloat4ColumnsMatchesProductionCatalog(t *testing.T) {
	got := make([]string, 0, len(MoneyFloat4Columns))
	for _, c := range MoneyFloat4Columns {
		got = append(got, c.Table+"."+c.Column)
	}
	sort.Strings(got)

	want := append([]string(nil), productionFloat4ColumnsOn20260827...)
	sort.Strings(want)

	missing := difference(want, got)
	extra := difference(got, want)

	if len(missing) > 0 {
		t.Errorf("清单漏了这些列 —— 它们在生产库里是 float4，不加进清单就永远不会被加宽：\n  %s",
			strings.Join(missing, "\n  "))
	}
	if len(extra) > 0 {
		t.Errorf("清单里多了这些列（生产目录里不存在同名 float4 列）。"+
			"若确实是新库才有的列，它由 struct tag 直接建成 float8，不需要进这份清单：\n  %s",
			strings.Join(extra, "\n  "))
	}
	if len(got) != len(productionFloat4ColumnsOn20260827) {
		t.Errorf("清单 %d 项，生产实测 %d 项", len(got), len(productionFloat4ColumnsOn20260827))
	}
}

// difference 返回在 a 里但不在 b 里的元素。
func difference(a, b []string) []string {
	inB := make(map[string]struct{}, len(b))
	for _, x := range b {
		inB[x] = struct{}{}
	}
	var out []string
	for _, x := range a {
		if _, ok := inB[x]; !ok {
			out = append(out, x)
		}
	}
	return out
}

// TestMigration020RegisteredAsBeforeAutoMigration —— 接线守卫（不需要 Postgres）。
//
// 别的测试都直接调 migrateWidenMoneyColumnsToFloat8，函数写对但**没注册**时它们全绿，
// 而生产上升级库里的列会一直是 float4。这里守注册本身。
//
// 顺序必须是 Before 而不是 After：加宽要发生在 AutoMigrate 之前。放到 After 就变成
// 先由 AutoMigrate 的类型等价启发式去 ALTER（"real" vs "double precision" 的别名判定），
// 那条路径失败会直接让进程起不来。
func TestMigration020RegisteredAsBeforeAutoMigration(t *testing.T) {
	if !registeredVersions.hasBefore(20) {
		t.Errorf("迁移 020 不在 BeforeAutoMigrate 注册表里（before=%v after=%v）。"+
			"没有它，从旧版本升级上来的 Postgres 库里金额列会一直是 float4。",
			registeredVersions.before, registeredVersions.after)
	}
	if registeredVersions.hasAfter(20) {
		t.Error("迁移 020 被注册成 AfterAutoMigration 了 —— 必须是 Before，见本函数注释。")
	}
}

// TestPostgresBeforeAutoMigrateOnEmptyDB —— 迁移 020 是**史上第一条 Before 迁移**
// （实测注册表：before=[20]，after=[1..19]）。在它之前 BeforeAutoMigrate 一直走
// `len(migrations)==0` 早退，从没真正执行过 —— 也就是说这条路径是新活的。
//
// 关键点：Before 阶段跑在 AutoMigrate **之前**，那时连 migration_records 表都还不存在，
// 得靠 ensureMigrationRecordTable 自己建。这个测试就是在验空库上那一步不炸。
// 与 TestPostgresAfterAutoMigrateOnEmptyDB 是同一模式的镜像。
func TestPostgresBeforeAutoMigrateOnEmptyDB(t *testing.T) {
	conn := openPostgresForTest(t)

	// 干净起点：连 migration_records 都不存在，复刻全新部署。
	_ = conn.Migrator().DropTable("migration_records")

	if err := BeforeAutoMigrate(conn); err != nil {
		t.Fatalf("BeforeAutoMigrate on empty postgres: %v", err)
	}

	if !conn.Migrator().HasTable("migration_records") {
		t.Fatal("Before 阶段没能建出 migration_records（AutoMigrate 还没跑，只能靠它自己建）")
	}
	var status int
	if err := conn.Raw(`SELECT status FROM migration_records WHERE version = 20`).
		Scan(&status).Error; err != nil {
		t.Fatalf("read migration record 20: %v", err)
	}
	if status != int(MigrationRecordStatusSuccess) {
		t.Errorf("迁移 020 记录状态 = %d，want %d（success）", status, MigrationRecordStatusSuccess)
	}
}

// TestPostgresMigration020NilAndNonPostgresAreNoOps 覆盖两条早退分支。
func TestPostgresMigration020NilAndNonPostgresAreNoOps(t *testing.T) {
	if err := migrateWidenMoneyColumnsToFloat8(nil); err == nil {
		t.Error("db 为 nil 时应报错")
	}
	// 非 Postgres 方言直接返回 nil。用 SQLite 连接验证：它不该尝试任何 ALTER
	// （SQLite 上 ALTER COLUMN TYPE 是语法错误，真跑了会报错，所以这条断言有强度）。
	sqliteConn := openLedgerOpeningDB(t) // 现成的 SQLite 测试库助手（019_test.go）
	if err := migrateWidenMoneyColumnsToFloat8(sqliteConn); err != nil {
		t.Errorf("SQLite 上应为无操作，却报错：%v", err)
	}
}
