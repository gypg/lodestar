package modelprobe

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/utils/cache"
)

// setupProbeTestDB 起一个独立内存库 + settings 缓存，重置包级内存态。
// 每个 t 一套 DSN，避免 shared-cache 串扰（项目里 91xxxx/94xxxx 段教训）。
func setupProbeTestDB(t *testing.T) context.Context {
	t.Helper()
	ctx := context.Background()
	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", testName)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_ = cache.New[model.SettingKey, string](16) // 占位：真实 setting 缓存在 op/setting 包内部
	if err := setting.RefreshCache(ctx); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}

	Invalidate()
	t.Cleanup(Invalidate)
	return ctx
}

func setProbeSetting(t *testing.T, key model.SettingKey, value string) {
	t.Helper()
	if err := setting.SetString(key, value); err != nil {
		t.Fatalf("set setting %s=%s: %v", key, value, err)
	}
}

// T-B3 默认关闭：不显式开启时 HiddenSnapshot 返回 nil（一切模型视为可用）。
func TestHiddenSnapshotNilWhenDisabled(t *testing.T) {
	ctx := setupProbeTestDB(t)
	_ = ctx

	// 默认值就是关闭（DefaultSettings: model_probe_enabled=false），这里只断言。
	enabled, err := setting.GetBool(model.SettingKeyModelProbeEnabled)
	if err != nil {
		t.Fatalf("read default enabled: %v", err)
	}
	if enabled {
		t.Fatal("model_probe_enabled defaults to true, want false: probing spends real upstream money, the operator must opt in")
	}

	// 即使库里已有失败计数，关闭开关也不隐藏任何模型。
	RecordProbeResult(context.Background(), "g-bad", false)
	if hidden := HiddenSnapshot(); len(hidden) != 0 {
		t.Fatalf("HiddenSnapshot() with probe disabled = %v, want empty: presentation must fall back to pre-probe behaviour when probing is off", hidden)
	}
}

// T-B5 第 3 轮才隐藏：1 轮、2 轮不隐藏，第 3 轮隐藏。三个断言缺一不可——
// 只测第 3 轮抓不到"第 1 轮就隐藏"的坏实现（M-B3 杀手腿是"1 轮不隐藏"）。
func TestThresholdThreeHidesOnlyOnThirdRound(t *testing.T) {
	ctx := setupProbeTestDB(t)
	setProbeSetting(t, model.SettingKeyModelProbeEnabled, "true")
	setProbeSetting(t, model.SettingKeyModelProbeFailThreshold, "3")

	if notify := RecordProbeResult(ctx, "g3", false); notify {
		t.Fatal("round 1: RecordProbeResult() notify = true, want false")
	}
	hiddenAfterOne := HiddenSnapshot()
	if len(hiddenAfterOne) != 0 {
		t.Fatalf("after round 1 HiddenSnapshot() = %v, want empty: 1 failure must NOT hide (this is the M-B3 killer leg)", hiddenAfterOne)
	}

	if notify := RecordProbeResult(ctx, "g3", false); notify {
		t.Fatal("round 2: RecordProbeResult() notify = true, want false")
	}
	if hidden := HiddenSnapshot(); len(hidden) != 0 {
		t.Fatalf("after round 2 HiddenSnapshot() = %v, want empty: 2 failures must NOT hide", hidden)
	}

	if notify := RecordProbeResult(ctx, "g3", false); !notify {
		t.Fatal("round 3: RecordProbeResult() notify = false, want true: crossing the threshold must trigger the operator notification")
	}
	hiddenAfterThreshold := HiddenSnapshot()
	if _, ok := hiddenAfterThreshold["g3"]; !ok {
		t.Fatalf("after round 3 HiddenSnapshot() = %v, want g3 hidden: 3 consecutive failures must hide the model", hiddenAfterThreshold)
	}
	MarkNotified(ctx, "g3")

	// 后续失败轮次不再重复通知（T-B8 的状态机半边）。
	if notify := RecordProbeResult(ctx, "g3", false); notify {
		t.Fatal("round 4: RecordProbeResult() notify = true, want false: same failure episode must notify only once")
	}
}

// T-B6 成功重置计数：失败 2 轮 + 成功 1 轮 + 失败 1 轮 → 不隐藏（也没通知）。
func TestSuccessResetsConsecutiveFailures(t *testing.T) {
	ctx := setupProbeTestDB(t)
	setProbeSetting(t, model.SettingKeyModelProbeEnabled, "true")
	setProbeSetting(t, model.SettingKeyModelProbeFailThreshold, "3")

	for i := 0; i < 2; i++ {
		if notify := RecordProbeResult(ctx, "g6", false); notify {
			t.Fatalf("failure round %d: notify = true, want false", i+1)
		}
	}
	if notify := RecordProbeResult(ctx, "g6", true); notify {
		t.Fatal("success round: notify = true, want false")
	}
	// 成功后必须真正清零：只差这一轮失败就得从 0 重新数。
	if notify := RecordProbeResult(ctx, "g6", false); notify {
		t.Fatal("post-success failure: notify = true, want false: a success must reset the counter, otherwise 2 fails + success + 1 fail wrongly hides")
	}
	if hidden := HiddenSnapshot(); len(hidden) != 0 {
		t.Fatalf("HiddenSnapshot() = %v, want empty: fail/fail/success/fail must not hide", hidden)
	}
}

