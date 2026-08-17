package relay

/*
429 渠道内延时重试（rate limit hold）。

在"少 key / 贵 key"场景下，429 时"同一渠道等一会儿再试"比立刻换 Key/渠道
更省——省 key 切换、降低 429 烧键。默认关闭，行为与历史完全一致：
429 立刻换 Key / 渠道（ratelimit_cooldown 冷却照旧生效）。

只对「Code == 429 且 Scope == ScopeSameChannel」的失败生效。400 这类
ScopeNone 终态错误（terminal_error.go 的 R-3 契约）不经过这里，也绝不能
被 hold 拦住；401/403/空输出等其它 ScopeSameChannel 失败保持立即换 Key。

实现照抄 octopus 的可中断版本：等待必须 select ctx.Done()，客户端断连
立刻返回 false。上游踩过裸 time.Sleep 不可中断的坑，这里不回退。
*/

import (
	"context"
	"time"

	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/utils/log"
)

const (
	defaultRateLimitHoldIntervalSec = 10
	defaultRateLimitHoldMaxWaitSec  = 60
)

// rateLimitHoldConfig 描述「渠道内 429 延时重试」策略。
type rateLimitHoldConfig struct {
	Enabled  bool
	Interval time.Duration
	MaxWait  time.Duration
}

func getRateLimitHoldConfig() rateLimitHoldConfig {
	cfg := rateLimitHoldConfig{
		Enabled:  false,
		Interval: time.Duration(defaultRateLimitHoldIntervalSec) * time.Second,
		MaxWait:  time.Duration(defaultRateLimitHoldMaxWaitSec) * time.Second,
	}

	if v, err := setting.GetBool(dbmodel.SettingKeyRateLimitHoldEnabled); err == nil {
		cfg.Enabled = v
	}
	if v, err := setting.GetInt(dbmodel.SettingKeyRateLimitHoldInterval); err == nil && v > 0 {
		cfg.Interval = time.Duration(v) * time.Second
	}
	if v, err := setting.GetInt(dbmodel.SettingKeyRateLimitHoldMaxWait); err == nil && v > 0 {
		cfg.MaxWait = time.Duration(v) * time.Second
	}
	// 间隔不应超过总等待上限，否则一次等待就会直接耗尽预算。
	if cfg.Interval > cfg.MaxWait {
		cfg.Interval = cfg.MaxWait
	}
	return cfg
}

// shouldHoldOnRateLimit 判断本次失败是否应进入「当前渠道内延时重试」。
// 仅对真正的 429 生效；其它 ScopeSameChannel（401/403/空输出）保持原立即换 Key。
func shouldHoldOnRateLimit(cfg rateLimitHoldConfig, decision RetryDecision) bool {
	return cfg.Enabled && decision.Scope == ScopeSameChannel && decision.Code == 429
}

// canContinueRateLimitHold 在累计等待 waited 后是否还能再等一轮 interval。
// 当剩余预算不足一整轮 interval 时停止 hold，转正常的换 Key/渠道流程。
func canContinueRateLimitHold(cfg rateLimitHoldConfig, waited time.Duration) bool {
	if !cfg.Enabled || cfg.Interval <= 0 || cfg.MaxWait <= 0 {
		return false
	}
	return waited+cfg.Interval <= cfg.MaxWait
}

// waitRateLimitHold 阻塞等待下一轮 429 重试，同时响应客户端/操作上下文取消。
// 返回 true 表示等待完成可继续重试；false 表示上下文已取消，应放弃本次请求。
// 上游曾用裸 time.Sleep 导致客户端断连后白等满整个间隔，这里必须保持可中断。
func waitRateLimitHold(ctx context.Context, cfg rateLimitHoldConfig, channelName string, waited time.Duration) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	remainingBudget := cfg.MaxWait - waited
	waitFor := cfg.Interval
	// 剩余预算不足一整轮 interval 时只等剩余部分；预算已耗尽（<=0）时
	// waitFor 也被压到 0，直接返回 false —— 调用方有 canContinueRateLimitHold
	// 预检，这里是第二道守卫，保证函数独立调用也不会越界等待。
	if remainingBudget < waitFor {
		waitFor = remainingBudget
	}
	if waitFor <= 0 {
		return false
	}

	log.Infof("rate limit hold: channel=%s wait=%s elapsed=%s max=%s",
		channelName, waitFor, waited.Round(time.Millisecond), cfg.MaxWait)

	timer := time.NewTimer(waitFor)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
