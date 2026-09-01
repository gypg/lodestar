package user

/*
WO-017 — users.quota 离散变动的唯一入口。

在此之前余额变动散落在 5 个包里各写裸 SQL（epay / Stripe / 兑换码 / 管理员调整 /
订阅购买），没有任何一处留下"谁改的、为什么改"。管理员给用户加钱和扣钱完全无痕，
用户争议（"我充了 10 块只到账 5 块"）无法查证。

漏斗保证两件事：
 1. 余额更新与流水行**在同一个事务内** —— 否则会出现"钱到账了但无痕"或"有痕但钱没到"。
 2. 非有限值（NaN / ±Inf）在唯一入口被拦掉。此前 AddQuota 靠 JSON decoder 拒绝 NaN
    字面量挡着，那是调用方的巧合而不是守卫：一旦出现 strconv.ParseFloat 之类的第二个
    调用来源就破（ParseFloat 接受 "NaN"）。`quota + NaN` 会永久毒化该列 —— 之后每个
    `remaining > 0` 都是 false，账户锁死且任何充值都无法在算术上修复。

用量结算（SettleUsage）**刻意不走这里**，理由见 model/quota_ledger.go 的注释。
*/

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"

	"gorm.io/gorm"
)

// ErrInsufficientBalance 表示带 RequireAffordable 的扣款因余额不足未发生。
var ErrInsufficientBalance = errors.New("insufficient balance")

// ErrMissingLedgerKind 表示调用方没有给出事件类型。
// 拒绝而不是填默认值：一条 kind 为空的流水在对账时无法归类，等于没记。
var ErrMissingLedgerKind = errors.New("ledger entry kind is required")

// LedgerEntry 描述一次余额变动的"为什么"。
type LedgerEntry struct {
	// Kind 必填，取 model.LedgerKind* 常量。
	Kind string

	RefType string
	RefID   string

	// ActorID 是**操作者**（管理员的 user_id）；网关回调与系统开账填 0。
	// 填成受益人 ID 等于审计失效 —— 那样查不出是谁动的手。
	ActorID uint

	Reason string

	// RequireAffordable 仅对负 delta 有意义：为 true 时扣款带原子
	// `WHERE quota >= -delta` 守卫，余额不足返回 ErrInsufficientBalance 且分毫不动。
	//
	// 这个守卫（而不是 read-then-check）是并发安全的关键：余额 10 时两个并发的
	// 价格 8 的购买只能成功一个。购买用 true；用户已消耗的欠款和管理员纠错用 false
	// —— 后两者必须允许余额变负，否则已交付的成本会被静默丢弃。
	RequireAffordable bool
}

// MutateQuota 原子地调整用户余额并写入一条流水。
//
// tx 非 nil 时复用调用方事务（多数调用点已在事务内，必须复用而不是另开，否则
// 支付订单置成 success 与余额入账会落在两个事务里）；tx 为 nil 时自开事务。
//
// delta 有符号：正数入账，负数出账。
func MutateQuota(tx *gorm.DB, userID uint, delta float64, e LedgerEntry, ctx context.Context) error {
	if math.IsNaN(delta) || math.IsInf(delta, 0) {
		return fmt.Errorf("%w: %v", ErrNonFiniteAmount, delta)
	}
	if e.Kind == "" {
		return ErrMissingLedgerKind
	}
	if delta == 0 {
		// 零额变动既不改余额也不留流水 —— 记一行 delta=0 只是噪音。
		return nil
	}

	apply := func(tx *gorm.DB) error {
		q := tx.Model(&model.User{}).Where("id = ?", userID)
		guarded := e.RequireAffordable && delta < 0
		if guarded {
			q = q.Where("quota >= ?", -delta)
		}

		res := q.Update("quota", gorm.Expr("quota + ?", delta))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			if guarded {
				// 余额不足，或用户行不存在。两者都用余额不足报出去 —— 这沿用了
				// 收束前订阅购买的行为，且再查一次 users 来区分并不值得：调用方
				// 对这两种情况的处理是一样的（拒绝本次购买）。
				return ErrInsufficientBalance
			}
			return ErrUserNotFound
		}

		return tx.Create(&model.QuotaLedger{
			UserID:    userID,
			Delta:     delta,
			Kind:      e.Kind,
			RefType:   e.RefType,
			RefID:     e.RefID,
			ActorID:   e.ActorID,
			Reason:    e.Reason,
			CreatedAt: time.Now().Unix(),
		}).Error
	}

	if tx != nil {
		return apply(tx)
	}
	return db.GetDB().WithContext(ctx).Transaction(apply)
}

// ledgerSum 返回某用户流水 delta 的合计。
//
// 刻意不导出：生产侧的对账走 ReconcileDrifts（一条 SQL 扫全表），单用户求和目前只有
// 本包测试在用（算 Σdelta 验不变式）。导出一个没人调的函数就是下一轮死代码扫描的条目。
// 将来真要做"某用户的流水明细"接口，连同分页一起提上去再导出。
func ledgerSum(userID uint, ctx context.Context) (float64, error) {
	var sum *float64
	err := db.GetDB().WithContext(ctx).
		Model(&model.QuotaLedger{}).
		Where("user_id = ?", userID).
		Select("SUM(delta)").
		Scan(&sum).Error
	if err != nil {
		return 0, err
	}
	if sum == nil {
		return 0, nil // 无流水行：SUM 返回 NULL，不是 0
	}
	return *sum, nil
}

// ListByUser returns a page of the given user's own ledger entries, newest first.
//
// 多租户隔离在 op 层做：调用方（handler）只传 uid，不会因为忘记加 WHERE user_id
// 而泄露跨用户流水 —— 这是 WO-026 阶段 A 的核心安全要求，参照 apikey.ListByUser 的
// 同一模式。handler 不直接查 quota_ledgers 表。
//
// 分页与既有端点（audit.List / errorlog.List）一致：page 从 1 起、page_size 默认 20、
// 上限 100。返回 newest-first（按 created_at DESC, id DESC）——客户最近一笔在最上面。
// 不返回 total：与既有分页端点风格一致，前端按“返回条数 < page_size 判断末页”。
func ListByUser(userID uint, page, pageSize int, ctx context.Context) ([]model.QuotaLedger, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	entries := make([]model.QuotaLedger, 0, pageSize)
	err := db.GetDB().WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC, id DESC").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Find(&entries).Error
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// 对账（不变式校验）在 reconcile.go。
