package walletusage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/op/relaylog"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/user"
)

// initUsageTestDB boots an in-memory SQLite + shared log DB + caches, turns
// relay_log_keep_enabled ON, and creates one user owning one API key. Returns
// the key id (so tests can build RelayLog rows that ListByUser will claim).
//
// Following the analytics test scaffolding (analytics_grouphealth_test.go):
// in-memory shared-cache DSN, InitLogDB("") for the shared-log variant, and
// RefreshCache so the setting reads see keep-enabled=true.
func initUsageTestDB(t *testing.T) (uid uint, keyID int) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "walletusage.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	if err := db.InitLogDB("", "", false); err != nil {
		t.Fatalf("InitLogDB(shared): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("RefreshCache: %v", err)
	}
	if err := setting.SetString(model.SettingKeyRelayLogKeepEnabled, "true"); err != nil {
		t.Fatalf("enable relay log keep: %v", err)
	}

	u := model.User{Username: "usage-user", Password: "usage-password-1", Role: model.UserRoleUser}
	if err := user.Create(model.UserCreateRequest{
		Username: u.Username, Password: u.Password, Role: u.Role,
	}, context.Background()); err != nil {
		t.Fatalf("create user: %v", err)
	}
	got, err := user.GetByUsername(u.Username, context.Background())
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	uid = got.ID
	key := model.APIKey{UserID: uid, Name: "k", APIKey: "sk-lodestar-usage-test"}
	if err := apikey.Create(&key, context.Background()); err != nil {
		t.Fatalf("create apikey: %v", err)
	}
	// Reset the relay log cache to a known empty state between tests.
	t.Cleanup(relaylog.SetCacheForTest(nil))
	return uid, key.ID
}

// nowLog builds a RelayLog at time t for keyID/modelName.
func nowLog(id int64, t int64, keyID int, modelName string, cost float64) model.RelayLog {
	return model.RelayLog{
		ID:               id,
		Time:             t,
		RequestModelName: modelName,
		ActualModelName:  modelName,
		RequestAPIKeyID:  keyID,
		InputTokens:      10,
		OutputTokens:     5,
		Cost:             cost,
	}
}

// TestWO023_T1_UnflushedLogVisibleFromCache 缺陷 A 核心：一条只进内存缓存、未触发
// 刷盘（缓存 < 200 条）的日志，必须立刻被 ModelBreakdownForUser / DailySeriesForUser
// 看到。修复前这两函数直查 DB，缓存里的行查不到 → requests=0 → 红。
func TestWO023_T1_UnflushedLogVisibleFromCache(t *testing.T) {
	uid, keyID := initUsageTestDB(t)
	now := time.Now().Unix()
	restore := relaylog.SetCacheForTest([]model.RelayLog{
		nowLog(1001, now, keyID, "Qwen3-Max", 0.0000072),
	})
	defer restore()

	models, ok, err := ModelBreakdownForUser(uid, 30, 16, context.Background())
	if err != nil || !ok {
		t.Fatalf("ModelBreakdownForUser ok=%v err=%v", ok, err)
	}
	if len(models) != 1 || models[0].Model != "Qwen3-Max" || models[0].Requests != 1 {
		t.Fatalf("T1 model breakdown = %+v, want 1 row Qwen3-Max requests=1（未刷盘日志必须可见）", models)
	}
	if models[0].Cost < 0.0000071 || models[0].Cost > 0.0000073 {
		t.Fatalf("T1 model cost = %v, want ~0.0000072", models[0].Cost)
	}

	series, sok, serr := DailySeriesForUser(uid, 14, context.Background())
	if serr != nil || !sok {
		t.Fatalf("DailySeriesForUser ok=%v err=%v", sok, serr)
	}
	today := time.Now().Format("20060102")
	var todayPoint *DailyPoint
	for i := range series {
		if series[i].Date == today {
			todayPoint = &series[i]
			break
		}
	}
	if todayPoint == nil || todayPoint.Requests != 1 {
		t.Fatalf("T1 daily today = %+v, want requests=1（未刷盘日志必须进入分日）", todayPoint)
	}
}

// TestWO023_T2_NoDoubleCountWhenInBothCacheAndDB 方案 1 核心风险：同一条日志同时
// 存在于内存缓存与 DB（刷盘 race 窗口 / 测试种子重叠）时，requests 必须只算一次。
// 实现靠把缓存里的 id 从 DB 查询里排除（id NOT IN cacheIDs），任何一边漏掉都会
// 在这里红。M-b（合并时不做 id 去重）正是要这条红。
func TestWO023_T2_NoDoubleCountWhenInBothCacheAndDB(t *testing.T) {
	uid, keyID := initUsageTestDB(t)
	now := time.Now().Unix()
	row := nowLog(2002, now, keyID, "Qwen3-Max", 0.0000072)

	// 1) 同一条日志既进 DB（模拟已刷盘）…
	if err := db.GetLogDB().Create(&row).Error; err != nil {
		t.Fatalf("seed DB: %v", err)
	}
	// 2) …又留在缓存里（模拟刷盘后缓存前缀尚未截断的窗口）。
	restore := relaylog.SetCacheForTest([]model.RelayLog{row})
	defer restore()

	models, ok, err := ModelBreakdownForUser(uid, 30, 16, context.Background())
	if err != nil || !ok {
		t.Fatalf("ModelBreakdownForUser ok=%v err=%v", ok, err)
	}
	if len(models) != 1 || models[0].Requests != 1 {
		t.Fatalf("T2 double-count: models=%+v, want exactly 1 request for the overlapping row（id 去重失效会让 requests=2）", models)
	}
	if models[0].Cost < 0.0000071 || models[0].Cost > 0.0000073 {
		t.Fatalf("T2 cost = %v, want ~0.0000072（去重漏了会让 cost 翻倍）", models[0].Cost)
	}
}

// TestWO023_T3_FlushedLogStillVisibleFromDB 刷盘后缓存清空，日志必须仍能从 DB
// 读到。这条保护"修复不要把 DB 路径改坏"——M-c（只合并 per_model 不合并 daily）
// 也会让 daily 断言在这里红。
func TestWO023_T3_FlushedLogStillVisibleFromDB(t *testing.T) {
	uid, keyID := initUsageTestDB(t)
	now := time.Now().Unix()
	row := nowLog(3003, now, keyID, "Qwen3-Max", 0.0000072)
	if err := db.GetLogDB().Create(&row).Error; err != nil {
		t.Fatalf("seed DB: %v", err)
	}
	// 缓存为空，模拟刷盘已经把前缀截断后的稳态。
	restore := relaylog.SetCacheForTest(nil)
	defer restore()

	models, ok, err := ModelBreakdownForUser(uid, 30, 16, context.Background())
	if err != nil || !ok {
		t.Fatalf("ModelBreakdownForUser ok=%v err=%v", ok, err)
	}
	if len(models) != 1 || models[0].Requests != 1 {
		t.Fatalf("T3 model breakdown = %+v, want 1 row requests=1 from DB", models)
	}
	series, sok, serr := DailySeriesForUser(uid, 14, context.Background())
	if serr != nil || !sok {
		t.Fatalf("DailySeriesForUser ok=%v err=%v", sok, serr)
	}
	today := time.Now().Format("20060102")
	var todayPoint *DailyPoint
	for i := range series {
		if series[i].Date == today {
			todayPoint = &series[i]
			break
		}
	}
	if todayPoint == nil || todayPoint.Requests != 1 {
		t.Fatalf("T3 daily today = %+v, want requests=1 from DB", todayPoint)
	}
}
