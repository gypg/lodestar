package modelprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/utils/log"
)

// modelprobe 把 model_probe_states 表缓存进进程内存：
// 探测任务每轮读写、market/capabilities 请求频繁读，逐行查表没必要。
// 写路径始终"先改内存、再落库"（SQLite 走串行写队列）；读路径惰性加载，
// LoadFromDB 供启动时显式恢复（balancer.LoadRuntimeState 同款三段式）。
// 计数必须跨重启保留，否则一次重启就永远凑不满"连续 N 轮失败"（工单 3.3 硬要求）。
var packageState struct {
	mu     sync.RWMutex
	states map[string]*model.ModelProbeState
	loaded bool
}

func init() {
	packageState.states = make(map[string]*model.ModelProbeState)
}

// ensureLoaded 在持有写锁时惰性从库加载。读接口（HiddenSnapshot/LastProbedAt）
// 与写接口（RecordProbeResult）都要先过这里，否则进程启动后未经 LoadFromDB 的
// 路径会拿空 map 做判定。
func ensureLoadedLocked() {
	if packageState.loaded {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := loadRows(ctx)
	if err != nil {
		// 读库失败时按空表继续：探测状态缺失只会让模型显示为"可用"（回退
		// 到探测引入前的行为），不该阻塞读路径。下一轮 RecordProbeResult
		// 重写状态行时自然修正。
		log.Warnf("model probe: lazy load failed, treating as empty: %v", err)
	}
	states := make(map[string]*model.ModelProbeState, len(rows))
	for i := range rows {
		states[rows[i].GroupName] = &rows[i]
	}
	packageState.states = states
	packageState.loaded = true
}

func loadRows(ctx context.Context) ([]model.ModelProbeState, error) {
	conn := db.GetDB()
	if conn == nil {
		return nil, nil
	}
	var rows []model.ModelProbeState
	if err := conn.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to load model probe states: %w", err)
	}
	return rows, nil
}

// LoadFromDB 启动时把持久化的探测状态恢复进内存。失败返回 error 由调用方决定
// 是否致命（cmd/start.go 目前记 warning 继续）。
func LoadFromDB(ctx context.Context) error {
	rows, err := loadRows(ctx)
	if err != nil {
		return err
	}

	packageState.mu.Lock()
	defer packageState.mu.Unlock()
	states := make(map[string]*model.ModelProbeState, len(rows))
	for i := range rows {
		states[rows[i].GroupName] = &rows[i]
	}
	packageState.states = states
	packageState.loaded = true
	log.Infof("model probe state loaded: %d group(s)", len(rows))
	return nil
}

// Invalidate 丢弃内存缓存，下次访问时从库重载。测试模拟"重启"用；
// 运行期探测状态只由探测任务这一处写，生产上无需主动失效。
func Invalidate() {
	packageState.mu.Lock()
	defer packageState.mu.Unlock()
	packageState.loaded = false
	packageState.states = make(map[string]*model.ModelProbeState)
}

// IsEnabled 读取探测开关（默认关闭）。关闭时一切探测标记退回"可用"。
func IsEnabled() bool {
	v, err := setting.GetBool(model.SettingKeyModelProbeEnabled)
	return err == nil && v
}

// HiddenSnapshot 返回当前达到"连续失败阈值"的分组：小写分组名 → 最近一次探测时刻。
// 探测关闭（默认）时返回 nil——所有模型视为可用，呈现层回退到探测引入前的行为。
// market 与 capabilities 两个回灌点共用这一份判定，避免阈值口径漂移。
func HiddenSnapshot() map[string]time.Time {
	if !IsEnabled() {
		return nil
	}
	threshold, err := FailThreshold()
	if err != nil {
		return nil
	}

	packageState.mu.Lock()
	defer packageState.mu.Unlock()
	ensureLoadedLocked()
	hidden := make(map[string]time.Time)
	for name, st := range packageState.states {
		if st.ConsecutiveFailures >= threshold {
			hidden[strings.ToLower(name)] = st.LastProbedAt
		}
	}
	return hidden
}

// FailThreshold 返回连续失败阈值 setting（默认 3，工单 3.3 要求可配置）。
func FailThreshold() (int, error) {
	v, err := setting.GetInt(model.SettingKeyModelProbeFailThreshold)
	if err != nil {
		return 0, err
	}
	if v < 1 {
		return 1, nil
	}
	return v, nil
}

// FailThresholdOr 是 FailThreshold 的容错版：读不到配置时用 fallback
// （通知文案/payload 组装这类展示路径不允许因配置缺失而中断）。
func FailThresholdOr(fallback int) int {
	v, err := FailThreshold()
	if err != nil {
		return fallback
	}
	return v
}

// LastProbedAt 返回分组的上次探测时间；从未探测过返回零值。
func LastProbedAt(groupName string) time.Time {
	packageState.mu.Lock()
	defer packageState.mu.Unlock()
	ensureLoadedLocked()
	st, ok := packageState.states[strings.TrimSpace(groupName)]
	if !ok {
		return time.Time{}
	}
	return st.LastProbedAt
}

