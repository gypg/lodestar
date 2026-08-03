package analytics

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/op/channel"
	"github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/op/navorder"
	"github.com/gypg/lodestar/internal/op/relaylog"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/stats"
	"github.com/gypg/lodestar/internal/relay/balancer"
	"github.com/gypg/lodestar/internal/utils/semantic_cache"
)

const analyticsRouteHealthFailureWindow = 24 * time.Hour

type analyticsAggregateMetrics struct {
	InputTokens    int64
	OutputTokens   int64
	TotalCost      float64
	RequestSuccess int64
	RequestFailed  int64
}

type analyticsSummaryRow struct {
	analyticsAggregateMetrics
	RequestCount  int64
	FallbackCount int64
}

type analyticsProviderAggregateRow struct {
	ChannelID   int
	ChannelName string
	analyticsAggregateMetrics
}

type analyticsModelAggregateRow struct {
	ModelName string
	analyticsAggregateMetrics
}

type analyticsAPIKeyAggregateRow struct {
	APIKeyID int
	Name     string
	analyticsAggregateMetrics
}

type analyticsChannelModelAggregateRow struct {
	ChannelID   int
	ChannelName string
	ModelName   string
	analyticsAggregateMetrics
}

type analyticsFailureAggregateRow struct {
	ChannelID        int
	RequestModelName string
	ActualModelName  string
	FailureCount     int64
	LastFailureAt    int64
}

// AnalyticsOverviewGet returns aggregate analytics for the given range.
// When userID is non-nil, only that user's API keys are counted (multi-tenant isolation).
func AnalyticsOverviewGet(ctx context.Context, r model.AnalyticsRange, userID *uint) (*model.AnalyticsOverview, error) {
	daily, err := stats.GetDaily(ctx)
	if err != nil {
		return nil, err
	}
	mergedDaily := mergeAnalyticsDailyWithToday(daily, stats.TodayGet())
	metrics := aggregateAnalyticsDailyMetrics(mergedDaily, r, stats.Now())

	channels, err := channel.List(ctx)
	if err != nil {
		return nil, err
	}

	// Multi-tenant: scope API key count to the user's own keys when userID is set.
	var apiKeys []model.APIKey
	if userID != nil {
		apiKeys, err = apikey.ListByUser(*userID, ctx)
	} else {
		apiKeys, err = apikey.List(ctx)
	}
	if err != nil {
		return nil, err
	}

	providerCount := 0
	modelNames := make(map[string]struct{})
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		providerCount++
		for _, modelName := range splitAnalyticsChannelModels(ch) {
			modelNames[modelName] = struct{}{}
		}
	}

	apiKeyCount := 0
	for _, apiKey := range apiKeys {
		if apiKey.Enabled {
			apiKeyCount++
		}
	}

	logSummary, err := loadAnalyticsSummary(ctx, r)
	if err != nil {
		return nil, err
	}
	fallbackRate := 0.0
	if logSummary.RequestCount > 0 {
		fallbackRate = (float64(logSummary.FallbackCount) / float64(logSummary.RequestCount)) * 100
	}

	overview := buildAnalyticsOverview(metrics, providerCount, apiKeyCount, len(modelNames), fallbackRate)
	return &overview, nil
}

func AnalyticsUtilizationGet(ctx context.Context, r model.AnalyticsRange) (*model.AnalyticsUtilization, error) {
	providerBreakdown, err := AnalyticsProviderBreakdownGet(ctx, r)
	if err != nil {
		return nil, err
	}
	modelBreakdown, err := AnalyticsModelBreakdownGet(ctx, r)
	if err != nil {
		return nil, err
	}
	apiKeyBreakdown, err := AnalyticsAPIKeyBreakdownGet(ctx, r)
	if err != nil {
		return nil, err
	}

	return &model.AnalyticsUtilization{
		ProviderBreakdown: providerBreakdown,
		ModelBreakdown:    modelBreakdown,
		APIKeyBreakdown:   apiKeyBreakdown,
	}, nil
}

func AnalyticsProviderBreakdownGet(ctx context.Context, r model.AnalyticsRange) ([]model.AnalyticsProviderBreakdownItem, error) {
	rows, err := loadAnalyticsProviderRows(ctx, r)
	if err != nil {
		return nil, err
	}

	channels, err := channel.List(ctx)
	if err != nil {
		return nil, err
	}
	channelByID := make(map[int]model.Channel, len(channels))
	for _, ch := range channels {
		channelByID[ch.ID] = ch
	}

	return buildProviderBreakdown(rows, channelByID), nil
}

func AnalyticsModelBreakdownGet(ctx context.Context, r model.AnalyticsRange) ([]model.AnalyticsModelBreakdownItem, error) {
	rows, err := loadAnalyticsModelRows(ctx, r)
	if err != nil {
		return nil, err
	}
	return buildModelBreakdown(rows), nil
}

