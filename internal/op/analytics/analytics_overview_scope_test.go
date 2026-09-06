package analytics

/*
WO-040 ②：AnalyticsOverviewGet 的多租户隔离。

缺陷：userID 只到达 apikey.ListByUser（API 密钥数那一卡），其余数据全部全站：
用量 4 指标来自 stats_daily（无 user 维度）、provider/model 数来自全站渠道表、
回退率来自全站 relay_logs。user 角色持 stats:read，主页「中枢概览」能看到
其他租户的消费额与请求量。

本文件钉死：非 staff（userID 非 nil）的 overview ——
  - 用量 4 指标只含该用户 API key 的累计统计（stats_api_keys 累计口径，
    StatsAPIKey 无日期维度，range 退化为累计是已知取舍）；
  - 回退率只统计该用户 key 的 relay_logs；
  - provider/model 数对非 staff 置零（站点规模信息不外泄）；
  - APIKeyCount 只数自己的（既有行为，回归守卫）。
staff（userID nil）保持全站口径。
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
	"github.com/gypg/lodestar/internal/op/stats"
)

func initOverviewScopeDB(t *testing.T) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "analytics-scope.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	if err := db.InitLogDB("", "", false); err != nil {
		t.Fatalf("InitLogDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("RefreshCache failed: %v", err)
	}
	if err := setting.SetString(model.SettingKeyRelayLogKeepEnabled, "true"); err != nil {
		t.Fatalf("enable relay log keep failed: %v", err)
	}
	if err := apikey.RefreshCache(context.Background()); err != nil {
		t.Fatalf("apikey RefreshCache failed: %v", err)
	}
}

func seedOverviewUser(t *testing.T, name string) uint {
	t.Helper()
	u := model.User{Username: name, Password: "x", Role: model.UserRoleUser, Quota: 5}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatalf("create user %s: %v", name, err)
	}
	return u.ID
}

func seedOverviewKey(t *testing.T, uid uint, keyID int, name string) {
	t.Helper()
	k := model.APIKey{ID: keyID, UserID: uid, Name: name, APIKey: "sk-lodestar-" + name, Enabled: true}
	if err := db.GetDB().Create(&k).Error; err != nil {
		t.Fatalf("create key %s: %v", name, err)
	}
	if err := apikey.RefreshCache(context.Background()); err != nil {
		t.Fatalf("rebuild key cache: %v", err)
	}
}

func seedOverviewRelayLog(t *testing.T, id int64, keyID int, attempts int, cost float64) {
	t.Helper()
	l := model.RelayLog{
		ID: id, Time: time.Now().Unix(),
		RequestModelName: "wo040-model", ActualModelName: "wo040-model",
		RequestAPIKeyID: keyID,
		ChannelId:       1, ChannelName: "wo040-channel",
		InputTokens: 10, OutputTokens: 5, Cost: cost,
		TotalAttempts: attempts,
	}
	if err := db.GetLogDB().Create(&l).Error; err != nil {
		t.Fatalf("seed relay log: %v", err)
	}
}

// TestAnalyticsOverviewGet_UserScopeIsolates 隔离主断言。
func TestAnalyticsOverviewGet_UserScopeIsolates(t *testing.T) {
	initOverviewScopeDB(t)

	userA := seedOverviewUser(t, "scope-user-a")
	seedOverviewKey(t, userA, 8801, "scope-key-a")
	userB := seedOverviewUser(t, "scope-user-b")
	seedOverviewKey(t, userB, 8802, "scope-key-b")

	// keyA：1 次成功、20 token、$0.30；keyB：5 次成功、500 token、$9.00。
	stats.APIKeyUpdate(8801, model.StatsMetrics{RequestSuccess: 1, InputToken: 15, OutputToken: 5, InputCost: 0.20, OutputCost: 0.10})
	stats.APIKeyUpdate(8802, model.StatsMetrics{RequestSuccess: 5, InputToken: 400, OutputToken: 100, InputCost: 7.00, OutputCost: 2.00})

	// relay_logs：keyA 无回退（attempts=1）、keyB 有回退（attempts=2）。
	seedOverviewRelayLog(t, 901, 8801, 1, 0.30)
	seedOverviewRelayLog(t, 902, 8802, 2, 9.00)
	restore := relaylog.SetCacheForTest(nil)
	t.Cleanup(restore)

	// stats_daily：site-wide 用量 4 指标的数据源（生产由 stats 管道持续写入）。
	// 今天 + 昨天，合计与 per-key 灌入量对齐：2 次成功、500+20 token、$9.3。
	today := time.Now().Format("20060102")
	yesterday := time.Now().AddDate(0, 0, -1).Format("20060102")
	for date, succ := range map[string]int64{today: 1, yesterday: 1} {
		row := model.StatsDaily{Date: date, StatsMetrics: model.StatsMetrics{
			RequestSuccess: succ, InputToken: 250, OutputToken: 60,
			InputCost: 3.60, OutputCost: 1.05,
		}}
		if err := db.GetDB().Create(&row).Error; err != nil {
			t.Fatalf("seed stats_daily %s: %v", date, err)
		}
	}

	userAID := userA
	scoped, err := AnalyticsOverviewGet(context.Background(), model.AnalyticsRange7D, &userAID)
	if err != nil {
		t.Fatalf("scoped overview: %v", err)
	}

	if scoped.RequestCount != 1 {
		t.Fatalf("scoped request count = %d, want 1 (user A only)", scoped.RequestCount)
	}
	if scoped.TotalTokens != 20 {
		t.Fatalf("scoped total tokens = %d, want 20 (user A only)", scoped.TotalTokens)
	}
	if scoped.TotalCost < 0.29 || scoped.TotalCost > 0.31 {
		t.Fatalf("scoped total cost = %f, want ~0.30 (user A only)", scoped.TotalCost)
	}
	if scoped.SuccessRate < 99.9 || scoped.SuccessRate > 100.1 {
		t.Fatalf("scoped success rate = %f, want ~100 (user A had no failures)", scoped.SuccessRate)
	}
	if scoped.FallbackRate != 0 {
		t.Fatalf("scoped fallback rate = %f, want 0 (user A never fell back; user B's fallback must not leak)", scoped.FallbackRate)
	}
	if scoped.ProviderCount != 0 {
		t.Fatalf("scoped provider count = %d, want 0 — site scale must not leak to customers", scoped.ProviderCount)
	}
	if scoped.ModelCount != 0 {
		t.Fatalf("scoped model count = %d, want 0 — site scale must not leak to customers", scoped.ModelCount)
	}
	if scoped.APIKeyCount != 1 {
		t.Fatalf("scoped api key count = %d, want 1 (own keys only)", scoped.APIKeyCount)
	}

	// staff（userID=nil）：全站口径——两用户合计。
	all, err := AnalyticsOverviewGet(context.Background(), model.AnalyticsRange7D, nil)
	if err != nil {
		t.Fatalf("site-wide overview: %v", err)
	}
	if all.RequestCount != 2 {
		t.Fatalf("site-wide request count = %d, want 2 (stats_daily rows)", all.RequestCount)
	}
	if all.FallbackRate <= 0 {
		t.Fatalf("site-wide fallback rate = %f, want >0 (user B fell back)", all.FallbackRate)
	}
	if all.TotalCost < 9.0 {
		t.Fatalf("site-wide total cost = %f, want >= 9.3 (both users)", all.TotalCost)
	}
	if all.TotalTokens != 620 {
		t.Fatalf("site-wide total tokens = %d, want 620", all.TotalTokens)
	}
}

// TestAnalyticsOverviewGet_UserWithNoKeys 零 key 用户：全零而不是全站数字。
func TestAnalyticsOverviewGet_UserWithNoKeys(t *testing.T) {
	initOverviewScopeDB(t)

	seedOverviewUser(t, "scope-user-empty")
	seedOverviewKey(t, seedOverviewUser(t, "scope-user-other"), 8803, "scope-key-other")
	stats.APIKeyUpdate(8803, model.StatsMetrics{RequestSuccess: 7, InputCost: 3.5})
	seedOverviewRelayLog(t, 911, 8803, 1, 3.5)
	restore := relaylog.SetCacheForTest(nil)
	t.Cleanup(restore)

	empty := seedOverviewUser(t, "scope-user-nokeys")
	uid := empty
	got, err := AnalyticsOverviewGet(context.Background(), model.AnalyticsRange7D, &uid)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if got.RequestCount != 0 || got.TotalTokens != 0 || got.TotalCost != 0 {
		t.Fatalf("keyless user got site data: %+v", got.AnalyticsMetrics)
	}
	if got.FallbackRate != 0 {
		t.Fatalf("keyless user fallback rate = %f, want 0", got.FallbackRate)
	}
}

// TestAPIKeysAggregate 累计聚合本体：缺省 key 跳过、已知 key 求和。
func TestAPIKeysAggregate(t *testing.T) {
	initOverviewScopeDB(t)
	stats.APIKeyUpdate(7701, model.StatsMetrics{RequestSuccess: 2, InputToken: 30, OutputToken: 10, InputCost: 0.3, OutputCost: 0.1})
	stats.APIKeyUpdate(7702, model.StatsMetrics{RequestSuccess: 3, InputToken: 70, OutputToken: 20, InputCost: 0.7, OutputCost: 0.2})

	got := stats.APIKeysAggregate([]int{7701, 7702, 9999})
	if got.RequestSuccess != 5 || got.InputToken != 100 || got.OutputToken != 30 {
		t.Fatalf("aggregate = %+v, want summed across 7701+7702", got)
	}
	if got.InputCost < 0.99 || got.InputCost > 1.01 || got.OutputCost < 0.29 || got.OutputCost > 0.31 {
		t.Fatalf("aggregate costs = in %f out %f, want ~1.0 / ~0.3", got.InputCost, got.OutputCost)
	}
}
