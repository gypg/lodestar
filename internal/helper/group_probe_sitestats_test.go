package helper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	appmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/transformer/outbound"
)

/*
站点渠道探测必须把 attempts 记进站点模型小时桶（stats_site_model_hourlies）。

缺陷：testGroupModelItem 在 group_probe.go:413 构造了完整的 []ChannelAttempt，
却只交给 recordTestLog（写 relay_logs），从未交给 op.StatsSiteModelHourlyRecordAttempts。
后果：站点渠道的"使用历史/可用性"图表对探测流量恒为 0，只有真实 relay 才会累加。

这不是有意排除测试流量：回填任务 stats_site_model_backfill.go:96 的 WHERE 只过滤
time/id，不过滤 is_test，所以探测记录在任何一次回填后**都会**进桶。活体探测不进桶
与回填进桶自相矛盾，修的是这个不一致。

入口选 TestGroupModels（导出的探测入口），不直接调 recordSiteModelStats：
后者只能守住"函数体会不会累加"，守不住"调用点有没有把 attempts 传进去"——
正是 [[lodestar-worker-false-evidence]] 第七变体（绕过调用点的测试冒充守卫）。

观测点选导出的 op.SiteChannelModelHourlyForAccount，它会合并尚未刷盘的内存桶，
因此无需等待 SaveDB。断言 success/failure 的**具体数值**，不只是"非零"。

变异验收（4 杀 1 等价）：
  - 传 nil attempts（≈删调用点）→ 杀
  - 只记最后一跳 → 杀（failure=1≠3，"非零"断言抓不到）
  - success/failed 语义互换 → 杀
  - 去掉 binding.Found 守卫 → 杀（须查内存桶总量：错误桶落在 SiteAccountID=0，
    按 account 查或数 DB 行数都会漏，第一版就是这样存活的）
  - fallbackModel 传 ""（本文件不断言）→ **等价变异，无法杀**：
    group_probe.go:417 恒把 item.ModelName 写进 attempt.ModelName，
    op 侧 fallback 分支（stats_site_model.go:97）从本调用点不可达。
    仍按原样传 item.ModelName 作为防御性参数，不是测试缺口。
*/

// newProbeUpstreamOK 返回一个恒回合法 chat completion 的上游。
func newProbeUpstreamOK(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
}

// newProbeUpstream429 返回一个恒回 429 的上游，用于触发三次重试。
func newProbeUpstream429(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
	}))
}

// probeGroupFor 构造单渠道单模型的探测分组。
func probeGroupFor(channelID int, channelName, baseURL, modelName string) (*appmodel.Group, map[int]appmodel.Channel) {
	channels := map[int]appmodel.Channel{
		channelID: {
			ID:       channelID,
			Name:     channelName,
			Type:     outbound.OutboundTypeOpenAIChat,
			Enabled:  true,
			BaseUrls: []appmodel.BaseUrl{{URL: baseURL}},
			Keys:     []appmodel.ChannelKey{{Enabled: true, ChannelKey: "sk-probe"}},
		},
	}
	group := &appmodel.Group{
		Name:         "probe-group-" + channelName,
		EndpointType: appmodel.EndpointTypeAll,
		Items: []appmodel.GroupItem{
			{ID: 1, ChannelID: channelID, ModelName: modelName, Priority: 1, Weight: 1},
		},
	}
	return group, channels
}

// seedSiteBinding 建立 channelID → site account 的绑定，让 lookupChannelSiteBinding
// 能解析出站点。没有绑定时 StatsSiteModelHourlyUpdate 会静默丢弃（非站点渠道）。
// initSiteStatsTestEnv 在日志测试环境之上清空站点模型内存桶。桶是包级全局的，
// 不重置会让"总量为 0"的断言依赖用例执行顺序（实测同包跑时会看到前序用例的 2 个桶）。
func initSiteStatsTestEnv(t *testing.T) context.Context {
	t.Helper()
	ctx := initGroupProbeLogTestEnv(t)
	op.StatsSiteModelHourlyResetForTest()
	t.Cleanup(op.StatsSiteModelHourlyResetForTest)
	return ctx
}

func seedSiteBinding(t *testing.T, channelID, siteAccountID int, groupKey string) {
	t.Helper()

	// 绑定行有指向 sites / site_accounts 的外键，父行必须先存在。
	site := appmodel.Site{
		ID:       siteAccountID,
		Name:     "probe-site-" + groupKey + "-" + strconv.Itoa(siteAccountID),
		Platform: appmodel.SitePlatformNewAPI,
		BaseURL:  "http://127.0.0.1:1",
		Enabled:  true,
	}
	if err := db.GetDB().Create(&site).Error; err != nil {
		t.Fatalf("seed site: %v", err)
	}
	account := appmodel.SiteAccount{
		ID:             siteAccountID,
		SiteID:         site.ID,
		Name:           "probe-account",
		CredentialType: appmodel.SiteCredentialTypeAPIKey,
		APIKey:         "sk-probe-account",
		Enabled:        true,
	}
	if err := db.GetDB().Create(&account).Error; err != nil {
		t.Fatalf("seed site account: %v", err)
	}

	binding := appmodel.SiteChannelBinding{
		SiteID:        site.ID,
		SiteAccountID: account.ID,
		GroupKey:      groupKey,
		ChannelID:     channelID,
	}
	if err := db.GetDB().Create(&binding).Error; err != nil {
		t.Fatalf("seed site channel binding: %v", err)
	}
}