func AnalyticsAPIKeyBreakdownGet(ctx context.Context, r model.AnalyticsRange) ([]model.AnalyticsAPIKeyBreakdownItem, error) {
	rows, err := loadAnalyticsAPIKeyRows(ctx, r)
	if err != nil {
		return nil, err
	}
	return buildAPIKeyBreakdown(rows), nil
}

// AnalyticsChannelModelBreakdownGet returns (channel,model) cross-dimensional stats.
func AnalyticsChannelModelBreakdownGet(ctx context.Context, r model.AnalyticsRange, groupID *int) ([]model.AnalyticsChannelModelItem, error) {
	channels, err := channel.List(ctx)
	if err != nil {
		return nil, err
	}
	channelByID := make(map[int]model.Channel, len(channels))
	for _, ch := range channels {
		channelByID[ch.ID] = ch
	}

	scope := make(map[string]struct{})
	if groupID != nil {
		groups, err := group.GroupList(ctx)
		if err != nil {
			return nil, err
		}
		for _, g := range groups {
			if g.ID != *groupID {
				continue
			}
			for _, it := range g.Items {
				scope[strconv.Itoa(it.ChannelID)+"\x00"+it.ModelName] = struct{}{}
			}
		}
	}

	rows, err := loadAnalyticsChannelModelRows(ctx, r, scope)
	if err != nil {
		return nil, err
	}
	return buildChannelModelBreakdown(rows, channelByID), nil
}

// AnalyticsAutoStrategyGet returns Auto strategy runtime snapshot.
func AnalyticsAutoStrategyGet(ctx context.Context, groupID *int) ([]model.AutoStrategySnapshotItem, error) {
	channels, err := channel.List(ctx)
	if err != nil {
		return nil, err
	}
	channelByID := make(map[int]model.Channel, len(channels))
	for _, ch := range channels {
		channelByID[ch.ID] = ch
	}

	var channelIDs []int
	if groupID != nil {
		groups, err := group.GroupList(ctx)
		if err != nil {
			return nil, err
		}
		seen := make(map[int]struct{})
		for _, g := range groups {
			if g.ID != *groupID {
				continue
			}
			for _, it := range g.Items {
				if _, ok := seen[it.ChannelID]; ok {
					continue
				}
				seen[it.ChannelID] = struct{}{}
				channelIDs = append(channelIDs, it.ChannelID)
			}
		}
	}

	snapshot := balancer.GetAutoStatsSnapshot(channelIDs)
	minSamples := balancer.GetAutoStrategyMinSamples()

	items := make([]model.AutoStrategySnapshotItem, 0, len(snapshot))
	for _, s := range snapshot {
		var lastActive int64
		if !s.LastActiveAt.IsZero() {
			lastActive = s.LastActiveAt.Unix()
		}
		chName := ""
		enabled := false
		if c, ok := channelByID[s.ChannelID]; ok {
			chName = c.Name
			enabled = c.Enabled
		}
		items = append(items, model.AutoStrategySnapshotItem{
			ChannelID:     s.ChannelID,
			ChannelName:   chName,
			Enabled:       enabled,
			ModelName:     s.ModelName,
			SuccessRate:   s.SuccessRate * 100,
			SampleCount:   s.SampleCount,
			AvgLatencyMs:  s.AvgLatencyMs,
			LastActiveAt:  lastActive,
			MinSamplesMet: s.SampleCount >= minSamples,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].SuccessRate != items[j].SuccessRate {
			return items[i].SuccessRate < items[j].SuccessRate
		}
		if items[i].SampleCount != items[j].SampleCount {
			return items[i].SampleCount > items[j].SampleCount
		}
		if items[i].ChannelName != items[j].ChannelName {
			return items[i].ChannelName < items[j].ChannelName
		}
		return items[i].ModelName < items[j].ModelName
	})

	return items, nil
}

func AnalyticsGroupHealthGet(ctx context.Context) ([]model.AnalyticsGroupHealthItem, error) {
	groups, err := group.GroupList(ctx)
	if err != nil {
		return nil, err
	}
	channels, err := channel.List(ctx)
	if err != nil {
		return nil, err
	}

	channelByID := make(map[int]model.Channel, len(channels))
	for _, ch := range channels {
		channelByID[ch.ID] = ch
		channelByID[ch.ID] = ch
	}

	failures, err := loadAnalyticsFailureRows(ctx, stats.Now().Add(-analyticsRouteHealthFailureWindow))
	if err != nil {
		return nil, err
	}

	return buildGroupHealth(groups, channelByID, failures), nil
}

