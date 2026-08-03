package relaylog

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
)

func TestRelayLogFlushToDBSkipsDuplicateIDsAndTruncatesCache(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "relaylog.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	existing := model.RelayLog{ID: 101, Time: 1, RequestModelName: "existing"}
	if err := db.GetDB().Create(&existing).Error; err != nil {
		t.Fatalf("seed relay log failed: %v", err)
	}

	relayLogCacheLock.Lock()
	relayLogCache = []model.RelayLog{
		{ID: 101, Time: 2, RequestModelName: "duplicate"},
		{ID: 102, Time: 3, RequestModelName: "new"},
	}
	relayLogCacheLock.Unlock()
	t.Cleanup(func() {
		relayLogCacheLock.Lock()
		relayLogCache = make([]model.RelayLog, 0, relayLogMaxSize)
		relayLogCacheLock.Unlock()
	})

	if err := relayLogFlushToDB(context.Background()); err != nil {
		t.Fatalf("relayLogFlushToDB returned error: %v", err)
	}

	var count int64
	if err := db.GetDB().Model(&model.RelayLog{}).Count(&count).Error; err != nil {
		t.Fatalf("count relay logs failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("relay log count = %d, want 2", count)
	}

	var inserted model.RelayLog
	if err := db.GetDB().First(&inserted, "id = ?", 102).Error; err != nil {
		t.Fatalf("new relay log was not inserted: %v", err)
	}

	relayLogCacheLock.Lock()
	cacheLen := len(relayLogCache)
	relayLogCacheLock.Unlock()
	if cacheLen != 0 {
		t.Fatalf("relay log cache len = %d, want 0", cacheLen)
	}
}

func TestRelayLogCleanupAll(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "relaylog-cleanup.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Seed DB with a mix of success and error logs
	seed := []model.RelayLog{
		{ID: 401, Time: 1, RequestModelName: "model-a", Error: ""},
		{ID: 402, Time: 2, RequestModelName: "model-b", Error: "timeout"},
		{ID: 403, Time: 3, RequestModelName: "model-c", Error: ""},
	}
	if err := db.GetDB().Create(&seed).Error; err != nil {
		t.Fatalf("seed relay logs failed: %v", err)
	}

	var before int64
	db.GetDB().Model(&model.RelayLog{}).Count(&before)
	if before != 3 {
		t.Fatalf("expected 3 seeded logs, got %d", before)
	}

	if err := relayLogCleanupAll(context.Background()); err != nil {
		t.Fatalf("relayLogCleanupAll returned error: %v", err)
	}

	var after int64
	if err := db.GetDB().Model(&model.RelayLog{}).Count(&after).Error; err != nil {
		t.Fatalf("count after cleanup failed: %v", err)
	}
	if after != 0 {
		t.Fatalf("relay log count = %d, want 0 (all logs should be deleted)", after)
	}
}

func TestRelayLogListExcludesConfiguredGroups(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "relaylog-exclude.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("RefreshCache failed: %v", err)
	}

	// 日志保存关闭：RelayLogList 只读内存缓存，便于断言过滤行为。
	if err := setting.SetString(model.SettingKeyRelayLogKeepEnabled, "false"); err != nil {
		t.Fatalf("disable relay log keep failed: %v", err)
	}
	if err := setting.SetString(model.SettingKeyLogExcludedGroups, `["stress-test"]`); err != nil {
		t.Fatalf("set excluded groups failed: %v", err)
	}

	restore := SetCacheForTest([]model.RelayLog{
		{ID: 1, Time: 1, RequestModelName: "gpt-4"},
		{ID: 2, Time: 2, RequestModelName: "stress-test"},
		{ID: 3, Time: 3, RequestModelName: "claude"},
		{ID: 4, Time: 4, RequestModelName: "stress-test"},
	})
	t.Cleanup(restore)

	logs, err := RelayLogList(context.Background(), LogFilter{}, 1, 50)
	if err != nil {
		t.Fatalf("RelayLogList returned error: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("RelayLogList returned %d logs, want 2 (stress-test excluded)", len(logs))
	}
	for _, l := range logs {
		if l.RequestModelName == "stress-test" {
			t.Fatalf("excluded group log leaked into result: id=%d", l.ID)
		}
	}

	// 清空屏蔽配置后，全部日志都应返回。
	if err := setting.SetString(model.SettingKeyLogExcludedGroups, "[]"); err != nil {
		t.Fatalf("clear excluded groups failed: %v", err)
	}
	logs, err = RelayLogList(context.Background(), LogFilter{}, 1, 50)
	if err != nil {
		t.Fatalf("RelayLogList returned error: %v", err)
	}
	if len(logs) != 4 {
		t.Fatalf("RelayLogList returned %d logs, want 4 (no exclusion)", len(logs))
	}
}

func TestRelayLogStreamExcluded(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "relaylog-stream-exclude.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("RefreshCache failed: %v", err)
	}
	if err := setting.SetString(model.SettingKeyLogExcludedGroups, `["stress-test"]`); err != nil {
		t.Fatalf("set excluded groups failed: %v", err)
	}

	if !RelayLogStreamExcluded("stress-test") {
		t.Fatalf("expected stress-test to be excluded from stream")
	}
	if RelayLogStreamExcluded("gpt-4") {
		t.Fatalf("expected gpt-4 to NOT be excluded from stream")
	}
}

