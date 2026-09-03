package customeralert

/*
Lodestar — 面向客户本人的低余额 / 订阅到期预警（WO-026 阶段 C）。

与运营者告警（alert.NotifChannelList → 固定收件人）的区别：这是发给客户本人邮箱的。
防重复发送是硬要求：同一用户同一档位只发一封，跨过档位（更低）才再发。

状态存 user_alert_states 表（每用户一行），与内存解耦——重启不丢防重标记，
多字段原子更新走 GORM Updates。邮件发送接口注入（Sender），生产接 email 包，
测试接 spy；低余额与订阅到期两个维度共用同一张状态表的独立列。
*/

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Sender 发一封面向客户的预警邮件。生产实现是 email.SendCustom；测试注入 spy。
type Sender func(to string, subject string, body string) error

// 邮件与内部状态不含 API key、上游地址、渠道信息——只有余额数字/到期时间。
// 阈值 setting：低余额阈值（USD），订阅到期提前天数。均可在后台改，任务体每轮重读。
const (
	SettingKeyLowBalanceThreshold = model.SettingKeyCustomerAlertBalanceThreshold
	SettingKeyExpiryDaysAhead     = model.SettingKeyCustomerAlertExpiryDays
)

// balanceTier 把余额映射成档位：threshold 本身是第一档，之后按半阈值的倍数递减
// （threshold、threshold/2、threshold/4 …）。同一档位内波动不重发；跌穿到更低档位再发。
// 纵深防御（WO-030 缺陷 C 第三层）：非有限/非正阈值直接返 0；循环带硬上限，
// 任何非法输入都不可能挂死任务（+Inf 曾使 tier/2 > 0 恒真）。
func balanceTier(balance, threshold float64) float64 {
	if threshold <= 0 || math.IsNaN(threshold) || math.IsInf(threshold, 0) {
		return 0
	}
	tier := threshold
	for i := 0; i < 128 && balance <= tier/2 && tier/2 > 0; i++ {
		tier /= 2
	}
	return tier
}

