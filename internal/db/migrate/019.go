package migrate

import (
	"fmt"
	"time"

	"github.com/gypg/lodestar/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 19,
		Up:      migrateQuotaLedgerOpeningBalance,
		Down:    stubDown(19),
	})
}

// migrateQuotaLedgerOpeningBalance 给流水表上线前就已存在的用户补一行开账记录。
//
// 不变式是 `quota == Σ(ledger.delta) - used_quota`。存量用户的余额是在流水表存在之前
// 累积起来的，一行流水都没有，所以升级后第一天就全员对不上账，对账接口会把整张用户表
// 报成漂移，真正的漂移反而淹没在噪音里。
//
// delta 取 `quota + used_quota` 而**不是** quota：已消耗的部分同样属于历史入账。
// 举例 quota=45 / used_quota=20 的用户，开账 delta 必须是 65，这样
// 65 - 20 = 45 才等于当前余额；写成 45 的话等式左右差出一个 used_quota。
//
// 这里刻意不走 user.MutateQuota 漏斗，两个理由：
//  1. 依赖方向 —— internal/db 依赖本包，本包再反向 import internal/op/user 会成环。
//  2. 语义 —— 开账**不改余额**，只补一条解释既有余额从何而来的历史记录。漏斗的职责是
//     "改余额并留痕"，这里只有后半段。
//
// 幂等靠 NOT EXISTS：已有任何流水行的用户一律跳过。重跑安全。
func migrateQuotaLedgerOpeningBalance(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	// 全新库先跑 AutoMigrate，两张表都该在；缺任何一张就跳过，别让迁移把启动搞挂。
	if !db.Migrator().HasTable(&model.User{}) || !db.Migrator().HasTable(&model.QuotaLedger{}) {
		return nil
	}

	// 单条 INSERT ... SELECT：用户多少行都是一次往返，且天然原子。
	//
	// `quota + used_quota != 0` 跳过合计为零的用户 —— 那种情况 Σdelta 本就该是 0，
	// 记一行 delta=0 只是噪音（与漏斗对 delta==0 的处理一致）。注意判据是**合计**不为零，
	// 不是 quota 不为零：余额花光的用户 quota=0 而 used_quota=50，合计 50 必须开账，
	// 否则 0 == 0 - 50 不成立，这个用户第一天就被报成漂移。
	return db.Exec(`
INSERT INTO quota_ledgers (user_id, delta, kind, ref_type, ref_id, actor_id, reason, created_at)
SELECT u.id, u.quota + u.used_quota, ?, '', '', 0, ?, ?
FROM users u
WHERE u.quota + u.used_quota != 0
  AND NOT EXISTS (SELECT 1 FROM quota_ledgers l WHERE l.user_id = u.id)`,
		model.LedgerKindOpeningBalance,
		"WO-017 ledger opening balance",
		time.Now().Unix(),
	).Error
}
