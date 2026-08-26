package user

/*
WO-017 — 只读对账。

不变式：`users.quota == Σ(quota_ledger.delta) - users.used_quota`

左边是当前余额，右边是"历史上一共入账多少，减去累计消耗多少"。两边对不上（漂移）只有
两种可能，都需要人工介入：
  - 有一笔余额改动绕过了漏斗（漏斗之外又出现了直接写 quota 的地方）；
  - 流水写了但余额没落地（事务被拆开过）。

用量结算（SettleUsage）不进流水是刻意的，等式仍然闭合 —— 它对 quota 和 used_quota
用同一个 amount，右边一减一加正好抵消。详见 model/quota_ledger.go 的注释。
*/

import (
	"context"

	"github.com/gypg/lodestar/internal/db"
)

// ReconcileTolerance 是判定漂移的容差。
//
// 必须有容差：quota 是 float，一串加减之后 45 可能存成 44.999999999999993。
// 用 `drift != 0` 会把每个活跃用户都报成漂移，真正的漂移淹没在噪音里，对账接口等于废掉。
// 实测量级：0.1 累加十次的误差是 1.1e-16，比这个容差小七个数量级。
const ReconcileTolerance = 1e-9

// QuotaDrift 是一个对不上账的用户。
type QuotaDrift struct {
	UserID    uint    `json:"user_id"`
	Username  string  `json:"username"`
	Quota     float64 `json:"quota"`
	UsedQuota float64 `json:"used_quota"`
	LedgerSum float64 `json:"ledger_sum"`

	// Drift = quota - (Σdelta - used_quota)。0 表示对上账。
	// 正数：余额比流水能解释的多（凭空多出来的钱）；负数：少。
	Drift float64 `json:"drift"`
}

// ReconcileDrifts 返回所有对不上不变式的用户，漂移绝对值大的排在前面。
// 全部对上时返回空切片而不是 nil —— 调用方直接序列化成 JSON 的 `[]`。
//
// LEFT JOIN 而不是 INNER：没有任何流水行的用户 Σdelta 记作 0，他们恰恰是最需要被
// 报出来的（要么该开账没开，要么余额来源不明）。INNER JOIN 会把这类用户整个漏掉。
func ReconcileDrifts(ctx context.Context) ([]QuotaDrift, error) {
	// drift 的表达式在 SELECT / HAVING / ORDER BY 里出现三次。写成常量避免三处漂移 ——
	// 其中任意一处与另外两处不一致，就会出现"报出来的行和排序依据算的不是同一个数"。
	const driftExpr = "u.quota - (COALESCE(SUM(l.delta), 0) - u.used_quota)"

	rows := make([]QuotaDrift, 0)
	err := db.GetDB().WithContext(ctx).
		Table("users u").
		Select(`u.id AS user_id,
			u.username AS username,
			u.quota AS quota,
			u.used_quota AS used_quota,
			COALESCE(SUM(l.delta), 0) AS ledger_sum,
			`+driftExpr+` AS drift`).
		Joins("LEFT JOIN quota_ledgers l ON l.user_id = u.id").
		Group("u.id, u.username, u.quota, u.used_quota").
		// 在 DB 侧过滤：漂移是异常，正常情况下这里返回零行，不必把整张用户表拉进内存。
		Having("ABS("+driftExpr+") > ?", ReconcileTolerance).
		Order("ABS(" + driftExpr + ") DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