func TestRelayLogCleanupAllFastClearAllowsReinsert(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "relaylog-fastclear.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	seed := []model.RelayLog{
		{ID: 1, Time: 1, RequestModelName: "a"},
		{ID: 2, Time: 2, RequestModelName: "b"},
		{ID: 3, Time: 3, RequestModelName: "c"},
	}
	if err := db.GetDB().Create(&seed).Error; err != nil {
		t.Fatalf("seed relay logs failed: %v", err)
	}

	// FastClearTable 走 DROP + AutoMigrate 重建（SQLite）。
	if err := relayLogCleanupAll(context.Background()); err != nil {
		t.Fatalf("relayLogCleanupAll returned error: %v", err)
	}

	var count int64
	if err := db.GetDB().Model(&model.RelayLog{}).Count(&count).Error; err != nil {
		t.Fatalf("count after fast clear failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("relay log count = %d, want 0 after fast clear", count)
	}

	// 重建后表与索引应可正常工作：再次写入并按时间范围查询。
	if err := db.GetDB().Create(&model.RelayLog{ID: 10, Time: 100, RequestModelName: "reinsert"}).Error; err != nil {
		t.Fatalf("reinsert after fast clear failed: %v", err)
	}
	var got model.RelayLog
	if err := db.GetDB().Where("time >= ?", 50).First(&got).Error; err != nil {
		t.Fatalf("query by time index after rebuild failed: %v", err)
	}
	if got.ID != 10 {
		t.Fatalf("reinserted row id = %d, want 10", got.ID)
	}
}

