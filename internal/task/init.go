package task

import (
	"context"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/backup"
	"github.com/gypg/lodestar/internal/op/ratelimitstore"
	"github.com/gypg/lodestar/internal/op/relaylog"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/stats"
	"github.com/gypg/lodestar/internal/op/subscription"
	"github.com/gypg/lodestar/internal/price"
	"github.com/gypg/lodestar/internal/relay"
	"github.com/gypg/lodestar/internal/relay/balancer"
	"github.com/gypg/lodestar/internal/utils/log"
)

const (
	TaskPriceUpdate   = "price_update"
	TaskStatsSave     = "stats_save"
	TaskRuntimeState  = "runtime_state_save"
	TaskRelayLogSave  = "relay_log_save"
	TaskSyncLLM       = "sync_llm"
	TaskCleanLLM      = "clean_llm"
	TaskBaseUrlDelay  = "base_url_delay"
	TaskWebDAVBackup  = "webdav_backup"
	TaskErrorLogClean = "error_log_cleanup"
	TaskSubExpire     = "subscription_expire"
	TaskCustomerAlert = "customer_alert"
)

// subExpireInterval 是过期订阅状态回收的周期。
//
// 计费与配额池两处读取点（subscription.GetUserSubscription、
// subscription.activePoolSubscription）的 WHERE 都带 expires_at > now，所以这个
// 任务跑不跑都不影响钱 —— 它修的是 status 列本身：没有它，过期订阅永远停在
// "active"，管理端和用户端的订阅列表都把它渲染成绿色「活跃」徽章
// （web/src/components/modules/subscription/index.tsx:625）。
//
// 一小时是订阅时长里最细的整档（model.SubDurationHour），比它更密没有意义；
// 更疏则按小时售卖的套餐会有大半天显示错误。
const subExpireInterval = time.Hour

