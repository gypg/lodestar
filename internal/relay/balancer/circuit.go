package balancer

import (
	"fmt"
	"math/bits"
	"strings"
	"sync"
	"time"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/utils/log"
)

// CircuitState 熔断器状态
type CircuitState int

const (
	StateClosed   CircuitState = iota // 正常通行
	StateOpen                         // 熔断中，拒绝所有请求
	StateHalfOpen                     // 半开，仅允许单个试探请求
)

// circuitEntry 单个熔断器条目
type circuitEntry struct {
	State               CircuitState
	ConsecutiveFailures int64
	LastFailureTime     time.Time
	TripCount           int // 累计熔断触发次数（用于指数退避）
	mu                  sync.Mutex
}

// 全局熔断器存储
var globalBreaker sync.Map // key: string -> value: *circuitEntry

// circuitKey 生成熔断器键：channelID:channelKeyID:modelName
func circuitKey(channelID, keyID int, modelName string) string {
	return fmt.Sprintf("%d:%d:%s", channelID, keyID, modelName)
}

// getOrCreateEntry 获取或创建熔断器条目
func getOrCreateEntry(key string) *circuitEntry {
	if v, ok := globalBreaker.Load(key); ok {
		if entry, ok := v.(*circuitEntry); ok {
			return entry
		}
	}
	entry := &circuitEntry{State: StateClosed}
	actual, _ := globalBreaker.LoadOrStore(key, entry)
	if e, ok := actual.(*circuitEntry); ok {
		return e
	}
	return entry
}

// getThreshold 获取熔断阈值配置
func getThreshold() int64 {
	v, err := setting.GetInt(model.SettingKeyCircuitBreakerThreshold)
	if err != nil || v <= 0 {
		return 5
	}
	return int64(v)
}

// GetCooldown 获取当前冷却时间（带指数退避）
func GetCooldown(tripCount int) time.Duration {
	base, err := setting.GetInt(model.SettingKeyCircuitBreakerCooldown)
	if err != nil || base <= 0 {
		base = 60
	}
	maxCooldown, err := setting.GetInt(model.SettingKeyCircuitBreakerMaxCooldown)
	if err != nil || maxCooldown <= 0 {
		maxCooldown = 600
	}

	// 指数退避：baseCooldown * 2^(tripCount-1)
	cooldown := base
	if tripCount > 1 {
		shift := tripCount - 1
		if shift > 20 { // 防止过大的位移
			shift = 20
		}
		// 防止 base << shift 溢出 int：若 base 的二进制位数加上 shift
		// 超过 int 的位宽，左移会溢出产生负值，直接使用最大冷却时间。
		if base > 0 && shift >= bits.Len(uint(base)) {
			cooldown = maxCooldown
		} else {
			cooldown = base << shift
		}
	}
	if cooldown > maxCooldown {
		cooldown = maxCooldown
	}

	return time.Duration(cooldown) * time.Second
}

