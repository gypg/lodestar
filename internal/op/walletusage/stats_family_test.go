package walletusage

/*
WO-042：/api/v1/stats/* 五端点的租户收窄。

缺陷（WO-041 生产实测）：getStatsToday/Daily/Hourly/Total 直接返回全站缓存，
getStatsChannel 返回真实渠道名。客户（user 角色，持 stats:read）在主页看到
上游渠道实名与全站聚合。

处置（CC 拍板走 B，优先体验）：
  - /channel       → 非 staff 返空集（渠道名是 upstream config，收窄也泄露）
  - /today|hourly|daily → 按 key 归属从 relay_logs 重建同形 StatsMetrics
    （必须走 walletusage 的 loadUserLogsMerged：内存未刷盘 + DB 已刷盘去重合并，
    否则"总额变了、分日没变"）
  - /total         → stats.APIKeysAggregate 累计（日志保留期 7 天，累计查不出）

本文件测 op 层重建函数 FamilyForAPIKeys / HourlySeriesForAPIKeys /
DailySeriesForAPIKeys 的分桶、成功判据（error = '' OR IS NULL 双覆盖）、
保留期关闭语义、日志保留不足 7 天的真实空洞（不造数）。
*/

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
)

func initFamilyScopeDB(t *testing.T) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "stats-family.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	if err := db.InitLogDB("", "", false); err != nil {
		t.Fatalf("InitLogDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("RefreshCache: %v", err)
	}
	if err := setting.SetString(model.SettingKeyRelayLogKeepEnabled, "true"); err != nil {
		t.Fatalf("enable keep: %v", err)
	}
	if err := apikey.RefreshCache(context.Background()); err != nil {
		t.Fatalf("apikey refresh: %v", err)
	}
	// relaylog 内存缓存是包级全局：跨测试的写入会串进"今天"重建，逐测试清空。
	restore := relaylog.SetCacheForTest(nil)
	t.Cleanup(restore)
}

