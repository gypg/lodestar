package analytics

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gypg/lodestar/internal/model"
)

// ---------- Build functions: transform raw DB/cache rows into API response models ----------

func buildAnalyticsOverview(metrics model.StatsMetrics, providerCount, apiKeyCount, modelCount int, fallbackRate float64) model.AnalyticsOverview {
	requestCount := metrics.RequestSuccess + metrics.RequestFailed
	successRate := 0.0
	if requestCount > 0 {
		successRate = (float64(metrics.RequestSuccess) / float64(requestCount)) * 100
	}

	return model.AnalyticsOverview{
		AnalyticsMetrics: model.AnalyticsMetrics{
			RequestCount: requestCount,
			TotalTokens:  metrics.InputToken + metrics.OutputToken,
			InputTokens:  metrics.InputToken,
			OutputTokens: metrics.OutputToken,
			TotalCost:    metrics.InputCost + metrics.OutputCost,
			SuccessRate:  successRate,
		},
		ProviderCount: providerCount,
		APIKeyCount:   apiKeyCount,
		ModelCount:    modelCount,
		FallbackRate:  fallbackRate,
	}
}

func buildProviderBreakdown(rows map[int]*analyticsProviderAggregateRow, channelByID map[int]model.Channel) []model.AnalyticsProviderBreakdownItem {
	items := make([]model.AnalyticsProviderBreakdownItem, 0, len(rows))
	for channelID, row := range rows {
		if row == nil {
			continue
		}

		requestCount := row.RequestSuccess + row.RequestFailed
		successRate := 0.0
		if requestCount > 0 {
			successRate = (float64(row.RequestSuccess) / float64(requestCount)) * 100
		}

		channelName := strings.TrimSpace(row.ChannelName)
		enabled := false
		if c, ok := channelByID[channelID]; ok {
			if channelName == "" {
				channelName = c.Name
			}
			enabled = c.Enabled
		}
		if channelName == "" {
			channelName = "Unknown Channel"
		}

		items = append(items, model.AnalyticsProviderBreakdownItem{
			ChannelID:   channelID,
			ChannelName: channelName,
			Enabled:     enabled,
			AnalyticsMetrics: model.AnalyticsMetrics{
				RequestCount: requestCount,
				TotalTokens:  row.InputTokens + row.OutputTokens,
				InputTokens:  row.InputTokens,
				OutputTokens: row.OutputTokens,
				TotalCost:    row.TotalCost,
				SuccessRate:  successRate,
			},
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].RequestCount != items[j].RequestCount {
			return items[i].RequestCount > items[j].RequestCount
		}
		if items[i].TotalCost != items[j].TotalCost {
			return items[i].TotalCost > items[j].TotalCost
		}
		return items[i].ChannelName < items[j].ChannelName
	})

	return items
}

func buildModelBreakdown(rows map[string]*analyticsModelAggregateRow) []model.AnalyticsModelBreakdownItem {
	items := make([]model.AnalyticsModelBreakdownItem, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		modelName := strings.TrimSpace(row.ModelName)
		if modelName == "" {
			continue
		}

		requestCount := row.RequestSuccess + row.RequestFailed
		successRate := 0.0
		if requestCount > 0 {
			successRate = (float64(row.RequestSuccess) / float64(requestCount)) * 100
		}

		items = append(items, model.AnalyticsModelBreakdownItem{
			ModelName: modelName,
			AnalyticsMetrics: model.AnalyticsMetrics{
				RequestCount: requestCount,
				TotalTokens:  row.InputTokens + row.OutputTokens,
				InputTokens:  row.InputTokens,
				OutputTokens: row.OutputTokens,
				TotalCost:    row.TotalCost,
				SuccessRate:  successRate,
			},
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].RequestCount != items[j].RequestCount {
			return items[i].RequestCount > items[j].RequestCount
		}
		if items[i].TotalCost != items[j].TotalCost {
			return items[i].TotalCost > items[j].TotalCost
		}
		return items[i].ModelName < items[j].ModelName
	})

	return items
}

