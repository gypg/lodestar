package op

import (
	"context"
	"slices"
	"strings"
	"time"

	"sync"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/modelprobe"
)

type modelMarketStatsAggregate struct {
	waitTime       int64
	requestSuccess int64
	requestFailed  int64
}

// marketCache provides a short-lived TTL cache for the aggregated model market
// response. This avoids redundant recomputation when multiple clients request
// the data within a short window (e.g. concurrent polling).
var marketCache struct {
	mu        sync.RWMutex
	result    model.ModelMarketResponse
	expiresAt time.Time
}

const marketCacheTTL = 5 * time.Second

// ModelMarketInvalidateCache forces the next ModelMarketGet call to recompute.
func ModelMarketInvalidateCache() {
	marketCache.mu.Lock()
	marketCache.expiresAt = time.Time{}
	marketCache.mu.Unlock()
}

func ModelMarketGet(ctx context.Context, lastUpdateTime time.Time) (model.ModelMarketResponse, error) {
	// Fast path: return cached result if still valid.
	marketCache.mu.RLock()
	if time.Now().Before(marketCache.expiresAt) {
		cached := marketCache.result
		marketCache.mu.RUnlock()
		return cached, nil
	}
	marketCache.mu.RUnlock()

	models, err := LLMList(ctx)
	if err != nil {
		return model.ModelMarketResponse{}, err
	}

	modelChannels, err := ChannelLLMList(ctx)
	if err != nil {
		return model.ModelMarketResponse{}, err
	}

	items, summary := buildModelMarket(models, modelChannels, channelCache.GetAll(), StatsModelList(), lastUpdateTime)
	// WO-028：探测连续失败达阈值的模型盖上 ProbeFailedAt（最近失败时刻），
	// 广场默认视图据此隐藏、显示全部视图据此给标识。探测关闭/从未探测 → nil map，
	// 行为与探测引入前一致。
	hiddenProbes := modelprobe.HiddenSnapshot()
	for i := range items {
		if probedAt, bad := hiddenProbes[strings.ToLower(items[i].Name)]; bad {
			// 取地址前必须复制到局部变量：probedAt 是循环内的新变量所以本身安全，
			// 但显式写出来是为了防后来者改成 &hiddenProbes[...]（map 元素不可取址）。
			failedAt := probedAt
			items[i].ProbeFailedAt = &failedAt
		}
	}
	resp := model.ModelMarketResponse{
		Summary: summary,
		Items:   items,
	}

	// Store in cache.
	marketCache.mu.Lock()
	marketCache.result = resp
	marketCache.expiresAt = time.Now().Add(marketCacheTTL)
	marketCache.mu.Unlock()

	return resp, nil
}