// IsTripped 检查通道是否处于熔断状态
// 返回 tripped=true 表示该通道应被跳过，remaining 为剩余冷却时间
func IsTripped(channelID, keyID int, modelName string) (tripped bool, remaining time.Duration) {
	key := circuitKey(channelID, keyID, modelName)
	v, ok := globalBreaker.Load(key)
	if !ok {
		return false, 0 // 无记录，视为 Closed
	}
	entry, ok := v.(*circuitEntry)
	if !ok {
		return false, 0
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	switch entry.State {
	case StateClosed:
		return false, 0

	case StateOpen:
		cooldown := GetCooldown(entry.TripCount)
		elapsed := time.Since(entry.LastFailureTime)
		if elapsed >= cooldown {
			entry.State = StateHalfOpen
			log.Infof("circuit breaker [%s] Open -> HalfOpen (cooldown %v elapsed)", key, cooldown)
			return false, 0
		}
		// 仍在冷却中
		return true, cooldown - elapsed

	case StateHalfOpen:
		// 已有试探请求在进行中，拒绝其他请求
		return true, 0

	default:
		return false, 0
	}
}

// RecordSuccess 记录成功，重置熔断器状态
func RecordSuccess(channelID, keyID int, modelName string) {
	key := circuitKey(channelID, keyID, modelName)
	v, ok := globalBreaker.Load(key)
	if !ok {
		return
	}
	entry, ok := v.(*circuitEntry)
	if !ok {
		return
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.State == StateHalfOpen {
		log.Infof("circuit breaker [%s] HalfOpen -> Closed (probe succeeded)", key)
	}

	// 重置全部状态
	entry.State = StateClosed
	entry.ConsecutiveFailures = 0
	entry.TripCount = 0
}

// RecordFailure 记录失败，可能触发熔断
func RecordFailure(channelID, keyID int, modelName string) {
	key := circuitKey(channelID, keyID, modelName)
	entry := getOrCreateEntry(key)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	switch entry.State {
	case StateClosed:
		entry.LastFailureTime = time.Now()
		entry.ConsecutiveFailures++
		threshold := getThreshold()
		if entry.ConsecutiveFailures >= threshold {
			entry.State = StateOpen
			entry.TripCount++
			log.Warnf("circuit breaker [%s] Closed -> Open (failures=%d >= threshold=%d, tripCount=%d, cooldown=%v)",
				key, entry.ConsecutiveFailures, threshold, entry.TripCount, GetCooldown(entry.TripCount))
		}

	case StateHalfOpen:
		// 试探失败，重新进入 Open 状态，TripCount 递增（冷却时间翻倍）
		entry.LastFailureTime = time.Now()
		entry.State = StateOpen
		entry.TripCount++
		entry.ConsecutiveFailures = 0 // 重新开始计数
		log.Warnf("circuit breaker [%s] HalfOpen -> Open (probe failed, tripCount=%d, cooldown=%v)",
			key, entry.TripCount, GetCooldown(entry.TripCount))

	case StateOpen:
		// Open 状态下不更新 LastFailureTime，避免冷却计时器被反复重置
		// 导致熔断器无法转为 HalfOpen。
	}
}

// RemoveChannelEntries 删除指定渠道的所有熔断器条目。
// 在渠道被删除时调用，防止 globalBreaker map 无限增长。
func RemoveChannelEntries(channelID int) {
	prefix := fmt.Sprintf("%d:", channelID)
	globalBreaker.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok && len(k) > 0 && strings.HasPrefix(k, prefix) {
			globalBreaker.Delete(key)
		}
		return true
	})
}

// PurgeIdleEntries 删除空闲时长超过 idleFor 的熔断器条目。globalBreaker 的 key 含
// 客户端请求携带的 modelName（基数不受控），缺少按空闲时长的周期回收会导致 map
// 无界增长（见 issue #46）。仅回收处于 Closed 且最近一次失败已超过 idleFor 的条目，
// 不动 Open/HalfOpen 状态（它们正在主动保护上游）。返回删除的条目数。
func PurgeIdleEntries(idleFor time.Duration) int {
	if idleFor <= 0 {
		return 0
	}
	now := time.Now()
	removed := 0
	globalBreaker.Range(func(key, value any) bool {
		entry, ok := value.(*circuitEntry)
		if !ok {
			globalBreaker.Delete(key)
			removed++
			return true
		}
		entry.mu.Lock()
		state := entry.State
		last := entry.LastFailureTime
		entry.mu.Unlock()
		// 仅清理 Closed 状态的条目：Open/HalfOpen 正在保护上游，不能丢弃。
		// LastFailureTime 为零值（从未失败）或已超过空闲阈值的 Closed 条目可回收。
		if state == StateClosed && (last.IsZero() || now.Sub(last) >= idleFor) {
			globalBreaker.Delete(key)
			removed++
		}
		return true
	})
	return removed
}