func TestRelayLogSeparateLogDBRoutesWrites(t *testing.T) {
	mainPath := filepath.Join(t.TempDir(), "main.db")
	if err := db.InitDB("sqlite", mainPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "logs.db")
	if err := db.InitLogDB("sqlite", logPath, false); err != nil {
		t.Fatalf("InitLogDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("RefreshCache failed: %v", err)
	}

	// Seed the in-memory cache and flush; logs must land on the log DB.
	restore := SetCacheForTest([]model.RelayLog{
		{ID: 1, Time: 1, RequestModelName: "m1"},
		{ID: 2, Time: 2, RequestModelName: "m2"},
	})
	t.Cleanup(restore)

	if err := relayLogFlushToDB(context.Background()); err != nil {
		t.Fatalf("relayLogFlushToDB failed: %v", err)
	}

	// Log DB should hold the rows.
	var logCount int64
	if err := db.GetLogDB().Model(&model.RelayLog{}).Count(&logCount).Error; err != nil {
		t.Fatalf("count log DB failed: %v", err)
	}
	if logCount != 2 {
		t.Fatalf("log DB relay log count = %d, want 2", logCount)
	}

	// Main DB's relay_logs table must remain empty (writes did not leak there).
	var mainCount int64
	if err := db.GetDB().Model(&model.RelayLog{}).Count(&mainCount).Error; err != nil {
		t.Fatalf("count main DB failed: %v", err)
	}
	if mainCount != 0 {
		t.Fatalf("main DB relay log count = %d, want 0 (logs must not leak to main DB)", mainCount)
	}
}

func TestRelayLogApplyKeepEnabledClosesAndReopensLogDB(t *testing.T) {
	mainPath := filepath.Join(t.TempDir(), "main.db")
	if err := db.InitDB("sqlite", mainPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "logs.db")
	if err := db.InitLogDB("sqlite", logPath, false); err != nil {
		t.Fatalf("InitLogDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Disabling logs clears and disconnects the separate log DB.
	if err := ApplyKeepEnabledChange(context.Background(), false); err != nil {
		t.Fatalf("ApplyKeepEnabledChange(false) failed: %v", err)
	}
	if db.GetLogDB() != nil {
		t.Fatalf("GetLogDB() should be nil after disabling logs in separate mode")
	}

	// Re-enabling reconnects.
	if err := ApplyKeepEnabledChange(context.Background(), true); err != nil {
		t.Fatalf("ApplyKeepEnabledChange(true) failed: %v", err)
	}
	if db.GetLogDB() == nil {
		t.Fatalf("GetLogDB() should be non-nil after re-enabling logs")
	}
}

// TestRelayLogListReadsPersistedCacheColumns 验证列表查询直接返回落库的
// semantic_cache_hit / cache_read_tokens 列，而不再读取并解析 response_content
// 大字段——这是日志列表加载缓慢问题的核心修复点。
func TestRelayLogListReadsPersistedCacheColumns(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "relaylog-cachecols.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	// 显式声明共用主库模式，清除可能被前序「独立日志库」测试残留的全局
	// logDBType 状态（CloseLogDB 在关闭后仍保留 logDBType 供 Reopen 使用），
	// 否则 GetLogDB 会走独立库分支返回 nil，跳过本测试需要的 DB 查询。
	if err := db.InitLogDB("", "", false); err != nil {
		t.Fatalf("InitLogDB(shared) failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("RefreshCache failed: %v", err)
	}
	// 启用日志保存，让列表查询走 DB 分支。
	if err := setting.SetString(model.SettingKeyRelayLogKeepEnabled, "true"); err != nil {
		t.Fatalf("enable relay log keep failed: %v", err)
	}

	// 落库时大字段是一个无法解析出缓存信号的占位串：若查询仍依赖解析
	// response_content，下面对 cache 列的断言就会失败。
	seed := []model.RelayLog{
		{ID: 1, Time: 1, RequestModelName: "gpt-4", SemanticCacheHit: true, CacheReadTokens: 0, ResponseContent: "not-json"},
		{ID: 2, Time: 2, RequestModelName: "claude", SemanticCacheHit: false, CacheReadTokens: 123, ResponseContent: "not-json"},
	}
	if err := db.GetDB().Create(&seed).Error; err != nil {
		t.Fatalf("seed relay logs failed: %v", err)
	}

	logs, err := RelayLogList(context.Background(), LogFilter{}, 1, 50)
	if err != nil {
		t.Fatalf("RelayLogList returned error: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("RelayLogList returned %d logs, want 2", len(logs))
	}

	byID := make(map[int64]model.RelayLogListItem, len(logs))
	for _, l := range logs {
		byID[l.ID] = l
	}
	if !byID[1].SemanticCacheHit {
		t.Fatalf("log 1 SemanticCacheHit = false, want true (should come from persisted column)")
	}
	if byID[2].CacheReadTokens != 123 {
		t.Fatalf("log 2 CacheReadTokens = %d, want 123 (should come from persisted column)", byID[2].CacheReadTokens)
	}
}

// TestRelayLogListFiltersByFields 覆盖 LogFilter 各筛选维度在内存缓存路径上的行为
// （关闭日志保存，RelayLogList 只读缓存）。DB 路径的筛选条件与缓存路径一一对应，
// 此处以缓存路径为代表做行为断言。
func TestRelayLogListFiltersByFields(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "relaylog-filter.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("RefreshCache failed: %v", err)
	}
	// 关闭日志保存：RelayLogList 只读内存缓存，便于断言筛选行为。
	if err := setting.SetString(model.SettingKeyRelayLogKeepEnabled, "false"); err != nil {
		t.Fatalf("disable relay log keep failed: %v", err)
	}

	restore := SetCacheForTest([]model.RelayLog{
		{ID: 1, Time: 10, RequestModelName: "gpt-4o", ActualModelName: "gpt-4o-2024", ChannelId: 100, RequestAPIKeyID: 7, EndpointType: "chat", Error: ""},
		{ID: 2, Time: 20, RequestModelName: "claude-3", ActualModelName: "claude-3-opus", ChannelId: 200, RequestAPIKeyID: 8, EndpointType: "messages", Error: "timeout"},
		{ID: 3, Time: 30, RequestModelName: "gpt-4o-mini", ActualModelName: "gpt-4o-mini", ChannelId: 100, RequestAPIKeyID: 7, EndpointType: "chat", Error: ""},
		{ID: 4, Time: 40, RequestModelName: "gemini", ActualModelName: "gemini-pro", ChannelId: 300, RequestAPIKeyID: 9, EndpointType: "chat", Error: "rate_limit"},
	})
	t.Cleanup(restore)

	// 结果按「新 -> 旧」顺序返回，这里按 ID 断言。
	ids := func(logs []model.RelayLogListItem) []int64 {
		out := make([]int64, 0, len(logs))
		for _, l := range logs {
			out = append(out, l.ID)
		}
		return out
	}
	equal := func(got []int64, want ...int64) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	t.Run("model substring matches request or actual", func(t *testing.T) {
		// 大小写不敏感：命中 request(gpt-4o) 与 actual/mini
		logs, err := RelayLogList(context.Background(), LogFilter{Model: "GPT-4O"}, 1, 50)
		if err != nil {
			t.Fatalf("RelayLogList error: %v", err)
		}
		if got := ids(logs); !equal(got, 3, 1) {
			t.Fatalf("model filter got %v, want [3 1]", got)
		}
	})

	t.Run("model matches actual_model_name", func(t *testing.T) {
		logs, err := RelayLogList(context.Background(), LogFilter{Model: "opus"}, 1, 50)
		if err != nil {
			t.Fatalf("RelayLogList error: %v", err)
		}
		if got := ids(logs); !equal(got, 2) {
			t.Fatalf("actual model filter got %v, want [2]", got)
		}
	})

	t.Run("channel_id", func(t *testing.T) {
		logs, err := RelayLogList(context.Background(), LogFilter{ChannelID: intPtr(100)}, 1, 50)
		if err != nil {
			t.Fatalf("RelayLogList error: %v", err)
		}
		if got := ids(logs); !equal(got, 3, 1) {
			t.Fatalf("channel filter got %v, want [3 1]", got)
		}
	})

	t.Run("api_key_id", func(t *testing.T) {
		logs, err := RelayLogList(context.Background(), LogFilter{APIKeyID: intPtr(8)}, 1, 50)
		if err != nil {
			t.Fatalf("RelayLogList error: %v", err)
		}
		if got := ids(logs); !equal(got, 2) {
			t.Fatalf("api key filter got %v, want [2]", got)
		}
	})

	t.Run("endpoint_type", func(t *testing.T) {
		logs, err := RelayLogList(context.Background(), LogFilter{EndpointType: "chat"}, 1, 50)
		if err != nil {
			t.Fatalf("RelayLogList error: %v", err)
		}
		if got := ids(logs); !equal(got, 4, 3, 1) {
			t.Fatalf("endpoint type filter got %v, want [4 3 1]", got)
		}
	})

	t.Run("has_error true", func(t *testing.T) {
		tt := true
		logs, err := RelayLogList(context.Background(), LogFilter{HasError: &tt}, 1, 50)
		if err != nil {
			t.Fatalf("RelayLogList error: %v", err)
		}
		if got := ids(logs); !equal(got, 4, 2) {
			t.Fatalf("hasError=true filter got %v, want [4 2]", got)
		}
	})

	t.Run("has_error false", func(t *testing.T) {
		ff := false
		logs, err := RelayLogList(context.Background(), LogFilter{HasError: &ff}, 1, 50)
		if err != nil {
			t.Fatalf("RelayLogList error: %v", err)
		}
		if got := ids(logs); !equal(got, 3, 1) {
			t.Fatalf("hasError=false filter got %v, want [3 1]", got)
		}
	})

	t.Run("combined filters narrow results", func(t *testing.T) {
		// channel 100 的日志均成功，叠加 HasError=true 应为空
		tt := true
		logs, err := RelayLogList(context.Background(), LogFilter{ChannelID: intPtr(100), HasError: &tt}, 1, 50)
		if err != nil {
			t.Fatalf("RelayLogList error: %v", err)
		}
		if len(logs) != 0 {
			t.Fatalf("combined filter got %v, want empty", ids(logs))
		}
	})
}

func intPtr(v int) *int { return &v }