func buildModelMarket(
	models []model.LLMInfo,
	modelChannels []model.LLMChannel,
	channelsByID map[int]model.Channel,
	stats []model.StatsModel,
	lastUpdateTime time.Time,
) ([]model.ModelMarketItem, model.ModelMarketSummary) {
	statsByModelName := make(map[string]modelMarketStatsAggregate)
	for _, item := range stats {
		name := strings.ToLower(strings.TrimSpace(item.Name))
		if name == "" {
			continue
		}
		aggregate := statsByModelName[name]
		aggregate.waitTime += item.WaitTime
		aggregate.requestSuccess += item.RequestSuccess
		aggregate.requestFailed += item.RequestFailed
		statsByModelName[name] = aggregate
	}

	channelsByModelName := make(map[string][]model.ModelMarketChannel)
	seenChannelsByModel := make(map[string]map[int]struct{})
	// The registry lowercases every model name on insert, so it cannot answer
	// "how did upstream spell this". Channels keep the raw spelling, so the
	// first channel to advertise a model decides its display casing.
	displayNameByModelName := make(map[string]string)
	for _, item := range modelChannels {
		raw := strings.TrimSpace(item.Name)
		name := strings.ToLower(raw)
		if name == "" {
			continue
		}
		if _, ok := displayNameByModelName[name]; !ok {
			displayNameByModelName[name] = raw
		}
		if seenChannelsByModel[name] == nil {
			seenChannelsByModel[name] = make(map[int]struct{})
		}
		if _, exists := seenChannelsByModel[name][item.ChannelID]; exists {
			continue
		}
		seenChannelsByModel[name][item.ChannelID] = struct{}{}

		channel := channelsByID[item.ChannelID]
		enabledKeyCount := 0
		for _, key := range channel.Keys {
			if key.Enabled {
				enabledKeyCount++
			}
		}

		channelsByModelName[name] = append(channelsByModelName[name], model.ModelMarketChannel{
			ChannelID:       item.ChannelID,
			ChannelName:     item.ChannelName,
			Enabled:         item.Enabled,
			EnabledKeyCount: enabledKeyCount,
		})
	}

	items := make([]model.ModelMarketItem, 0, len(models))
	totalWaitTime := int64(0)
	totalRequests := int64(0)
	uniqueChannels := make(map[int]struct{})
	coverageCount := 0

	for _, llm := range models {
		name := strings.ToLower(strings.TrimSpace(llm.Name))
		modelItemChannels := channelsByModelName[name]
		if modelItemChannels == nil {
			modelItemChannels = make([]model.ModelMarketChannel, 0)
		}
		slices.SortFunc(modelItemChannels, func(a, b model.ModelMarketChannel) int {
			return strings.Compare(a.ChannelName, b.ChannelName)
		})

		enabledKeyCount := 0
		for _, channel := range modelItemChannels {
			enabledKeyCount += channel.EnabledKeyCount
			uniqueChannels[channel.ChannelID] = struct{}{}
		}

		statsAggregate := statsByModelName[name]
		requestCount := statsAggregate.requestSuccess + statsAggregate.requestFailed
		averageLatency := int64(0)
		successRate := 0.0
		if requestCount > 0 {
			averageLatency = statsAggregate.waitTime / requestCount
			successRate = float64(statsAggregate.requestSuccess) / float64(requestCount)
		}

		totalWaitTime += statsAggregate.waitTime
		totalRequests += requestCount
		coverageCount += len(modelItemChannels)

		items = append(items, model.ModelMarketItem{
			Name:             llm.Name,
			DisplayName:      displayNameByModelName[name],
			Input:            llm.Input,
			Output:           llm.Output,
			CacheRead:        llm.CacheRead,
			CacheWrite:       llm.CacheWrite,
			ChannelCount:     len(modelItemChannels),
			EnabledKeyCount:  enabledKeyCount,
			AverageLatencyMS: averageLatency,
			SuccessRate:      successRate,
			RequestSuccess:   statsAggregate.requestSuccess,
			RequestFailed:    statsAggregate.requestFailed,
			Channels:         modelItemChannels,
		})
	}

	slices.SortFunc(items, func(a, b model.ModelMarketItem) int {
		leftRequests := a.RequestSuccess + a.RequestFailed
		rightRequests := b.RequestSuccess + b.RequestFailed

		switch {
		case leftRequests == 0 && rightRequests > 0:
			return 1
		case leftRequests > 0 && rightRequests == 0:
			return -1
		case leftRequests > 0 && rightRequests > 0:
			leftRatio := a.RequestSuccess * rightRequests
			rightRatio := b.RequestSuccess * leftRequests
			if leftRatio != rightRatio {
				if leftRatio > rightRatio {
					return -1
				}
				return 1
			}
		}

		if a.RequestSuccess != b.RequestSuccess {
			if a.RequestSuccess > b.RequestSuccess {
				return -1
			}
			return 1
		}
		if leftRequests != rightRequests {
			if leftRequests > rightRequests {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})

	summaryAverageLatency := int64(0)
	if totalRequests > 0 {
		summaryAverageLatency = totalWaitTime / totalRequests
	}

	return items, model.ModelMarketSummary{
		ModelCount:         len(items),
		CoverageCount:      coverageCount,
		UniqueChannelCount: len(uniqueChannels),
		AverageLatencyMS:   summaryAverageLatency,
		LastUpdateTime:     lastUpdateTime,
	}
}