func buildAPIKeyBreakdown(rows map[string]*analyticsAPIKeyAggregateRow) []model.AnalyticsAPIKeyBreakdownItem {
	items := make([]model.AnalyticsAPIKeyBreakdownItem, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		name := strings.TrimSpace(row.Name)
		if name == "" {
			if row.APIKeyID > 0 {
				name = "Key #" + strconv.Itoa(row.APIKeyID)
			} else {
				name = "Unknown Key"
			}
		}

		requestCount := row.RequestSuccess + row.RequestFailed
		successRate := 0.0
		if requestCount > 0 {
			successRate = (float64(row.RequestSuccess) / float64(requestCount)) * 100
		}

		item := model.AnalyticsAPIKeyBreakdownItem{
			Name: name,
			AnalyticsMetrics: model.AnalyticsMetrics{
				RequestCount: requestCount,
				TotalTokens:  row.InputTokens + row.OutputTokens,
				InputTokens:  row.InputTokens,
				OutputTokens: row.OutputTokens,
				TotalCost:    row.TotalCost,
				SuccessRate:  successRate,
			},
		}
		if row.APIKeyID > 0 {
			id := row.APIKeyID
			item.APIKeyID = &id
		}
		items = append(items, item)
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].RequestCount != items[j].RequestCount {
			return items[i].RequestCount > items[j].RequestCount
		}
		if items[i].TotalCost != items[j].TotalCost {
			return items[i].TotalCost > items[j].TotalCost
		}
		return items[i].Name < items[j].Name
	})

	return items
}

