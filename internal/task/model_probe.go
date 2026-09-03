package task

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gypg/lodestar/internal/helper"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/alert"
	"github.com/gypg/lodestar/internal/op/channel"
	"github.com/gypg/lodestar/internal/op/modelprobe"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/utils/log"
)

// TaskModelProbe 定时模型可用度探测（WO-028）。
//
// 注册是固定短 tick（modelProbeTick）无条件注册，任务体每轮自检开关与周期：
//   - model_probe_enabled 默认关闭——探测是真上游请求（真花钱），默认开等于替
//     运营者做花钱的决定；关闭时空转一轮（一次分组列表查询的代价）。
//   - model_probe_interval_hours 改动即时生效，无需重启（ unlike sync_llm 那类
//     读一次注册定终身的任务）：每个分组按自己的 LastProbedAt 判 due。
//
// 探测哪些分组：全部分组。本系统"模型广场的一个模型 = 一个分组"
// （GroupListModelCapabilities 按分组名聚合），"全部分组"就是"全部模型"，
// 不存在 分组数×模型数 的乘数放大；一轮请求量 = 有 item 的分组数。
const TaskModelProbe = "model_probe"

const modelProbeTick = 10 * time.Minute

const modelProbeRunTimeout = 30 * time.Minute

const modelProbeGroupTimeout = 5 * time.Minute

const modelProbeDefaultIntervalHours = 2

func ModelProbeTask() {
	ctx, cancel := context.WithTimeout(context.Background(), modelProbeRunTimeout)
	defer cancel()

	enabled, err := setting.GetBool(model.SettingKeyModelProbeEnabled)
	if err != nil || !enabled {
		return
	}
	interval := probeInterval()

	skipGroups, err := modelprobe.SkipGroups()
	if err != nil {
		log.Warnf("model probe: failed to read skip groups (probing without skips): %v", err)
		skipGroups = map[string]struct{}{}
	}

	groups, err := op.GroupList(ctx)
	if err != nil {
		log.Warnf("model probe: failed to list groups: %v", err)
		return
	}

	for i := range groups {
		group := &groups[i]
		if ctx.Err() != nil {
			log.Warnf("model probe: context done, stopping mid-run (remaining groups skip this round)")
			return
		}
		if len(group.Items) == 0 {
			continue
		}
		if _, skipped := skipGroups[strings.ToLower(strings.TrimSpace(group.Name))]; skipped {
			continue
		}
		if !probeDue(modelprobe.LastProbedAt(group.Name), time.Now(), interval) {
			continue
		}

		summary, ok := probeGroupOnce(ctx, group)
		if !ok {
			continue
		}

		// 模型级成败 = 任一 item 通过即算通过（广场语义是"这个模型能不能用"，
		// 多渠道模型单渠道坏不影响可用性；渠道级健康另有渠道测试/熔断器/告警规则管）。
		modelPassed := false
		var lastFailed helper.GroupModelTestResult
		for _, result := range summary.Results {
			if result.Passed {
				modelPassed = true
				break
			}
			lastFailed = result
		}

		if notifyNeeded := modelprobe.RecordProbeResult(ctx, group.Name, modelPassed); notifyNeeded {
			notifyModelProbeFailure(ctx, group.Name, lastFailed)
		}
	}
}

// probeGroupOnce 用同步版入口跑一次分组探测。同步版（TestGroupModels）直接拿返回值，
// 不走进度轮询——异步版的 ClientID + 进度记录路径在本项目出过两次真缺陷，绕开它。
func probeGroupOnce(ctx context.Context, group *model.Group) (*helper.GroupModelTestSummary, bool) {
	channels := make(map[int]model.Channel, len(group.Items))
	for _, item := range group.Items {
		if _, ok := channels[item.ChannelID]; ok {
			continue
		}
		ch, err := channel.Get(item.ChannelID, ctx)
		if err != nil || ch == nil {
			continue
		}
		channels[item.ChannelID] = *ch
	}

	groupCtx, cancel := context.WithTimeout(ctx, modelProbeGroupTimeout)
	defer cancel()
	start := time.Now()
	summary, err := helper.TestGroupModels(groupCtx, group, channels)
	if err != nil {
		// 整轮没跑起来（如分组无有效渠道），不计入模型的成败，也不推进 LastProbedAt
		// ——下一 tick 重试，而不是把"没探成"当成"探过了"。
		log.Warnf("model probe: group %q test errored: %v", group.Name, err)
		return nil, false
	}
	log.Infof("model probe: group %q done, passed=%d/%d took=%s", group.Name, countPassed(summary), summary.Total, time.Since(start).Round(time.Second))
	return summary, true
}

