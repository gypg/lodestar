package relaylog

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
)

// R-9: relay_log_attempts 曾由调用方在 RelayLogAdd 之后立即写入，而 RelayLogAdd
// 只进内存缓存，父日志要等刷盘才落库。实测（修复前）：走完生产调用序列后
// relay_log_attempts 有 2 行、relay_logs 有 0 行；此时按渠道做 IncludeAttempts
// 过滤命中 0 条，进程若在刷盘前重启则明细永久孤立。
// 现在明细随父日志同批写入（flushAttemptsForBatch）。

func setupAttemptsFlushDB(t *testing.T) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "relaylog-attempts-flush.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	if err := db.InitLogDB("", "", false); err != nil {
		t.Fatalf("InitLogDB(shared) failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("RefreshCache failed: %v", err)
	}
	if err := setting.SetString(model.SettingKeyRelayLogKeepEnabled, "true"); err != nil {
		t.Fatalf("enable relay log keep failed: %v", err)
	}
	t.Cleanup(SetCacheForTest(nil))
}

func failoverLog() model.RelayLog {
	return model.RelayLog{
		Time: 200, RequestModelName: "gpt-4o", ActualModelName: "gpt-4o",
		ChannelId: 22, ChannelName: "channelB", Error: "", TotalAttempts: 2,
		Attempts: []model.ChannelAttempt{
			{ChannelID: 11, ChannelName: "channelA", ModelName: "gpt-4o", Status: model.AttemptFailed, Duration: 120},
			{ChannelID: 22, ChannelName: "channelB", ModelName: "gpt-4o", Status: model.AttemptSuccess, Duration: 340},
		},
	}
}

func countRows(t *testing.T) (logs int64, attempts int64) {
	t.Helper()
	conn := db.GetLogDB()
	if err := conn.Model(&model.RelayLog{}).Count(&logs).Error; err != nil {
		t.Fatalf("count relay_logs: %v", err)
	}
	if err := conn.Model(&model.RelayLogAttempt{}).Count(&attempts).Error; err != nil {
		t.Fatalf("count relay_log_attempts: %v", err)
	}
	return logs, attempts
}

// 明细绝不能先于父日志出现在库里。断言的是"缓存中、未刷盘"这个中间状态。
func TestRelayLogAddDoesNotWriteAttemptsBeforeParentLog(t *testing.T) {
	setupAttemptsFlushDB(t)

	relayLog := failoverLog()
	if err := RelayLogAdd(context.Background(), &relayLog); err != nil {
		t.Fatalf("RelayLogAdd: %v", err)
	}

	logs, attempts := countRows(t)
	if logs != 0 {
		t.Fatalf("relay_logs = %d, want 0 (RelayLogAdd 只进缓存)", logs)
	}
	if attempts != 0 {
		t.Fatalf("relay_log_attempts = %d, want 0 —— 明细先于父日志落库（R-9 回归）", attempts)
	}
}