func AnalyticsEvaluationGet(_ context.Context) (*model.AnalyticsEvaluationSummary, error) {
	enabled, err := setting.GetBool(model.SettingKeySemanticCacheEnabled)
	if err != nil {
		return nil, err
	}
	ttlSeconds, err := setting.GetInt(model.SettingKeySemanticCacheTTL)
	if err != nil {
		return nil, err
	}
	threshold, err := setting.GetInt(model.SettingKeySemanticCacheThreshold)
	if err != nil {
		return nil, err
	}
	maxEntries, err := setting.GetInt(model.SettingKeySemanticCacheMaxEntries)
	if err != nil {
		return nil, err
	}

	hits, misses, currentEntries := semantic_cache.Stats()
	summary := &model.AnalyticsEvaluationSummary{
		SemanticCache: navorder.BuildSemanticCacheEvaluationSummary(
			enabled,
			semantic_cache.RuntimeEnabled(),
			ttlSeconds,
			threshold,
			maxEntries,
			currentEntries,
			hits,
			misses,
			semantic_cache.GetRuntimeStats(),
		),
	}
	return summary, nil
}

// mergeAnalyticsDailyWithToday merges today's in-memory stats into the daily slice.
func mergeAnalyticsDailyWithToday(daily []model.StatsDaily, today model.StatsDaily) []model.StatsDaily {
	if today.Date == "" {
		return daily
	}

	merged := make([]model.StatsDaily, 0, len(daily)+1)
	replaced := false
	for _, item := range daily {
		if item.Date == today.Date {
			merged = append(merged, today)
			replaced = true
			continue
		}
		merged = append(merged, item)
	}
	if !replaced {
		merged = append(merged, today)
	}
	return merged
}

func aggregateAnalyticsDailyMetrics(daily []model.StatsDaily, r model.AnalyticsRange, now time.Time) model.StatsMetrics {
	startDate := analyticsStartDate(r, now)
	var metrics model.StatsMetrics
	for _, item := range daily {
		if startDate != "" && item.Date < startDate {
			continue
		}
		metrics.Add(item.StatsMetrics)
	}
	return metrics
}

// ---------- Latency distribution ----------

// AnalyticsLatencyDistributionGet returns latency and FTUT distribution for the given range.
func AnalyticsLatencyDistributionGet(ctx context.Context, r model.AnalyticsRange, modelFilter string) (*model.LatencyDistribution, error) {
	return loadLatencyDistribution(ctx, r, modelFilter)
}

// AnalyticsLatencyModelsGet returns the distinct request_model_name values present in
// relay_logs (DB + in-memory cache) within the given range, for populating the model filter.
func AnalyticsLatencyModelsGet(ctx context.Context, r model.AnalyticsRange) ([]string, error) {
	startUnix := analyticsRangeStartUnix(r, stats.Now())
	seen := make(map[string]struct{})

	keepEnabled, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return nil, err
	}

	if keepEnabled {
		query := db.GetDB().WithContext(ctx).
			Model(&model.RelayLog{}).
			Distinct("request_model_name")
		if startUnix != nil {
			query = query.Where("time >= ?", *startUnix)
		}
		var names []string
		if err := query.Pluck("request_model_name", &names).Error; err != nil {
			return nil, err
		}
		for _, n := range names {
			if n != "" {
				seen[n] = struct{}{}
			}
		}
	}

	cache, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	for _, logItem := range cache {
		if startUnix != nil && logItem.Time < *startUnix {
			continue
		}
		if logItem.RequestModelName != "" {
			seen[logItem.RequestModelName] = struct{}{}
		}
	}
	lock.Unlock()

	models := make([]string, 0, len(seen))
	for name := range seen {
		models = append(models, name)
	}
	sort.Strings(models)
	return models, nil
}

// AnalyticsModelLatencyListGet returns per-model latency stats for all models in the time range.
func AnalyticsModelLatencyListGet(ctx context.Context, r model.AnalyticsRange) ([]model.ModelLatencyItem, error) {
	startUnix := analyticsRangeStartUnix(r, stats.Now())

	keepEnabled, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return nil, err
	}

	type rawRow struct {
		ModelName string `gorm:"column:model_name"`
		UseTime   int    `gorm:"column:use_time"`
	}

	var rows []rawRow

	if keepEnabled {
		query := db.GetDB().WithContext(ctx).
			Model(&model.RelayLog{}).
			Select("request_model_name AS model_name, use_time").
			Where("request_model_name <> ''")
		if startUnix != nil {
			query = query.Where("time >= ?", *startUnix)
		}
		if err := query.Scan(&rows).Error; err != nil {
			return nil, err
		}
	}

	cache, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	for _, entry := range cache {
		if startUnix != nil && entry.Time < *startUnix {
			continue
		}
		if entry.RequestModelName != "" {
			rows = append(rows, rawRow{ModelName: entry.RequestModelName, UseTime: entry.UseTime})
		}
	}
	lock.Unlock()

	byModel := make(map[string][]int64)
	for _, row := range rows {
		byModel[row.ModelName] = append(byModel[row.ModelName], int64(row.UseTime))
	}

	result := make([]model.ModelLatencyItem, 0, len(byModel))
	for modelName, times := range byModel {
		var total int64
		for _, t := range times {
			total += t
		}
		avgMs := int64(0)
		if len(times) > 0 {
			avgMs = total / int64(len(times))
		}
		fTimes := make([]float64, len(times))
		for i, t := range times {
			fTimes[i] = float64(t)
		}
		sort.Float64s(fTimes)
		item := model.ModelLatencyItem{
			ModelName:     modelName,
			TotalRequests: int64(len(times)),
			AvgMs:         avgMs,
			P50Ms:         int64(percentileFromSorted(fTimes, 0.50)),
			P95Ms:         int64(percentileFromSorted(fTimes, 0.95)),
			P99Ms:         int64(percentileFromSorted(fTimes, 0.99)),
		}
		result = append(result, item)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].AvgMs < result[j].AvgMs
	})

	return result, nil
}