// SkipGroups 返回人工跳过列表（小写分组名集合）。跳过 = 不探测、不计数、不通知
// （工单 3.5 人工覆盖位；与"广场隐藏"是两个独立维度，互不读写）。
func SkipGroups() (map[string]struct{}, error) {
	raw, err := setting.GetString(model.SettingKeyModelProbeSkipGroups)
	if err != nil {
		return nil, err
	}
	var names []string
	trimmed := strings.TrimSpace(raw)
	if trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &names); err != nil {
			return nil, fmt.Errorf("setting %s is not a JSON string array: %w", model.SettingKeyModelProbeSkipGroups, err)
		}
	}
	skipped := make(map[string]struct{}, len(names))
	for _, n := range names {
		if key := strings.ToLower(strings.TrimSpace(n)); key != "" {
			skipped[key] = struct{}{}
		}
	}
	return skipped, nil
}

// RecordProbeResult 写一轮探测结果：成功归零计数（对称性，工单 3.3），失败 +1。
// 返回 true 表示"这一失败 episode 尚未通知过且已跨过阈值"，调用方据此发通知；
// 通知确认送达后由调用方调 MarkNotified（防重收口，工单 3.4）。
func RecordProbeResult(ctx context.Context, groupName string, passed bool) (notify bool) {
	name := strings.TrimSpace(groupName)
	if name == "" {
		return false
	}

	threshold, err := FailThreshold()
	if err != nil {
		log.Warnf("model probe: failed to read fail threshold, skip counting: %v", err)
		return false
	}

	packageState.mu.Lock()
	ensureLoadedLocked()
	st, ok := packageState.states[name]
	if !ok {
		st = &model.ModelProbeState{GroupName: name}
		packageState.states[name] = st
	}
	if passed {
		st.ConsecutiveFailures = 0
		// 成功即结案：新一轮失败episode必须能重新触发通知，旧标记不能吞掉它。
		st.LastNotifiedFails = 0
	} else {
		st.ConsecutiveFailures++
	}
	st.LastProbedAt = time.Now()
	st.UpdatedAt = time.Now()

	if !passed && st.ConsecutiveFailures >= threshold && st.LastNotifiedFails < threshold {
		// 判据是 >= threshold 且本 episode 尚未成功通知过。不能用 == threshold：
		// 通知发送失败时计数下一轮就涨过阈值，== 判定会让重试永远轮空。
		notify = true
	}
	snapshot := *st
	packageState.mu.Unlock()

	persist(ctx, &snapshot)
	return notify
}

// MarkNotified 在通知确认送达后调用。发送失败不调用——下一轮 RecordProbeResult
// 的 >= && LastNotifiedFails < 判定仍在，自动重试。
func MarkNotified(ctx context.Context, groupName string) {
	name := strings.TrimSpace(groupName)
	packageState.mu.Lock()
	ensureLoadedLocked()
	st, ok := packageState.states[name]
	if !ok {
		packageState.mu.Unlock()
		return
	}
	st.LastNotifiedFails = st.ConsecutiveFailures
	snapshot := *st
	packageState.mu.Unlock()

	persist(ctx, &snapshot)
}

// RewindLastProbedForTest 把分组的 LastProbedAt 回拨到 now-d（仅供测试）：
// 定时任务的时间维度（probeDue）无法在测试里真等 2 小时，测试用它模拟"周期流逝"。
// 生产代码不得调用。
func RewindLastProbedForTest(groupName string, d time.Duration) {
	packageState.mu.Lock()
	defer packageState.mu.Unlock()
	st, ok := packageState.states[strings.TrimSpace(groupName)]
	if !ok {
		return
	}
	st.LastProbedAt = st.LastProbedAt.Add(-d)
	st.UpdatedAt = time.Now()
}

// persist 落库（同步直写，alert 任务同款先例：StateSet/HistoryAdd 也不走写队列）。
// upsert：存在则更新计数字段，不存在则插入。
//
// 不走 SQLite 串行写队列的理由：① 写频率极低（每轮每分组一行，默认 2h 一轮），
// 单写者锁无竞争压力；② 计数必须随轮持久——异步队列在关机时可能丢掉最后一次
// +1，重启后计数少一，T-B7 的重启保证与 T-B8 的每 episode 一次通知都会失真。
func persist(ctx context.Context, st *model.ModelProbeState) {
	conn := db.GetDB()
	if conn == nil {
		return
	}
	result := conn.WithContext(ctx).Model(&model.ModelProbeState{}).
		Where("group_name = ?", st.GroupName).
		Updates(map[string]any{
			"consecutive_failures": st.ConsecutiveFailures,
			"last_probed_at":       st.LastProbedAt,
			"last_notified_fails":  st.LastNotifiedFails,
			"updated_at":           st.UpdatedAt,
		})
	if result.Error != nil {
		log.Warnf("model probe: failed to persist state for %q: %v", st.GroupName, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		if err := conn.WithContext(ctx).Create(st).Error; err != nil {
			log.Warnf("model probe: failed to create state for %q: %v", st.GroupName, err)
		}
	}
}