// T-B7 计数跨重启不丢：记录 3 轮失败 → Invalidate（模拟重启丢内存）→
// 惰性加载后 HiddenSnapshot 仍隐藏。M-B5 的杀手。
func TestCountSurvivesRestart(t *testing.T) {
	ctx := setupProbeTestDB(t)
	setProbeSetting(t, model.SettingKeyModelProbeEnabled, "true")
	setProbeSetting(t, model.SettingKeyModelProbeFailThreshold, "3")

	for i := 0; i < 3; i++ {
		_ = RecordProbeResult(ctx, "g7", false)
	}
	hiddenBeforeRestart := HiddenSnapshot()
	if _, ok := hiddenBeforeRestart["g7"]; !ok {
		t.Fatalf("pre-restart HiddenSnapshot() = %v, want g7 hidden (test precondition)", hiddenBeforeRestart)
	}

	// SQLite 写队列异步落库，等它刷完（同进程 EnqueueWrite 顺序执行）。
	Invalidate() // 模拟重启：内存全丢
	hiddenAfterRestart := HiddenSnapshot()
	if _, ok := hiddenAfterRestart["g7"]; !ok {
		t.Fatalf("post-restart HiddenSnapshot() = %v, want g7 hidden: the counter must survive a restart (M-B5 killer); if this is red, persist() never reached the DB", hiddenAfterRestart)
	}
}

// T-B8 防重通知（发送侧确认路径）：MarkNotified 后同 episode 不再通知；
// 成功结案后新一轮失败可重新通知。
func TestNotifyOncePerEpisode(t *testing.T) {
	ctx := setupProbeTestDB(t)
	setProbeSetting(t, model.SettingKeyModelProbeEnabled, "true")
	setProbeSetting(t, model.SettingKeyModelProbeFailThreshold, "3")

	for i := 0; i < 2; i++ {
		_ = RecordProbeResult(ctx, "g8", false)
	}
	if notify := RecordProbeResult(ctx, "g8", false); !notify {
		t.Fatal("round 3: notify = false, want true (precondition)")
	}
	MarkNotified(ctx, "g8")
	if notify := RecordProbeResult(ctx, "g8", false); notify {
		t.Fatal("round 4 after MarkNotified: notify = true, want false: one failure episode must notify exactly once")
	}

	// 恢复后再坏：新一轮失败必须能重新通知。
	_ = RecordProbeResult(ctx, "g8", true)
	for i := 0; i < 3; i++ {
		notify := RecordProbeResult(ctx, "g8", false)
		if i < 2 && notify {
			t.Fatalf("new episode failure round %d: notify = true, want false", i+1)
		}
		if i == 2 && !notify {
			t.Fatal("new episode round 3: notify = false, want true: recovery must re-arm the notification")
		}
	}
}

// T-B4 人工跳过位：SkipGroups 解析 setting（大小写不敏感、trim）。
// "跳过 = 不探测/不计数/不通知"的调用侧由 task 层测试覆盖；这里钉住解析语义。
func TestSkipGroupsParsing(t *testing.T) {
	_ = setupProbeTestDB(t)
	setProbeSetting(t, model.SettingKeyModelProbeSkipGroups, `["Bad-Model", "  another-one  "]`)

	skipped, err := SkipGroups()
	if err != nil {
		t.Fatalf("SkipGroups() error = %v", err)
	}
	if _, ok := skipped["bad-model"]; !ok {
		t.Fatalf("SkipGroups() = %v, want \"Bad-Model\" (case-insensitive)", skipped)
	}
	if _, ok := skipped["another-one"]; !ok {
		t.Fatalf("SkipGroups() = %v, want \"another-one\" (trimmed)", skipped)
	}

	// 非法 JSON 必须报错而不是静默吞掉（静默 = 运营者打错字后全部模型照探不误）。
	setProbeSetting(t, model.SettingKeyModelProbeSkipGroups, `not-json`)
	if _, err := SkipGroups(); err == nil {
		t.Fatal("SkipGroups() with malformed JSON error = nil, want error")
	}
}

// T-B10 的状态机半边：计数/隐藏不写任何计费字段（本包根本没有计费依赖），
// 这里断言探测状态表只有预期的行——多余的行意味着把探测结果写到了不该写的地方。
func TestProbeStateTableOnlyHasProbeRows(t *testing.T) {
	ctx := setupProbeTestDB(t)
	setProbeSetting(t, model.SettingKeyModelProbeEnabled, "true")

	_ = RecordProbeResult(ctx, "g10", false)
	var count int64
	if err := db.GetDB().Model(&model.ModelProbeState{}).Count(&count).Error; err != nil {
		t.Fatalf("count model_probe_states: %v", err)
	}
	if count != 1 {
		t.Fatalf("model_probe_states rows = %d, want 1", count)
	}
}
