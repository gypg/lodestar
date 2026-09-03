package task

/*
Lodestar — 客户预警任务（WO-026 阶段 C）：低余额 + 订阅到期，发给客户本人邮箱。

注册模式与 TaskModelProbe 相同：固定短 tick 无条件注册，任务体每轮自检开关——
RUN() 只启动 Init() 时注册过的任务，运行时注册不生效。两个维度一轮扫完：
用户量 × 本地查询 + 0~N 封邮件（SMTP 未配置或用户无邮箱时自然跳过）。
任务体不发探测类上游请求，只有本地读 + SMTP 出站；周期固定 1 小时
（预警不需要更细的粒度，防重逻辑保证同一档位只发一封）。
*/

import (
	"context"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/customeralert"
	"github.com/gypg/lodestar/internal/op/email"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/subscription"
	"github.com/gypg/lodestar/internal/op/user"
	"github.com/gypg/lodestar/internal/utils/log"
)

const customerAlertTick = time.Hour

// CustomerAlertTask 把低余额/到期预警跑一轮。
func CustomerAlertTask() {
	run := func(ctx context.Context) error {
		if enabled, err := setting.GetBool(model.SettingKeyCustomerAlertEnabled); err != nil || !enabled {
			return nil // 默认关闭：给客户发邮件的决定不替运营者做
		}

		send := func(_ context.Context, u *model.User, message string) error {
			if u.Email == "" {
				return nil // 没邮箱的客户无处可发，静默跳过（不算失败，不重试）
			}
			return email.SendCustom(u.Email, "Lodestar 账户提醒", message)
		}

		users, err := user.List(ctx)
		if err != nil {
			return err
		}
		for i := range users {
			u := &users[i]
			if err := customeralert.CheckUserBalance(ctx, u, send); err != nil {
				log.Warnf("customer alert: balance check for user %d failed: %v", u.ID, err)
			}
			// WO-030 缺陷 B：必须传全部活跃订阅。GetUserSubscription 是 DESC 单条
			// （给 UI 视图用的），预警要最早到期那条——多条活跃订阅真实可达
			// （三条创建路径都是无条件 Create，user_id 非唯一键）。
			subs, err := subscription.ListActiveUserSubscriptions(u.ID, ctx)
			if err != nil {
				log.Warnf("customer alert: subscriptions for user %d failed: %v", u.ID, err)
				continue
			}
			if err := customeralert.CheckUserExpiry(ctx, u, subs, send); err != nil {
				log.Warnf("customer alert: expiry check for user %d failed: %v", u.ID, err)
			}
		}
		return nil
	}

	if db.IsSQLite() {
		db.EnqueueWrite(db.WriteJob{Name: TaskCustomerAlert, Fn: run})
		return
	}
	if err := run(context.Background()); err != nil {
		log.Warnf("customer alert task failed: %v", err)
	}
}