// 刷盘后父日志与明细必须同时可见，且明细内容完整（含失败那一跳）。
func TestRelayLogFlushWritesAttemptsWithParent(t *testing.T) {
	setupAttemptsFlushDB(t)

	relayLog := failoverLog()
	if err := RelayLogAdd(context.Background(), &relayLog); err != nil {
		t.Fatalf("RelayLogAdd: %v", err)
	}
	if err := relayLogFlushToDB(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}

	logs, attempts := countRows(t)
	if logs != 1 {
		t.Fatalf("relay_logs = %d, want 1", logs)
	}
	if attempts != 2 {
		t.Fatalf("relay_log_attempts = %d, want 2", attempts)
	}

	var rows []model.RelayLogAttempt
	if err := db.GetLogDB().Where("relay_log_id = ?", relayLog.ID).Order("channel_id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load attempts: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	// 死期望值：内容+状态+归属，不是"非空"。
	if rows[0].ChannelID != 11 || rows[0].Status != string(model.AttemptFailed) || rows[0].ChannelName != "channelA" {
		t.Errorf("rows[0] = %+v, want channel 11 channelA failed", rows[0])
	}
	if rows[1].ChannelID != 22 || rows[1].Status != string(model.AttemptSuccess) {
		t.Errorf("rows[1] = %+v, want channel 22 success", rows[1])
	}
	if rows[0].Time != relayLog.Time || rows[1].Time != relayLog.Time {
		t.Errorf("attempt Time = (%d, %d), want %d (= 父日志 time)", rows[0].Time, rows[1].Time, relayLog.Time)
	}
}

// 只有 DB 里两张表都在，IncludeAttempts 的 EXISTS 子查询才命中。清空缓存后
// 仍能按失败渠道（顶层渠道是 B）检索到，才说明明细真的可用。
func TestRelayLogFlushMakesAttemptsFilterableFromDB(t *testing.T) {
	setupAttemptsFlushDB(t)

	relayLog := failoverLog()
	if err := RelayLogAdd(context.Background(), &relayLog); err != nil {
		t.Fatalf("RelayLogAdd: %v", err)
	}
	if err := relayLogFlushToDB(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	// 清空缓存，强制走 DB 路径。
	t.Cleanup(SetCacheForTest(nil))

	logs, err := RelayLogList(context.Background(), LogFilter{ChannelID: intPtr(11), IncludeAttempts: true}, 1, 50)
	if err != nil {
		t.Fatalf("RelayLogList: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("ChannelID=11 IncludeAttempts got %d logs, want 1 (明细未随父日志落库？)", len(logs))
	}
	if logs[0].ID != relayLog.ID {
		t.Errorf("log ID = %d, want %d", logs[0].ID, relayLog.ID)
	}

	// 反方向：不开 IncludeAttempts 时不应命中（顶层渠道是 22，不是 11）。
	// 少了这条，上面的断言无法区分"明细命中"和"顶层渠道恰好匹配"。
	logs, err = RelayLogList(context.Background(), LogFilter{ChannelID: intPtr(11)}, 1, 50)
	if err != nil {
		t.Fatalf("RelayLogList: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("without IncludeAttempts got %d logs, want 0", len(logs))
	}
}

// 日志关闭时不写明细。enabled=false 下 RelayLogAdd 不触发刷盘，故直接验刷盘。
func TestRelayLogFlushSkipsAttemptsWhenKeepDisabled(t *testing.T) {
	setupAttemptsFlushDB(t)
	if err := setting.SetString(model.SettingKeyRelayLogKeepEnabled, "false"); err != nil {
		t.Fatalf("disable keep failed: %v", err)
	}

	relayLog := failoverLog()
	if err := RelayLogAdd(context.Background(), &relayLog); err != nil {
		t.Fatalf("RelayLogAdd: %v", err)
	}

	if _, attempts := countRows(t); attempts != 0 {
		t.Fatalf("relay_log_attempts = %d, want 0 when log keeping is disabled", attempts)
	}
}

// 占位尝试（ChannelID==0）不入表：它们无法按渠道聚合，留着只会污染统计。
func TestRelayLogFlushSkipsAttemptsWithoutChannel(t *testing.T) {
	setupAttemptsFlushDB(t)

	relayLog := model.RelayLog{
		Time: 300, RequestModelName: "gpt-4o", ChannelId: 22, TotalAttempts: 2,
		Attempts: []model.ChannelAttempt{
			{ChannelID: 0, ChannelName: "", ModelName: "gpt-4o", Status: model.AttemptFailed},
			{ChannelID: 22, ChannelName: "channelB", ModelName: "gpt-4o", Status: model.AttemptSuccess},
		},
	}
	if err := RelayLogAdd(context.Background(), &relayLog); err != nil {
		t.Fatalf("RelayLogAdd: %v", err)
	}
	if err := relayLogFlushToDB(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}

	if _, attempts := countRows(t); attempts != 1 {
		t.Fatalf("relay_log_attempts = %d, want 1 (占位尝试应被跳过)", attempts)
	}
}

// 父日志写失败时，绝不能留下明细行 —— 这正是 R-9 的不变式：明细不得先于
// （或脱离）父日志落库。把 relay_logs 整表删掉制造写失败，此时若明细写在父日志
// 之前，孤儿行就会留下来。
//
// ★ 这条测试是必需的，别以为前面几条已经覆盖了顺序：把 flushAttemptsForBatch
// 调用挪到 Create(&batch) 之前，其余所有测试全部照绿（它们只在刷盘返回后观测
// 状态，那时两次写都已发生）。只有父日志写失败这条路径能观测到顺序。
func TestRelayLogFlushLeavesNoAttemptsWhenParentInsertFails(t *testing.T) {
	setupAttemptsFlushDB(t)

	relayLog := failoverLog()
	if err := RelayLogAdd(context.Background(), &relayLog); err != nil {
		t.Fatalf("RelayLogAdd: %v", err)
	}

	// 干掉父表，令 Create(&batch) 必然失败；明细表保持存在。
	if err := db.GetLogDB().Migrator().DropTable(&model.RelayLog{}); err != nil {
		t.Fatalf("drop relay_logs: %v", err)
	}

	if err := relayLogFlushToDB(context.Background()); err == nil {
		t.Fatal("relayLogFlushToDB returned nil, want error (父表已删)")
	}

	var attempts int64
	if err := db.GetLogDB().Model(&model.RelayLogAttempt{}).Count(&attempts).Error; err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if attempts != 0 {
		t.Fatalf("relay_log_attempts = %d, want 0 —— 父日志写失败却留下了明细（顺序反了）", attempts)
	}
}

// ── 清理路径 ──
// R-9 第二半：relay_log_attempts 此前在任何清理路径下都不被删除（全仓 grep
// delete/clear/truncate 对该表零命中），故该表永不回收，且父日志被裁剪后明细
// 变成不可达的孤儿数据。实测（修复前）："清空全部日志"后明细 2 行全部残留。

// 用户点"清空全部日志"必须把明细一起清掉。
func TestRelayLogClearAlsoClearsAttempts(t *testing.T) {
	setupAttemptsFlushDB(t)

	relayLog := failoverLog()
	if err := RelayLogAdd(context.Background(), &relayLog); err != nil {
		t.Fatalf("RelayLogAdd: %v", err)
	}
	if err := relayLogFlushToDB(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if _, attempts := countRows(t); attempts != 2 {
		t.Fatalf("precondition: relay_log_attempts = %d, want 2", attempts)
	}

	if err := RelayLogClear(context.Background()); err != nil {
		t.Fatalf("RelayLogClear: %v", err)
	}

	logs, attempts := countRows(t)
	if logs != 0 {
		t.Errorf("relay_logs = %d, want 0", logs)
	}
	if attempts != 0 {
		t.Errorf("relay_log_attempts = %d, want 0 —— 明细泄漏（R-9 回归）", attempts)
	}

	// FastClearTable 走 DROP+重建，验证重建后表仍可写。
	if err := db.GetLogDB().Create(&model.RelayLogAttempt{
		RelayLogID: 1, ChannelID: 5, Status: string(model.AttemptSuccess), Time: 9,
	}).Error; err != nil {
		t.Fatalf("reinsert attempt after clear failed: %v", err)
	}
}

// 日志关闭时的整表清空路径同样要带上明细。
func TestRelayLogCleanupAllAlsoClearsAttempts(t *testing.T) {
	setupAttemptsFlushDB(t)

	relayLog := failoverLog()
	if err := RelayLogAdd(context.Background(), &relayLog); err != nil {
		t.Fatalf("RelayLogAdd: %v", err)
	}
	if err := relayLogFlushToDB(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}

	if err := relayLogCleanupAll(context.Background()); err != nil {
		t.Fatalf("relayLogCleanupAll: %v", err)
	}

	if logs, attempts := countRows(t); logs != 0 || attempts != 0 {
		t.Fatalf("after cleanupAll: relay_logs=%d relay_log_attempts=%d, want 0/0", logs, attempts)
	}
}

// 按条数裁剪：被裁掉的父日志，其明细必须一起走；保留的那些必须留下。
func TestRelayLogCleanupByCountAlsoPrunesAttempts(t *testing.T) {
	setupAttemptsFlushDB(t)
	if err := setting.SetString(model.SettingKeyRelayLogKeepCount, "2"); err != nil {
		t.Fatalf("set keep count failed: %v", err)
	}

	conn := db.GetLogDB()
	// id 递增，各带 1 行明细。keepCount=2、total=6 → 删一半（3 条最旧）。
	for i := 1; i <= 6; i++ {
		id := int64(i)
		if err := conn.Create(&model.RelayLog{ID: id, Time: id, RequestModelName: "m"}).Error; err != nil {
			t.Fatalf("seed log %d: %v", i, err)
		}
		if err := conn.Create(&model.RelayLogAttempt{
			RelayLogID: id, ChannelID: 7, Status: string(model.AttemptSuccess), Time: id,
		}).Error; err != nil {
			t.Fatalf("seed attempt %d: %v", i, err)
		}
	}

	if err := relayLogCleanup(context.Background()); err != nil {
		t.Fatalf("relayLogCleanup: %v", err)
	}

	logs, attempts := countRows(t)
	// 两张表必须裁掉同样多，否则就是孤儿或误删。
	if logs != attempts {
		t.Fatalf("relay_logs=%d but relay_log_attempts=%d — 明细与父日志裁剪不同步", logs, attempts)
	}
	if logs != 3 {
		t.Fatalf("relay_logs = %d, want 3 (6 条删一半)", logs)
	}
	// 保留的必须是最新那批：最小残留 id 应为 4。
	var minID int64
	if err := conn.Model(&model.RelayLog{}).Select("MIN(id)").Scan(&minID).Error; err != nil {
		t.Fatalf("min id: %v", err)
	}
	if minID != 4 {
		t.Fatalf("MIN(relay_logs.id) = %d, want 4 (应删最旧的 1~3)", minID)
	}
	var orphans int64
	if err := conn.Model(&model.RelayLogAttempt{}).
		Where("relay_log_id NOT IN (SELECT id FROM relay_logs)").Count(&orphans).Error; err != nil {
		t.Fatalf("orphan count: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("orphan attempt rows = %d, want 0", orphans)
	}
}

// 按天数裁剪：明细行自带 time，按同一截止点删。
func TestRelayLogCleanupByDaysAlsoPrunesAttempts(t *testing.T) {
	setupAttemptsFlushDB(t)
	if err := setting.SetString(model.SettingKeyRelayLogKeepCount, "0"); err != nil {
		t.Fatalf("clear keep count failed: %v", err)
	}
	if err := setting.SetString(model.SettingKeyRelayLogKeepPeriod, "1"); err != nil {
		t.Fatalf("set keep period failed: %v", err)
	}

	conn := db.GetLogDB()
	old := time.Now().Add(-48 * time.Hour).Unix()
	recent := time.Now().Unix()
	seed := func(id int64, ts int64) {
		if err := conn.Create(&model.RelayLog{ID: id, Time: ts, RequestModelName: "m"}).Error; err != nil {
			t.Fatalf("seed log %d: %v", id, err)
		}
		if err := conn.Create(&model.RelayLogAttempt{
			RelayLogID: id, ChannelID: 7, Status: string(model.AttemptSuccess), Time: ts,
		}).Error; err != nil {
			t.Fatalf("seed attempt %d: %v", id, err)
		}
	}
	seed(1, old)
	seed(2, recent)

	if err := relayLogCleanup(context.Background()); err != nil {
		t.Fatalf("relayLogCleanup: %v", err)
	}

	logs, attempts := countRows(t)
	if logs != 1 || attempts != 1 {
		t.Fatalf("relay_logs=%d relay_log_attempts=%d, want 1/1 (只删过期那条)", logs, attempts)
	}
	var keptID int64
	if err := conn.Model(&model.RelayLogAttempt{}).Select("relay_log_id").Scan(&keptID).Error; err != nil {
		t.Fatalf("kept attempt: %v", err)
	}
	if keptID != 2 {
		t.Fatalf("kept attempt relay_log_id = %d, want 2", keptID)
	}
}

// 一批多条日志时，每行明细必须归到各自的父日志，不能串台。
func TestRelayLogFlushAttributesAttemptsPerLog(t *testing.T) {
	setupAttemptsFlushDB(t)

	first := failoverLog()
	second := model.RelayLog{
		Time: 400, RequestModelName: "gpt-4o", ChannelId: 33, TotalAttempts: 1,
		Attempts: []model.ChannelAttempt{
			{ChannelID: 33, ChannelName: "channelC", ModelName: "gpt-4o", Status: model.AttemptSuccess},
		},
	}
	if err := RelayLogAdd(context.Background(), &first); err != nil {
		t.Fatalf("RelayLogAdd(first): %v", err)
	}
	if err := RelayLogAdd(context.Background(), &second); err != nil {
		t.Fatalf("RelayLogAdd(second): %v", err)
	}
	if err := relayLogFlushToDB(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}

	var firstRows, secondRows int64
	db.GetLogDB().Model(&model.RelayLogAttempt{}).Where("relay_log_id = ?", first.ID).Count(&firstRows)
	db.GetLogDB().Model(&model.RelayLogAttempt{}).Where("relay_log_id = ?", second.ID).Count(&secondRows)
	if firstRows != 2 {
		t.Errorf("first log attempts = %d, want 2", firstRows)
	}
	if secondRows != 1 {
		t.Errorf("second log attempts = %d, want 1", secondRows)
	}
}
