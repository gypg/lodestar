package migrate

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

func init() {
	// 刻意注册为 Before：加宽必须发生在 AutoMigrate **之前**。
	// AutoMigrate 自己也会比较列类型并尝试 ALTER，但那条路径依赖 GORM 的类型等价
	// 启发式（"real" vs "double precision" 的别名判定），且失败会直接让进程起不来。
	// 先把类型改对，AutoMigrate 再看到时已经一致，它那条分支就不会被触发。
	RegisterBeforeAutoMigration(Migration{
		Version: 20,
		Up:      migrateWidenMoneyColumnsToFloat8,
		Down:    stubDown(20),
	})
}

/*
迁移 020 —— 把金额/额度列从 float4 加宽到 float8。

# 为什么是缺陷

这些列的 struct tag 曾经写的是 `gorm:"type:real"`。`real` 在三个方言里语义**不同**：

	SQLite      REAL 恒为 8 字节 IEEE 双精度（类型名只决定亲和，不决定宽度）
	MySQL       REAL 默认是 DOUBLE 的同义词（8 字节；除非开 REAL_AS_FLOAT）
	PostgreSQL  real = float4，**4 字节**，numeric_precision=24，约 7 位有效十进制位

于是本地 SQLite 测试全绿，生产 Postgres 上却是单精度。两处实测后果：

 1. 对账接口（GET /api/v1/wallet/reconcile）会把每个活跃用户误报成漂移。
    不变式 `quota == Σ(ledger.delta) - used_quota` 的三个量都被 float4 截断，
    误差落在 1e-5 量级，而 user.ReconcileTolerance 是 1e-9 —— 差四个数量级。
    容差存在的理由恰恰是"别让浮点噪声把每个活跃用户报成漂移"，float4 把它自己
    要防的失效模式变成了必然。
 2. 金额本身丢精度。float4 存 99999.99 的误差是 0.0022（超过 0.2 分钱），
    存 1234.56 的误差是 5.9e-05。余额上千之后就丢亚分精度。

stats_* 的 input_cost / output_cost 不是"欠款"，但它们是**读-改-写**累加的
（op/stats 把增量加到旧值上再写回），每次写回都截断一次。累计到 $1000 时 float4
的 ULP 已是 6e-5，单笔低于这个量级的费用加进去等于没加 —— 误差不是一次性的，是复利。

# 为什么只 ALTER Postgres

按上表，SQLite 与 MySQL 的存储宽度**本来就是 8 字节**，没有需要修的东西。
这不是"暂不支持"，是这两个方言上不存在这个缺陷。

# 加宽是无损的，但不修复已损坏的值

float4 → float8 是纯加宽：每个 float4 都能被 float8 精确表示，存量值按位原样保留。
但**已经**在写入时丢掉的精度不会被找回（float4 里的 99999.9921875 加宽后仍是
99999.9921875，不会变回 99999.99）。核实过的生产现状：users.quota / used_quota 均为 0，
quota_ledgers / payment_orders / topup_codes / subscription_* 全为空表，
只有 stats_dailies 的 20 行成本聚合带着已截断的值 —— 那是展示用聚合，不是账。
所以此次没有需要修复的金额。若将来在已有真实余额的库上补这条迁移，得先评估存量值。
*/

// MoneyFloat4Column 标识一列金额/额度列。导出是为了让 internal/db 的端到端测试
// 能拿这份清单去构造"老库"（它需要真实的全量 schema，本包里的表状态被其它测试 drop 过）。
type MoneyFloat4Column struct{ Table, Column string }