func loadLatencyDistribution(ctx context.Context, r model.AnalyticsRange, modelFilter string) (*model.LatencyDistribution, error) {
	startUnix := analyticsRangeStartUnix(r, stats.Now())
	result := &model.LatencyDistribution{}

	keepEnabled, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return nil, err
	}

	var latencies []float64
	var ftuts []float64
	var totalUseTime int64
	var totalFtut int64
	var totalCount int64

	var hLt100, h100to500, h500to1k, h1kto5k, hGt5k int64

	if keepEnabled {
		type latencyRow struct {
			UseTime int `json:"use_time"`
			Ftut    int `json:"ftut"`
		}
		var dbRows []latencyRow
		query := db.GetDB().WithContext(ctx).
			Model(&model.RelayLog{}).
			Select("use_time, ftut")
		if startUnix != nil {
			query = query.Where("time >= ?", *startUnix)
		}
		if modelFilter != "" {
			query = query.Where("request_model_name = ?", modelFilter)
		}
		if err := query.Find(&dbRows).Error; err != nil {
			return nil, err
		}
		for _, row := range dbRows {
			if row.UseTime > 0 {
				latencies = append(latencies, float64(row.UseTime))
				totalUseTime += int64(row.UseTime)
				totalCount++
				switch {
				case row.UseTime < 100:
					hLt100++
				case row.UseTime < 500:
					h100to500++
				case row.UseTime < 1000:
					h500to1k++
				case row.UseTime < 5000:
					h1kto5k++
				default:
					hGt5k++
				}
			}
			if row.Ftut > 0 {
				ftuts = append(ftuts, float64(row.Ftut))
				totalFtut += int64(row.Ftut)
			}
		}
	}

	cache, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	for _, logItem := range cache {
		if startUnix != nil && logItem.Time < *startUnix {
			continue
		}
		if modelFilter != "" && logItem.RequestModelName != modelFilter {
			continue
		}
		if logItem.UseTime > 0 {
			latencies = append(latencies, float64(logItem.UseTime))
			totalUseTime += int64(logItem.UseTime)
			totalCount++
			switch {
			case logItem.UseTime < 100:
				hLt100++
			case logItem.UseTime < 500:
				h100to500++
			case logItem.UseTime < 1000:
				h500to1k++
			case logItem.UseTime < 5000:
				h1kto5k++
			default:
				hGt5k++
			}
		}
		if logItem.Ftut > 0 {
			ftuts = append(ftuts, float64(logItem.Ftut))
			totalFtut += int64(logItem.Ftut)
		}
	}
	lock.Unlock()

	result.TotalRequests = totalCount
	if totalCount > 0 {
		result.AvgMs = totalUseTime / totalCount
	}

	sort.Float64s(latencies)
	result.P50Ms = int64(percentileFromSorted(latencies, 0.50))
	result.P95Ms = int64(percentileFromSorted(latencies, 0.95))
	result.P99Ms = int64(percentileFromSorted(latencies, 0.99))

	sort.Float64s(ftuts)
	if len(ftuts) > 0 {
		result.FtutAvgMs = totalFtut / int64(len(ftuts))
		result.FtutP50Ms = int64(percentileFromSorted(ftuts, 0.50))
		result.FtutP95Ms = int64(percentileFromSorted(ftuts, 0.95))
		result.FtutP99Ms = int64(percentileFromSorted(ftuts, 0.99))
	}

	result.Buckets = []model.HistogramBucket{
		{Label: "<100ms", Count: hLt100},
		{Label: "100-500ms", Count: h100to500},
		{Label: "500ms-1s", Count: h500to1k},
		{Label: "1-5s", Count: h1kto5k},
		{Label: ">5s", Count: hGt5k},
	}

	return result, nil
}