// stateOf 懒加载（不存在则零值行，不落库）。
func stateOf(ctx context.Context, userID uint) (*model.UserAlertState, error) {
	var row model.UserAlertState
	err := db.GetDB().WithContext(ctx).Where("user_id = ?", userID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &model.UserAlertState{UserID: userID}, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// saveState 全列更新（行必已存在：调用方先 EnsureRow）。
func saveState(ctx context.Context, st *model.UserAlertState) error {
	st.UpdatedAt = time.Now().Unix()
	return db.GetDB().WithContext(ctx).Save(st).Error
}

// EnsureRow 幂等建行，persist 前保证行存在。sqlite 驱动不翻译 ErrDuplicatedKey，
// 用 OnConflict DoNothing 表达"已存在即可"。
func EnsureRow(ctx context.Context, userID uint) error {
	row := model.UserAlertState{UserID: userID}
	return db.GetDB().WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
}

// LowBalanceThreshold 返回低余额阈值（USD）。未配置或非法（<=0）时返回 0 = 功能关。
// 运行时读取侧防御（WO-030 缺陷 C 第二层）：旧库/缓存/绕过写入口可能给出 NaN/±Inf
// ——Go 浮点比较对 NaN 恒为 false，会穿透一切范围判断；+Inf 会让 balanceTier 的
// 二分不收敛。这里二次拒绝非有限值，不让非法阈值进入 tier 计算。
func LowBalanceThreshold() (float64, error) {
	v, err := setting.GetFloat(model.SettingKeyCustomerAlertBalanceThreshold)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("customer alert balance threshold is not finite (%v) — refusing to run balance alerts", v)
	}
	if v < 0 {
		return 0, fmt.Errorf("customer alert balance threshold must be >= 0, got %v", v)
	}
	return v, nil
}

// ExpiryDaysAhead 返回订阅到期提前预警天数。0 = 到期预警关。
func ExpiryDaysAhead() (int, error) {
	v, err := setting.GetInt(model.SettingKeyCustomerAlertExpiryDays)
	if err != nil {
		return 0, err
	}
	if v < 0 {
		return 0, fmt.Errorf("customer alert expiry days must be >= 0, got %v", v)
	}
	return v, nil
}

// balanceAlertNeeded 判定该用户当前余额是否需要发低余额邮件。
// 返回 (需要发送, 本轮档位)。防重判据：本档位从未发过（LastBalanceTier != tier）。
// 余额回升高于阈值时清档位（结案语义，与 WO-028 探测的 success-reset 同构：
// 下次再跌穿同一阈值要能重新收到一封）。
func balanceAlertNeeded(st *model.UserAlertState, balance, threshold float64) (bool, float64) {
	if threshold <= 0 || balance > threshold {
		if st.LastBalanceTier != 0 {
			st.LastBalanceTier = 0
			st.LastBalanceSentAt = 0
		}
		return false, 0
	}
	tier := balanceTier(balance, threshold)
	if st.LastBalanceTier == tier {
		return false, tier
	}
	return true, tier
}

// expiryAlertNeeded 判定该用户的活跃订阅是否触发到期预警。
// 一个用户多订阅时取最近到期的那条。防重：剩余天数没比上次发送时更紧迫就不发。
//
// "已发送"判据用 LastExpirySentAt != 0（WO-030 缺陷 A）：daysLeft 在最后 24 小时
// 截断为 0，与 LastExpiryDaysSent==0 的"从未发过"哨兵撞值，曾导致每小时重发。
// LastExpiryDaysSent 单独非零只出现在旧版本写入的历史行（已发送但 sentAt 未记），
// 同样视为已发，避免对旧数据多补发一封；days 与 sentAt 双零才是真正的未发送。
// 过期（daysLeft < 0，等待 ExpireSubscriptionsTask 翻状态的小时级窗口）与窗口外
// 都按结案处理：双字段清零，清完必须落库（见 CheckUserExpiry）。
func expiryAlertNeeded(st *model.UserAlertState, daysLeft, daysAhead int) (bool, int64) {
	if daysAhead <= 0 || daysLeft > daysAhead || daysLeft < 0 {
		if st.LastExpirySentAt != 0 || st.LastExpiryDaysSent != 0 {
			st.LastExpiryDaysSent = 0
			st.LastExpirySentAt = 0
		}
		return false, 0
	}
	sentBefore := st.LastExpirySentAt != 0 || st.LastExpiryDaysSent != 0 // 兼容旧行
	if sentBefore && daysLeft >= int(st.LastExpiryDaysSent) {
		return false, int64(daysLeft)
	}
	// 剩余天数比上次发送时更紧迫（如 1 天 → 0 天）：允许再发一封更紧急的提醒，
	// 之后同紧迫度不再重发（0 天轮次 0 >= 0 抑制）。
	return true, int64(daysLeft)
}

// SendFn 各任务侧注入的发送函数；返回错误时**不**落防重标记（下一轮重试，
// 与 WO-028 通知的"发送失败不结案"同语义）。
type SendFn func(ctx context.Context, u *model.User, message string) error

// CheckUserBalance 对单个用户跑低余额检查。成功发送（或无需发送）时持久化防重状态。
func CheckUserBalance(ctx context.Context, u *model.User, send SendFn) error {
	threshold, err := LowBalanceThreshold()
	if err != nil {
		// 非法阈值（NaN/±Inf/负数，第二层防御拦下）视为"该维度停用"而不是错误：
		// 当 error 上抛会让任务每轮 Warn 重试且永不恢复（配置没人改就一直刷）。
		// 与 threshold<=0 同路径安静跳过；写入口的 Validate 会在管理员保存时报错，
		// 两头夹住非法值。
		log.Printf("customer alert: balance threshold invalid (%v), balance alerts disabled this cycle", err)
		return nil
	}
	if threshold <= 0 {
		return nil
	}
	st, err := stateOf(ctx, u.ID)
	if err != nil {
		return err
	}
	prevTier := st.LastBalanceTier
	need, tier := balanceAlertNeeded(st, u.Quota, threshold)
	if !need {
		if st.LastBalanceTier == prevTier {
			return nil // 状态没动
		}
		// 结案清档位：必须落库，否则下轮读回旧档位，再跌穿永远不发（episode 语义）。
		return saveState(ctx, st)
	}
	msg := fmt.Sprintf("您的账户余额已低于 %.2f USD（当前余额 %.2f USD），请及时充值，以免影响使用。", threshold, u.Quota)
	if err := send(ctx, u, msg); err != nil {
		log.Printf("customer alert: balance mail for user %d failed (will retry next cycle): %v", u.ID, err)
		return nil // 发送失败不落标记，下一轮重试
	}
	st.LastBalanceTier = tier
	if err := EnsureRow(ctx, u.ID); err != nil {
		return err
	}
	return saveState(ctx, st)
}

// settleExpiryEpisode 是到期维度的共享结案路径（WO-032）：没有可预警的活跃
// 订阅时——无论因为空列表（生产入口 ListActiveUserSubscriptions 已在查询层滤掉
// 过期/取消行，WO-030 的"非空全不合格"腿够不到这里）、列表全不合格、还是维度
// 关闭——都必须清双标记并落库，否则旧标记会吞掉下一张订阅的预警 episode。
//
// 产品决定（WO-032 2.2）：维度关闭（daysAhead<=0）同样清标记。理由：标记只服务
// 预警 episode 的防重；关着的时候没有任何"正在进行的 episode"可言，留着只会在
// 运营者将来重新打开时吞掉第一批本该发出的预警。清标记的副作用是零（不发信、
// 不建行）。
//
// 纪律：行不存在（从未发过）时 stateOf 返回零值，不 Create、不落库；已有行但
// 标记双零同样不写库——只在真的清了东西时 saveState。
func settleExpiryEpisode(ctx context.Context, userID uint) error {
	st, err := stateOf(ctx, userID)
	if err != nil {
		return err
	}
	if st.LastExpirySentAt == 0 && st.LastExpiryDaysSent == 0 {
		return nil // 从未发过（可能连行都没有）：无事可结，不建行不写库
	}
	st.LastExpiryDaysSent = 0
	st.LastExpirySentAt = 0
	return saveState(ctx, st)
}

// CheckUserExpiry 对单个用户跑订阅到期检查。subs 传入该用户的活跃订阅
// （推荐 ListActiveUserSubscriptions 的结果），但本函数不信任列表：只认
// active 且未过期的行（T-B2 防御），soonest 取最早 ExpiresAt、并列取最小 ID
// （T-B3 稳定 tie-break）——不依赖调用方排序。
func CheckUserExpiry(ctx context.Context, u *model.User, subs []model.UserSubscription, send SendFn) error {
	daysAhead, err := ExpiryDaysAhead()
	if err != nil {
		return err
	}
	// 维度关闭（daysAhead<=0）与空列表（生产入口查询已滤掉不合格行，WO-032）：
	// 都是"没有可预警的活跃订阅"，走同一条结案路径清标记落库。
	if daysAhead <= 0 || len(subs) == 0 {
		return settleExpiryEpisode(ctx, u.ID)
	}
	nowUnix := time.Now().Unix()
	var soonest *model.UserSubscription
	for i := range subs {
		s := &subs[i]
		if s.Status != model.SubStatusActive || s.ExpiresAt <= nowUnix {
			continue
		}
		if soonest == nil || s.ExpiresAt < soonest.ExpiresAt || (s.ExpiresAt == soonest.ExpiresAt && s.ID < soonest.ID) {
			soonest = s
		}
	}
	if soonest == nil {
		// 列表里没有 active 且未过期的行（老订阅过期被翻状态/被取消）：与空列表
		// 同一条结案路径（WO-032）。
		return settleExpiryEpisode(ctx, u.ID)
	}
	daysLeft := int64((time.Until(time.Unix(soonest.ExpiresAt, 0)).Hours()) / 24)
	st, err := stateOf(ctx, u.ID)
	if err != nil {
		return err
	}
	prevDays := st.LastExpiryDaysSent
	prevSentAt := st.LastExpirySentAt
	need, daysLeftSent := expiryAlertNeeded(st, int(daysLeft), daysAhead)
	if !need {
		if st.LastExpiryDaysSent == prevDays && st.LastExpirySentAt == prevSentAt {
			return nil
		}
		// 续期/过期结案清标记：必须落库，否则下轮读回旧标记，episode 语义失效
		// （WO-026 回执实测过的缺陷形态）。
		return saveState(ctx, st)
	}
	msg := fmt.Sprintf("您的订阅（ID %d）将于 %d 天后到期，到期后额度将停止发放，请及时续费。", soonest.ID, daysLeft)
	if err := send(ctx, u, msg); err != nil {
		log.Printf("customer alert: expiry mail for user %d failed (will retry next cycle): %v", u.ID, err)
		return nil
	}
	// 成功才写发送标记：days 记发送时点的剩余天数，sentAt 是"曾发送"的存在性证据
	// （0 天会与未发送哨兵撞值，WO-030 缺陷 A）。两字段必须一起落。
	st.LastExpiryDaysSent = daysLeftSent
	st.LastExpirySentAt = time.Now().Unix()
	if err := EnsureRow(ctx, u.ID); err != nil {
		return err
	}
	return saveState(ctx, st)
}