// MoneyFloat4Columns 是曾以 `type:real` 落库的列的**历史清单**。
//
// 是历史清单而不是"当前模型里的钱列"：模型侧的 tag 已经改成 double precision，
// 新建库由 AutoMigrate 直接建成 float8，只有升级上来的老库才有 float4 要修。
// 因此这份清单一旦发版就**冻结**，不要因为以后新增了钱列而往里加 —— 新列不会是 float4。
//
// 完备性不靠人肉核对这份清单，靠 postgres_test.go 里的
// TestPostgresNoFloat4MoneyColumnsAfterMigrate：它断言跑完 Migrate 后全库零 float4，
// 漏了任何一列都会红。
var MoneyFloat4Columns = []MoneyFloat4Column{
	// 钱包与流水
	{"users", "quota"},
	{"users", "used_quota"},
	{"quota_ledgers", "delta"},

	// 支付与兑换
	{"payment_orders", "amount_usd"},
	{"payment_orders", "money"},
	{"topup_codes", "quota"},

	// 订阅
	{"subscription_plans", "price"},
	{"subscription_plans", "quota_amount"},
	{"subscription_orders", "money"},
	{"user_subscriptions", "amount_total"},
	{"user_subscriptions", "amount_used"},

	// 成本聚合：StatsMetrics 内嵌在 7 张表里，每张都有 input_cost / output_cost。
	// 数量看着多，其实是同一个 struct 字段被复制了 7 份。
	{"stats_totals", "input_cost"},
	{"stats_totals", "output_cost"},
	{"stats_dailies", "input_cost"},
	{"stats_dailies", "output_cost"},
	{"stats_hourlies", "input_cost"},
	{"stats_hourlies", "output_cost"},
	{"stats_models", "input_cost"},
	{"stats_models", "output_cost"},
	{"stats_channels", "input_cost"},
	{"stats_channels", "output_cost"},
	{"stats_api_keys", "input_cost"},
	{"stats_api_keys", "output_cost"},
	{"stats_site_model_hourlies", "input_cost"},
	{"stats_site_model_hourlies", "output_cost"},
}

// WidenMoneyColumnsToFloat8 是这条迁移执行的操作，导出供 internal/db 的测试在**真实
// 全量 schema** 上验证升级路径。
//
// 为什么必须导出、不能让测试走 db.Migrate：BeforeAutoMigrate 跑完会把注册表置 nil
// （每进程只跑一次）。internal/db 包里 db_test.go 的 InitDB 测试按文件名排在前面、
// 会先把注册表吃掉，于是通过 db.Migrate 验证升级路径的测试会**空转绿** —— 断言"没有
// float4"照样成立，因为新建库靠 struct tag 本来就是 float8。实测过这个假绿。
func WidenMoneyColumnsToFloat8(db *gorm.DB) error {
	return migrateWidenMoneyColumnsToFloat8(db)
}

func migrateWidenMoneyColumnsToFloat8(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if db.Dialector == nil {
		return nil
	}
	switch db.Dialector.Name() {
	case "postgres", "postgresql":
		return widenMoneyColumnsPostgres(db)
	default:
		// sqlite / mysql：存储宽度本就是 8 字节，无操作。见文件头注释。
		return nil
	}
}

// widenMoneyColumnsPostgres 只 ALTER 当前确实是 float4 的列。
//
// 先查目录再改，而不是无条件 ALTER + 忽略错误：
//   - 全新库这里返回零行（表还没建），天然跳过，不需要 HasTable 守卫；
//   - 已经加宽过的库同样返回零行，重跑安全；
//   - 真出错时错误会往上抛，不会被 `_ =` 吞掉变成静默失败。
func widenMoneyColumnsPostgres(db *gorm.DB) error {
	type target struct {
		TableName  string
		ColumnName string
	}
	var pending []target
	// current_schema() 而不是硬编码 'public'：跟着连接的 search_path 走，
	// 否则把表建在别的 schema 里时这条迁移会静默什么都不做。
	if err := db.Raw(`
SELECT table_name, column_name
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND data_type = 'real'`).Scan(&pending).Error; err != nil {
		return fmt.Errorf("query float4 columns: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	// 只动清单里的列。目录里若有清单外的 float4 列（历史遗留的已废弃列），
	// 不属于这条迁移的职责，留着不管。
	known := make(map[string]struct{}, len(MoneyFloat4Columns))
	for _, c := range MoneyFloat4Columns {
		known[c.Table+"."+c.Column] = struct{}{}
	}

	for _, p := range pending {
		if _, ok := known[p.TableName+"."+p.ColumnName]; !ok {
			continue
		}
		// 表名/列名来自 information_schema（不是外部输入），仍按方言转义 ——
		// 与 001.go 同一惯例，保留字或大小写敏感的标识符都能正确引用。
		stmt := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE double precision",
			quoteIdent(db, p.TableName), quoteIdent(db, p.ColumnName))
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("widen %s.%s to double precision: %w",
				p.TableName, p.ColumnName, err)
		}
	}
	return nil
}

// quoteIdent 按方言转义一个标识符（001.go 用的是同一条 QuoteTo 路径）。
func quoteIdent(db *gorm.DB, ident string) string {
	var sb strings.Builder
	db.Dialector.QuoteTo(&sb, ident)
	return sb.String()
}