func buildChannelModelBreakdown(rows map[string]*analyticsChannelModelAggregateRow, channelByID map[int]model.Channel) []model.AnalyticsChannelModelItem {
	items := make([]model.AnalyticsChannelModelItem, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		channelName := strings.TrimSpace(row.ChannelName)
		enabled := false
		if c, ok := channelByID[row.ChannelID]; ok {
			if channelName == "" {
				channelName = c.Name
			}
			enabled = c.Enabled
		}
		if channelName == "" {
			channelName = "Unknown Channel"
		}

		requestCount := row.RequestSuccess + row.RequestFailed
		successRate := 0.0
		if requestCount > 0 {
			successRate = (float64(row.RequestSuccess) / float64(requestCount)) * 100
		}

		items = append(items, model.AnalyticsChannelModelItem{
			ChannelID:   row.ChannelID,
			ChannelName: channelName,
			ModelName:   row.ModelName,
			Enabled:     enabled,
			AnalyticsMetrics: model.AnalyticsMetrics{
				RequestCount: requestCount,
				TotalTokens:  row.InputTokens + row.OutputTokens,
				InputTokens:  row.InputTokens,
				OutputTokens: row.OutputTokens,
				TotalCost:    row.TotalCost,
				SuccessRate:  successRate,
			},
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		ifailed := int64(float64(items[i].RequestCount) * (1 - items[i].SuccessRate/100))
		jfailed := int64(float64(items[j].RequestCount) * (1 - items[j].SuccessRate/100))
		if ifailed != jfailed {
			return ifailed > jfailed
		}
		if items[i].RequestCount != items[j].RequestCount {
			return items[i].RequestCount > items[j].RequestCount
		}
		if items[i].ChannelName != items[j].ChannelName {
			return items[i].ChannelName < items[j].ChannelName
		}
		return items[i].ModelName < items[j].ModelName
	})

	return items
}

func buildGroupHealth(groups []model.Group, channelByID map[int]model.Channel, failures map[string]*analyticsFailureAggregateRow) []model.AnalyticsGroupHealthItem {
	items := make([]model.AnalyticsGroupHealthItem, 0, len(groups))
	for _, group := range groups {
		itemCount := len(group.Items)
		enabledItemCount := 0
		disabledItemCount := 0
		failureCount := int64(0)
		lastFailureAt := int64(0)

		type chanFail struct {
			ChannelID     int
			ChannelName   string
			ModelName     string
			FailureCount  int64
			LastFailureAt int64
		}
		chanFailures := make(map[string]*chanFail)
		seenFailureKeys := make(map[string]struct{})
		for _, item := range group.Items {
			c, ok := channelByID[item.ChannelID]
			if ok && c.Enabled {
				enabledItemCount++
			} else {
				disabledItemCount++
			}

			for _, key := range []string{
				makeAnalyticsFailureKey(item.ChannelID, item.ModelName, item.ModelName),
				makeAnalyticsFailureKey(item.ChannelID, item.ModelName, group.Name),
			} {
				if _, ok := seenFailureKeys[key]; ok {
					continue
				}
				seenFailureKeys[key] = struct{}{}
				failure, ok := failures[key]
				if !ok || failure == nil {
					continue
				}
				failureCount += failure.FailureCount
				if failure.LastFailureAt > lastFailureAt {
					lastFailureAt = failure.LastFailureAt
				}

				cfKey := strconv.Itoa(item.ChannelID) + "\x00" + item.ModelName
				cf, ok := chanFailures[cfKey]
				if !ok {
					cf = &chanFail{
						ChannelID: item.ChannelID,
						ModelName: item.ModelName,
					}
					if c, ok := channelByID[item.ChannelID]; ok {
						cf.ChannelName = c.Name
					}
					chanFailures[cfKey] = cf
				}
				cf.FailureCount += failure.FailureCount
				if failure.LastFailureAt > cf.LastFailureAt {
					cf.LastFailureAt = failure.LastFailureAt
				}
			}
		}

		status := "healthy"
		score := 100
		switch {
		case itemCount == 0:
			status = "empty"
			score = 0
		case enabledItemCount == 0:
			status = "down"
			score = 20
		default:
			score -= (disabledItemCount * 40) / itemCount
			if failureCount > 0 {
				penalty := int(failureCount * 12)
				if penalty > 48 {
					penalty = 48
				}
				score -= penalty
			}
			if disabledItemCount > 0 || failureCount >= 3 {
				status = "degraded"
			} else if failureCount > 0 {
				status = "warning"
			}
		}

		if score < 0 {
			score = 0
		}

		failingChannels := make([]model.FailingChannelItem, 0, len(chanFailures))
		for _, cf := range chanFailures {
			if cf.FailureCount <= 0 {
				continue
			}
			failingChannels = append(failingChannels, model.FailingChannelItem{
				ChannelID:     cf.ChannelID,
				ChannelName:   cf.ChannelName,
				ModelName:     cf.ModelName,
				FailureCount:  cf.FailureCount,
				LastFailureAt: cf.LastFailureAt,
			})
		}
		sort.SliceStable(failingChannels, func(i, j int) bool {
			if failingChannels[i].FailureCount != failingChannels[j].FailureCount {
				return failingChannels[i].FailureCount > failingChannels[j].FailureCount
			}
			return failingChannels[i].ChannelName < failingChannels[j].ChannelName
		})
		if len(failingChannels) > 10 {
			failingChannels = failingChannels[:10]
		}

		channelIDs := make([]int, 0, len(group.Items))
		seenChannels := make(map[int]struct{})
		for _, item := range group.Items {
			if _, ok := seenChannels[item.ChannelID]; ok {
				continue
			}
			seenChannels[item.ChannelID] = struct{}{}
			channelIDs = append(channelIDs, item.ChannelID)
		}

		items = append(items, model.AnalyticsGroupHealthItem{
			GroupID:           group.ID,
			GroupName:         group.Name,
			EndpointType:      group.EndpointType,
			ItemCount:         itemCount,
			EnabledItemCount:  enabledItemCount,
			DisabledItemCount: disabledItemCount,
			FailureCount:      failureCount,
			LastFailureAt:     lastFailureAt,
			HealthScore:       score,
			Status:            status,
			Mode:              int(group.Mode),
			FailingChannels:   failingChannels,
			ChannelIDs:        channelIDs,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].HealthScore != items[j].HealthScore {
			return items[i].HealthScore < items[j].HealthScore
		}
		if items[i].FailureCount != items[j].FailureCount {
			return items[i].FailureCount > items[j].FailureCount
		}
		return items[i].GroupName < items[j].GroupName
	})

	return items
}

// ---------- Time range helpers ----------

func analyticsRangeStartUnix(r model.AnalyticsRange, now time.Time) *int64 {
	startDate := analyticsStartTime(r, now)
	if startDate == nil {
		return nil
	}
	unix := startDate.Unix()
	return &unix
}

func analyticsStartDate(r model.AnalyticsRange, now time.Time) string {
	start := analyticsStartTime(r, now)
	if start == nil {
		return ""
	}
	return start.Format("20060102")
}

func analyticsStartTime(r model.AnalyticsRange, now time.Time) *time.Time {
	location := now.Location()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)

	switch r {
	case model.AnalyticsRange1D:
		return &dayStart
	case model.AnalyticsRange7D:
		start := dayStart.AddDate(0, 0, -6)
		return &start
	case model.AnalyticsRange30D:
		start := dayStart.AddDate(0, 0, -29)
		return &start
	case model.AnalyticsRange90D:
		start := dayStart.AddDate(0, 0, -89)
		return &start
	case model.AnalyticsRangeYTD:
		start := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, location)
		return &start
	case model.AnalyticsRangeAll:
		return nil
	default:
		start := dayStart.AddDate(0, 0, -6)
		return &start
	}
}

// ---------- Latency helpers ----------

func percentileFromSorted(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func splitAnalyticsChannelModels(channel model.Channel) []string {
	parts := strings.Split(channel.Model, ",")
	if strings.TrimSpace(channel.CustomModel) != "" {
		parts = append(parts, strings.Split(channel.CustomModel, ",")...)
	}

	seen := make(map[string]struct{}, len(parts))
	models := make([]string, 0, len(parts))
	for _, part := range parts {
		modelName := strings.TrimSpace(part)
		if modelName == "" {
			continue
		}
		if _, ok := seen[modelName]; ok {
			continue
		}
		seen[modelName] = struct{}{}
		models = append(models, modelName)
	}
	return models
}
