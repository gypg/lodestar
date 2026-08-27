package model

/*
Lodestar commercial layer — 余额流水（quota ledger）。

记录 users.quota 的每一笔**离散**变动：开账 / 充值 / 兑换 / 管理员调整 / 订阅购买。
刻意**不记**每请求的用量结算 —— 那在热路径上每请求一次，且 users.used_quota 已经精确
累计了累计钱包消耗（SettleUsage 是它的唯一写入者，对 quota 和 used_quota 用同一个
amount）。因此不变式仍然闭合：

	users.quota == Σ(quota_ledger.delta) - users.used_quota

订阅池的消耗走 SubscriptionOrder.amount_used、不进 used_quota，所以不影响这条等式
（见 op/subscription/pool.go 的 "closed wallet ledger" 注释）。

用途：对账（GET /api/v1/wallet/reconcile）与纠错追溯 —— 管理员调整余额必须留下
"谁、何时、给谁、多少、为什么"。
*/

// QuotaLedger 是一条不可变的余额变动记录。写入后不应被 UPDATE。
type QuotaLedger struct {
	ID      int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID  uint    `json:"user_id" gorm:"index;not null"`
	Delta   float64 `json:"delta" gorm:"type:double precision;not null"` // 有符号：+ 入账，− 出账
	Kind    string  `json:"kind" gorm:"type:varchar(32);index;not null"`
	RefType string  `json:"ref_type" gorm:"type:varchar(32)"` // 关联单据类型，可空
	RefID   string  `json:"ref_id" gorm:"type:varchar(64);index"`

	// ActorID 是**操作者**，不是受益人。管理员调整时填管理员的 user_id；
	// 网关回调 / 系统开账填 0。把它填成受益人等于审计失效。
	ActorID uint `json:"actor_id" gorm:"index"`

	Reason    string `json:"reason" gorm:"type:varchar(255)"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index;autoCreateTime"`
}

func (QuotaLedger) TableName() string { return "quota_ledgers" }

// 流水事件类型。新增类型必须在这里定义常量 —— 不要在调用点散落字符串字面量，
// 否则 kind 拼错不会有任何信号（DB 照样收下），对账时才发现分类是错的。
const (
	// LedgerKindOpeningBalance 是流水表上线时给存量用户补的开账行。
	// delta = quota + used_quota（不是 quota）—— 已消耗的部分也属于历史入账，
	// 否则存量用户第一天就对不上账。
	LedgerKindOpeningBalance = "opening_balance"

	LedgerKindTopupEpay   = "topup_epay"
	LedgerKindTopupStripe = "topup_stripe"
	LedgerKindRedeem      = "redeem"

	// LedgerKindAdminAdjust 覆盖管理员的加款与扣款（delta 有符号）。
	// 这是唯一的人工纠错手段：退款到支付网关走网关后台，余额侧用一条负 delta 平账。
	LedgerKindAdminAdjust = "admin_adjust"

	LedgerKindSubscriptionPurchase = "subscription_purchase"
)

// 关联单据类型。
const (
	LedgerRefPaymentOrder     = "payment_order"
	LedgerRefTopupCode        = "topup_code"
	LedgerRefSubscriptionPlan = "subscription_plan"
)