// notifyModelProbeFailure 复用既有告警通道（alert.NotifChannelList + helper.SendNotification），
// 不新建通道。内容只含模型名/失败轮数/错误类别摘要（helper.SanitizeProbeNotifyMessage，
// WO-031）——不带渠道 key、不带上游 base_url；该承诺由摘要函数的类别白名单保证，
// 原始 err 文本（可能含完整 URL 与 ?key= 凭据）只留服务端日志。
func notifyModelProbeFailure(ctx context.Context, groupName string, result helper.GroupModelTestResult) {
	channels, err := alert.NotifChannelList(ctx)
	if err != nil {
		log.Warnf("model probe notify: failed to list notification channels: %v", err)
		return
	}
	if len(channels) == 0 {
		// 没配渠道也算"通知流程已走完"：否则永不配渠道的运营者会让这一失败
		// episode 每轮都走进这里。隐藏状态不受影响（只看计数与阈值）。
		modelprobe.MarkNotified(ctx, groupName)
		return
	}

	message := buildModelProbeNotifyMessage(groupName, result, resolveAlertNotifyLanguage())
	sentAny := false
	for i := range channels {
		payload := helper.AlertWebhookPayload{
			RuleName:      groupName,
			ConditionType: model.AlertRuleConditionType("model_probe"),
			State:         alertStateFiring,
			Message:       message,
			Threshold:     float64(modelprobe.FailThresholdOr(modelProbeFailThresholdDefault)),
			Time:          time.Now().Format(time.RFC3339),
		}
		if err := sendProbeNotification(&channels[i], payload); err != nil {
			// 发送失败不 MarkNotified：下一轮 >= 判定仍在，自动重试。
			log.Warnf("model probe notify: failed to send via %s for group %q: %v", channels[i].Type, groupName, err)
			continue
		}
		sentAny = true
	}
	if sentAny {
		modelprobe.MarkNotified(ctx, groupName)
	}
}

// buildModelProbeNotifyMessage 组装外发通知文本。"最近错误"走 helper 的安全摘要
// （WO-031）：原始 err 文本可能携带完整上游 URL（*url.Error 文案以 URL 开头）与
// Gemini 渠道 key（?key=），任何低层错误原文都不允许出站——外发只有固定类别 +
// 数字，原文保留在服务端日志。
// sendProbeNotification 是通知出站的唯一出口。包级变量是为了 T6 能用 spy 捕获
// "transport 实际收到的 payload"（M6 调用点守卫的钉点：任何绕过 buildModelProbe-
// NotifyMessage 直接拼原文的路径，改这里之外都进不了 transport）。生产零改动。
var sendProbeNotification = helper.SendNotification

func buildModelProbeNotifyMessage(groupName string, result helper.GroupModelTestResult, language string) string {
	threshold := fmt.Sprintf("%d", modelprobe.FailThresholdOr(modelProbeFailThresholdDefault))
	lastErr := helper.SanitizeProbeNotifyMessage(result.Message)
	message := fmt.Sprintf("model %q failed %s consecutive probe rounds; last error: %s", groupName, threshold, lastErr)
	switch normalizeAlertNotifyLanguage(language) {
	case "zh-Hans":
		message = fmt.Sprintf("模型 %q 已连续 %s 轮探测失败；最近错误：%s", groupName, threshold, lastErr)
	case "zh-Hant":
		message = fmt.Sprintf("模型 %q 已連續 %s 輪探測失敗；最近錯誤：%s", groupName, threshold, lastErr)
	}
	return message
}

const modelProbeFailThresholdDefault = 3

// probeDue 判断一个分组这一轮要不要探：从未探过必探；距上次探测 >= 周期才探。
// 抽成纯函数是为了 T-B2 可以直接断言"改 setting 后判定跟着变"。
func probeDue(lastProbedAt time.Time, now time.Time, interval time.Duration) bool {
	if lastProbedAt.IsZero() {
		return true
	}
	return !lastProbedAt.Add(interval).After(now)
}

func probeInterval() time.Duration {
	hours, err := setting.GetInt(model.SettingKeyModelProbeIntervalHours)
	if err != nil || hours < 1 {
		return modelProbeDefaultIntervalHours * time.Hour
	}
	return time.Duration(hours) * time.Hour
}

func countPassed(summary *helper.GroupModelTestSummary) int {
	count := 0
	for _, r := range summary.Results {
		if r.Passed {
			count++
		}
	}
	return count
}