func seedUserKey(t *testing.T, uid uint, keyID int, name string) {
	t.Helper()
	if err := db.GetDB().Create(&model.User{ID: uid, Username: name + "-u", Password: "x", Role: model.UserRoleUser, Quota: 5}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.GetDB().Create(&model.APIKey{ID: keyID, UserID: uid, Name: name, APIKey: "sk-lodestar-" + name, Enabled: true}).Error; err != nil {
		t.Fatalf("create key: %v", err)
	}
	if err := apikey.RefreshCache(context.Background()); err != nil {
		t.Fatalf("rebuild key cache: %v", err)
	}
}

func seedFamilyLog(t *testing.T, id int64, keyID int, at time.Time, in, out int64, cost float64, useTimeMs int64, ftut int64, errMsg string, attempts int) {
	t.Helper()
	l := model.RelayLog{
		ID: id, Time: at.Unix(),
		RequestModelName: "wo042-model", ActualModelName: "wo042-model",
		RequestAPIKeyID: keyID,
		ChannelId:       1, ChannelName: "secret-channel",
		InputTokens: int(in), OutputTokens: int(out), Cost: cost,
		UseTime: int(useTimeMs), Ftut: int(ftut),
		Error: errMsg, TotalAttempts: attempts,
	}
	if err := db.GetLogDB().Create(&l).Error; err != nil {
		t.Fatalf("seed relay log: %v", err)
	}
}

// famSeedTime 返回一个用于播种的时间戳，保证（1）与 now 同一本地日——today/
// hourly/daily 的分桶都按本地日期串比较；（2）不早于 today 族的 cutoff
// （time.Now().Truncate(24h) = UTC 零点——DB 查询按 time >= cutoff 过滤）。
// 偏移会跨任一边界时直接塌缩到 now：这些测试断言的是总量，多行同秒不影响；
// 小时级断言必须用返回值的小时，不能用新的 time.Now()。
// 不塌缩的路径下，运行边界竞态（测试内两次取 now 跨 UTC 零点）从分钟级窗口
// 收窄到亚秒级。
func famSeedTime(now time.Time, offset time.Duration) time.Time {
	at := now.Add(-offset)
	if at.Format("20060102") != now.Format("20060102") {
		return now
	}
	if at.Unix() < now.Truncate(24*time.Hour).Unix() {
		return now
	}
	return at
}

// TestFamilyForAPIKeys_BucketsToday：今天的日志进 Today 桶；成功判据双覆盖
// （error=” 与 NULL 都算成功，非空算失败）；token/cost/延迟/FTUT 汇总正确。
func TestFamilyForAPIKeys_BucketsToday(t *testing.T) {
	initFamilyScopeDB(t)
	uid := uint(601)
	seedUserKey(t, uid, 6601, "fam-a")

	now := time.Now()
	// 成功（error=''）、成功（error=NULL 的写法在 sqlite 里用 SQL 注入 NULL：这里用另一行保存时留零值再 update 模拟）
	seedFamilyLog(t, 7001, 6601, famSeedTime(now, time.Hour), 100, 50, 0.30, 1200, 300, "", 1)
	seedFamilyLog(t, 7002, 6601, famSeedTime(now, 2*time.Hour), 40, 10, 0.10, 800, 200, "", 1)
	// 失败（非空 error）
	seedFamilyLog(t, 7003, 6601, famSeedTime(now, 3*time.Hour), 5, 0, 0, 3000, 0, "upstream boom", 2)
	// NULL error 行：建后改 NULL
	seedFamilyLog(t, 7004, 6601, famSeedTime(now, 30*time.Minute), 60, 40, 0.20, 500, 100, "", 1)
	if err := db.GetLogDB().Model(&model.RelayLog{}).Where("id = ?", 7004).Update("error", nil).Error; err != nil {
		t.Fatalf("null error: %v", err)
	}

	m, ok, err := FamilyForAPIKeys([]int{6601}, "today")
	if err != nil || !ok {
		t.Fatalf("FamilyForAPIKeys today: ok=%v err=%v", ok, err)
	}
	if m.RequestSuccess != 3 || m.RequestFailed != 1 {
		t.Fatalf("success=%d failed=%d, want 3/1 — the success predicate must accept both '' and NULL error", m.RequestSuccess, m.RequestFailed)
	}
	if m.InputToken != 205 || m.OutputToken != 100 {
		t.Fatalf("tokens in=%d out=%d, want 205/100", m.InputToken, m.OutputToken)
	}
	if m.InputCost+m.OutputCost < 0.59 || m.InputCost+m.OutputCost > 0.61 {
		t.Fatalf("cost=%f, want ~0.60", m.InputCost+m.OutputCost)
	}
	if m.WaitTime != 1200+800+3000+500 {
		t.Fatalf("wait_time=%d, want 5500", m.WaitTime)
	}
}

// TestFamilyForAPIKeys_ScopeIsolation：别人的日志不进自己的桶。
func TestFamilyForAPIKeys_ScopeIsolation(t *testing.T) {
	initFamilyScopeDB(t)
	uid := uint(602)
	seedUserKey(t, uid, 6602, "fam-b")
	seedUserKey(t, 603, 6603, "fam-other")

	now := time.Now()
	seedFamilyLog(t, 7011, 6602, famSeedTime(now, time.Hour), 10, 5, 0.05, 100, 50, "", 1)
	seedFamilyLog(t, 7012, 6603, famSeedTime(now, time.Hour), 900, 500, 9.00, 9000, 900, "", 1)

	m, ok, err := FamilyForAPIKeys([]int{6602}, "today")
	if err != nil || !ok {
		t.Fatalf("today: ok=%v err=%v", ok, err)
	}
	if m.InputToken != 10 || m.OutputToken != 5 {
		t.Fatalf("tokens=%d/%d, want 10/5 — other users' logs must not leak", m.InputToken, m.OutputToken)
	}
	if m.RequestSuccess != 1 {
		t.Fatalf("success=%d, want 1", m.RequestSuccess)
	}
}

// TestFamilyForAPIKeys_DailyBuckets：分天分桶 + 空 day 填零（不造数）+ 保留期外不出现。
func TestDailySeriesForAPIKeys_Buckets(t *testing.T) {
	initFamilyScopeDB(t)
	uid := uint(604)
	seedUserKey(t, uid, 6604, "fam-c")

	now := time.Now()
	today := now.Format("20060102")
	yesterday := now.AddDate(0, 0, -1).Format("20060102")
	seedFamilyLog(t, 7021, 6604, famSeedTime(now, 2*time.Hour), 30, 20, 0.15, 400, 80, "", 1)
	seedFamilyLog(t, 7022, 6604, now.AddDate(0, 0, -1), 50, 30, 0.25, 600, 120, "", 1)
	// 8 天前：超出 7 天默认窗口，不得出现在 7 天序列里
	seedFamilyLog(t, 7023, 6604, now.AddDate(0, 0, -8), 999, 999, 9.99, 999, 999, "", 1)

	series, ok, err := DailySeriesForAPIKeys([]int{6604}, 7)
	if err != nil || !ok {
		t.Fatalf("daily: ok=%v err=%v", ok, err)
	}
	if len(series) != 7 {
		t.Fatalf("series len=%d, want 7", len(series))
	}
	byDate := make(map[string]model.StatsMetrics)
	for _, p := range series {
		byDate[p.Date] = p.StatsMetrics
	}
	if y, okk := byDate[yesterday]; !okk || y.RequestSuccess != 1 || y.InputToken != 50 {
		t.Fatalf("yesterday bucket wrong: %+v", byDate[yesterday])
	}
	if tt, okk := byDate[today]; !okk || tt.RequestSuccess != 1 || tt.InputToken != 30 {
		t.Fatalf("today bucket wrong: %+v", byDate[today])
	}
	for _, p := range series {
		if p.Date == now.AddDate(0, 0, -8).Format("20060102") {
			t.Fatal("8-day-old log must not appear in a 7-day series (retention is real)")
		}
	}
	// 无日志的天填零
	emptyDate := now.AddDate(0, 0, -3).Format("20060102")
	if e := byDate[emptyDate]; e.RequestSuccess != 0 || e.InputToken != 0 {
		t.Fatalf("empty day must be zero-filled, got %+v", e)
	}
}

// TestFamilyForAPIKeys_KeepDisabled：relay_log_keep_enabled 关闭 → ok=false
// （前端据此显示不可用），绝不静默返回全零。
func TestFamilyForAPIKeys_KeepDisabled(t *testing.T) {
	initFamilyScopeDB(t)
	uid := uint(605)
	seedUserKey(t, uid, 6605, "fam-d")
	seedFamilyLog(t, 7031, 6605, famSeedTime(time.Now(), time.Hour), 10, 5, 0.05, 100, 50, "", 1)
	if err := setting.SetString(model.SettingKeyRelayLogKeepEnabled, "false"); err != nil {
		t.Fatalf("disable keep: %v", err)
	}

	_, ok, err := FamilyForAPIKeys([]int{6605}, "today")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ok {
		t.Fatal("ok=true with relay_log_keep_enabled off — must report unavailable, not silent zeros")
	}
	_, ok, _ = DailySeriesForAPIKeys([]int{6605}, 7)
	if ok {
		t.Fatal("daily: ok=true with keep off")
	}
	_, ok, _ = HourlySeriesForAPIKeys([]int{6605})
	if ok {
		t.Fatal("hourly: ok=true with keep off")
	}
	// 恢复开关，避免污染后续测试（setting 缓存是包级全局）。
	if err := setting.SetString(model.SettingKeyRelayLogKeepEnabled, "true"); err != nil {
		t.Fatalf("restore keep: %v", err)
	}
}

// TestHourlySeriesForAPIKeys_Buckets：24 桶、只有当天。
func TestHourlySeriesForAPIKeys_Buckets(t *testing.T) {
	initFamilyScopeDB(t)
	uid := uint(606)
	seedUserKey(t, uid, 6606, "fam-e")

	now := time.Now()
	hour := famSeedTime(now, 10*time.Minute)
	seedFamilyLog(t, 7041, 6606, hour, 20, 10, 0.10, 200, 60, "", 1)

	buckets, ok, err := HourlySeriesForAPIKeys([]int{6606})
	if err != nil || !ok {
		t.Fatalf("hourly: ok=%v err=%v", ok, err)
	}
	if len(buckets) != 24 {
		t.Fatalf("len=%d, want 24", len(buckets))
	}
	// 断言对齐播种时间的小时，而不是新的 time.Now()：播种与分桶调用之间
	// 跨整点时（每小时前 10 分钟的窗口），now.Hour() 已 +1 而日志仍在上一桶。
	cur := buckets[hour.Hour()]
	if cur.RequestSuccess != 1 || cur.InputToken != 20 {
		t.Fatalf("seeded hour bucket = %+v, want 1 success / 20 in", cur)
	}
	// 其他桶全零
	other := (hour.Hour() + 1) % 24
	if buckets[other].RequestSuccess != 0 {
		t.Fatalf("empty hour bucket must be zero, got %+v", buckets[other])
	}
}