// siteModelHistory 取出指定 (account, group, model) 的历史聚合。
func siteModelHistory(t *testing.T, ctx context.Context, siteAccountID int, groupKey, modelName string) *appmodel.SiteModelHistorySummary {
	t.Helper()
	summaries, err := op.SiteChannelModelHourlyForAccount(ctx, siteAccountID)
	if err != nil {
		t.Fatalf("SiteChannelModelHourlyForAccount() error = %v", err)
	}
	return summaries[groupKey+"\x00"+modelName]
}

// TestGroupModelsRecordsSuccessfulProbeInSiteStats：一次成功探测必须让站点桶 success=1。
func TestGroupModelsRecordsSuccessfulProbeInSiteStats(t *testing.T) {
	ctx := initSiteStatsTestEnv(t)

	upstream := newProbeUpstreamOK(t)
	defer upstream.Close()

	const channelID = 940001
	const siteAccountID = 4201
	const groupKey = "default"
	seedSiteBinding(t, channelID, siteAccountID, groupKey)

	group, channels := probeGroupFor(channelID, "probe-site-ok", upstream.URL, "gpt-4o-mini")

	summary, err := TestGroupModels(ctx, group, channels)
	if err != nil {
		t.Fatalf("TestGroupModels() error = %v", err)
	}
	if !summary.Passed {
		t.Fatalf("TestGroupModels() summary.Passed = false, results = %+v", summary.Results)
	}

	history := siteModelHistory(t, ctx, siteAccountID, groupKey, "gpt-4o-mini")
	if history == nil {
		t.Fatal("site model history = nil: the probe never reached the site usage buckets")
	}
	if history.SuccessCount != 1 {
		t.Fatalf("history.SuccessCount = %d, want 1", history.SuccessCount)
	}
	if history.FailureCount != 0 {
		t.Fatalf("history.FailureCount = %d, want 0 on a passing probe", history.FailureCount)
	}
	if history.LastRequestAt == nil {
		t.Fatal("history.LastRequestAt = nil, want the probe timestamp")
	}
}

// TestGroupModelsRecordsRetriedFailureInSiteStats：三次重试全失败必须记 failure=3，
// 不是 1。只断言"非零"不会发现只记了最后一跳的实现。
func TestGroupModelsRecordsRetriedFailureInSiteStats(t *testing.T) {
	ctx := initSiteStatsTestEnv(t)

	upstream := newProbeUpstream429(t)
	defer upstream.Close()

	const channelID = 940002
	const siteAccountID = 4202
	const groupKey = "default"
	seedSiteBinding(t, channelID, siteAccountID, groupKey)

	group, channels := probeGroupFor(channelID, "probe-site-429", upstream.URL, "gpt-4o-mini")

	summary, err := TestGroupModels(ctx, group, channels)
	if err != nil {
		t.Fatalf("TestGroupModels() error = %v", err)
	}
	if summary.Passed {
		t.Fatal("TestGroupModels() summary.Passed = true, want false for a 429-only upstream")
	}

	history := siteModelHistory(t, ctx, siteAccountID, groupKey, "gpt-4o-mini")
	if history == nil {
		t.Fatal("site model history = nil: the failed probe never reached the site usage buckets")
	}
	if history.FailureCount != 3 {
		t.Fatalf("history.FailureCount = %d, want 3 (the probe retries three times)", history.FailureCount)
	}
	if history.SuccessCount != 0 {
		t.Fatalf("history.SuccessCount = %d, want 0 when every attempt failed", history.SuccessCount)
	}
}

// TestGroupModelsSkipsSiteStatsForUnboundChannel：非站点渠道（无绑定）不得产生桶。
// 防止修复退化成"给所有渠道都记站点统计"。
func TestGroupModelsSkipsSiteStatsForUnboundChannel(t *testing.T) {
	ctx := initSiteStatsTestEnv(t)

	upstream := newProbeUpstreamOK(t)
	defer upstream.Close()

	const channelID = 940003
	const unboundAccountID = 4203
	// 故意不建绑定。

	group, channels := probeGroupFor(channelID, "probe-site-unbound", upstream.URL, "gpt-4o-mini")

	if _, err := TestGroupModels(ctx, group, channels); err != nil {
		t.Fatalf("TestGroupModels() error = %v", err)
	}

	// 观测点必须是内存桶总量，不能只查某个 account 或数 DB 行数：
	// 去掉 binding.Found 守卫后，无绑定渠道会以 SiteAccountID=0 建桶，
	// 既不落在被查询的 account 下，也还没刷盘，两种查法都会漏（M4 实测存活）。
	if n := op.StatsSiteModelHourlyPendingCountForTest(); n != 0 {
		t.Fatalf("pending site model buckets = %d, want 0 for an unbound channel", n)
	}
	if history := siteModelHistory(t, ctx, unboundAccountID, "default", "gpt-4o-mini"); history != nil {
		t.Fatalf("site model history = %+v, want nil for an unbound channel", history)
	}
}