func Init() {
	if db.IsSQLite() {
		db.StartSerialWriter(context.Background())
	}
	relaylog.StartFlushWorker(context.Background())

	// 会话指标 worker 必须显式启动，不能放在 relay 包的 init() 里。
	// 它读 relayStreamSessions.shards，而那些 map 由 stream_session.go 中**更晚**的
	// init() 建立（同包 init 按文件名顺序执行），曾因此被 race detector 抓到真实竞态。
	relay.StartSessionMetricsWorker()
	priceUpdateIntervalHours, err := setting.GetInt(model.SettingKeyModelInfoUpdateInterval)
	if err != nil {
		log.Errorf("failed to get model info update interval: %v", err)
	} else {
		priceUpdateInterval := time.Duration(priceUpdateIntervalHours) * time.Hour
		Register(string(model.SettingKeyModelInfoUpdateInterval), priceUpdateInterval, true, func() {
			if err := price.UpdateLLMPrice(context.Background()); err != nil {
				log.Warnf("failed to update price info: %v", err)
			}
		})
	}

	Register(TaskBaseUrlDelay, 1*time.Hour, true, ChannelBaseUrlDelayTask)

	syncLLMIntervalHours, err := setting.GetInt(model.SettingKeySyncLLMInterval)
	if err != nil {
		log.Warnf("failed to get sync LLM interval: %v", err)
	} else {
		syncLLMInterval := time.Duration(syncLLMIntervalHours) * time.Hour
		Register(string(model.SettingKeySyncLLMInterval), syncLLMInterval, true, SyncModelsTask)
	}

	statsSaveIntervalMinutes, err := setting.GetInt(model.SettingKeyStatsSaveInterval)
	if err != nil {
		log.Warnf("failed to get stats save interval: %v", err)
	} else {
		statsSaveInterval := time.Duration(statsSaveIntervalMinutes) * time.Minute
		if db.IsSQLite() {
			Register(TaskStatsSave, statsSaveInterval, false, func() {
				db.EnqueueWrite(db.WriteJob{Name: "stats_save", Fn: func(_ context.Context) error {
					stats.SaveDBTask()
					return nil
				}})
			})
			Register(TaskRuntimeState, statsSaveInterval, false, func() {
				db.EnqueueWrite(db.WriteJob{Name: "runtime_state_save", Fn: func(_ context.Context) error {
					balancer.RuntimeStateSaveDBTask()
					return nil
				}})
			})
		} else {
			Register(TaskStatsSave, statsSaveInterval, false, stats.SaveDBTask)
			Register(TaskRuntimeState, statsSaveInterval, false, balancer.RuntimeStateSaveDBTask)
		}
	}

	Register(TaskRelayLogSave, 10*time.Minute, false, func() {
		// 清理过期的失败提示缓存条目
		relay.PurgeFailureHintCache()

		// 主动清理过期的流会话条目，避免仅依赖惰性触发（见 issue #46 内存暴涨）
		relay.PurgeExpiredStreamSessions()

		// 主动回收 balancer 三个全局 map 中长期空闲的条目。它们的 key 含客户端
		// 请求携带的 modelName（基数不受控），之前只在渠道/Key 删除时清理，缺少
		// 按空闲时长的周期回收，刷量/随机 model 名会导致 map 无界增长（见 issue #46）。
		const balancerIdleThreshold = time.Hour
		balancer.PurgeIdleEntries(balancerIdleThreshold)
		balancer.PurgeIdleStats(balancerIdleThreshold)
		balancer.PurgeIdleSessions(balancerIdleThreshold)

		// 清理限流 bucket 全局 map，防止刷量/随机 model 名导致无界增长
		if n := ratelimitstore.PurgeStaleBuckets(balancerIdleThreshold); n > 0 {
			log.Infof("purged %d stale ratelimit buckets", n)
		}

		if db.IsSQLite() {
			db.EnqueueWrite(db.WriteJob{Name: "relay_log_save", Fn: func(_ context.Context) error {
				return relaylog.RelayLogSaveDBTask(context.Background())
			}})
		} else {
			if err := relaylog.RelayLogSaveDBTask(context.Background()); err != nil {
				log.Warnf("relay log save db task failed: %v", err)
			}
		}
	})

	Register(TaskAlertEvaluate, 60*time.Second, false, EvaluateAlertRules)

	// 错误日志保留策略：超限（5000 条）删最旧一半，每 6 小时检查一次。
	Register(TaskErrorLogClean, 6*time.Hour, false, ErrorLogCleanup)

	// WebDAV cloud backup every 6 hours
	Register(TaskWebDAVBackup, 6*time.Hour, false, func() {
		if err := backup.PerformWebDAVBackup(context.Background()); err != nil {
			log.Warnf("webdav backup failed: %v", err)
		}
	})

	// Site sync task
	siteSyncIntervalHours, err := setting.GetInt(model.SettingKeySiteSyncInterval)
	if err != nil {
		log.Warnf("failed to get site sync interval: %v", err)
	} else {
		siteSyncInterval := time.Duration(siteSyncIntervalHours) * time.Hour
		Register(string(model.SettingKeySiteSyncInterval), siteSyncInterval, true, SiteSyncTask)
	}

	// Site checkin task
	siteCheckinIntervalHours, err := setting.GetInt(model.SettingKeySiteCheckinInterval)
	if err != nil {
		log.Warnf("failed to get site checkin interval: %v", err)
	} else {
		siteCheckinInterval := time.Duration(siteCheckinIntervalHours) * time.Hour
		Register(string(model.SettingKeySiteCheckinInterval), siteCheckinInterval, true, SiteCheckinTask)
	}

	// 过期订阅状态回收。runOnStart：进程重启后立即对账一次，否则一次崩溃就能让
	// 一批过期订阅多顶着「活跃」显示一个周期。
	Register(TaskSubExpire, subExpireInterval, true, ExpireSubscriptionsTask)

	// WO-028 定时模型可用度探测。固定短 tick 无条件注册，任务体每轮自检：
	// model_probe_enabled 关闭（默认）时直接返回——探测是真上游请求（真花钱），
	// 默认开等于替运营者做花钱的决定；周期 model_probe_interval_hours 同样任务体
	// 内生效，改动 setting 无需重启（不像 sync_llm 那类读一次注册定终身）。
	Register(TaskModelProbe, modelProbeTick, false, ModelProbeTask)

	// WO-026 阶段 C 客户预警（低余额/订阅到期，发给客户本人邮箱）。同样固定短
	// tick + 任务体自检：customer_alert_enabled 默认 false，运营者不开就没有任何
	// 邮件出去；防重档位见 internal/op/customeralert。
	Register(TaskCustomerAlert, customerAlertTick, true, CustomerAlertTask)
}

// ExpireSubscriptionsTask 把已过期但 status 仍为 active 的订阅改成 expired。
//
// SQLite 下必须走串行写队列：这是 UPDATE，和 stats/relay_log 那几个写任务一样，
// 直接并发写会撞上 SQLite 的单写者锁。
func ExpireSubscriptionsTask() {
	run := func(ctx context.Context) error {
		n, err := subscription.ExpireDueSubscriptions(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			log.Infof("expired %d due subscription(s)", n)
		}
		return nil
	}

	if db.IsSQLite() {
		db.EnqueueWrite(db.WriteJob{Name: TaskSubExpire, Fn: run})
		return
	}
	if err := run(context.Background()); err != nil {
		log.Warnf("expire due subscriptions failed: %v", err)
	}
}
