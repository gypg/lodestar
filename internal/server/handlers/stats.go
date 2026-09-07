package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/model"
	ak "github.com/gypg/lodestar/internal/op/apikey"
	ch "github.com/gypg/lodestar/internal/op/channel"
	st "github.com/gypg/lodestar/internal/op/stats"
	"github.com/gypg/lodestar/internal/op/walletusage"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
)

type apiKeyStatsResponse struct {
	model.StatsAPIKey
	Name string `json:"name"`
}

type channelStatsResponse struct {
	model.StatsChannel
	ChannelName string `json:"channel_name"`
	Enabled     bool   `json:"enabled"`
}

func init() {
	router.NewGroupRouter("/api/v1/stats").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermStatsRead)).
		AddRoute(
			router.NewRoute("/today", http.MethodGet).
				Handle(getStatsToday),
		).
		AddRoute(
			router.NewRoute("/daily", http.MethodGet).
				Handle(getStatsDaily),
		).
		AddRoute(
			router.NewRoute("/hourly", http.MethodGet).
				Handle(getStatsHourly),
		).
		AddRoute(
			router.NewRoute("/total", http.MethodGet).
				Handle(getStatsTotal),
		).
		AddRoute(
			router.NewRoute("/channel", http.MethodGet).
				Handle(getStatsChannel),
		).
		AddRoute(
			router.NewRoute("/apikey", http.MethodGet).
				Handle(getStatsAPIKey),
		)
}

func getStatsToday(c *gin.Context) {
	if !canSeeSiteWideAnalytics(c) {
		// WO-042：按调用者自己的 key 从 relay_logs 重建"今天"桶（同形
		// StatsMetrics），不再透传全站缓存。
		m, ok, err := walletusage.FamilyForAPIKeys(getStatsCallerKeyIDs(c), "today")
		if err != nil {
			resp.InternalError(c)
			return
		}
		if !ok {
			// relay_log_keep_enabled 关闭：如实报不可用，不静默全零。
			resp.Error(c, http.StatusServiceUnavailable, "usage history is unavailable (relay log keeping is disabled)")
			return
		}
		resp.Success(c, m)
		return
	}
	resp.Success(c, st.TodayGet())
}

func getStatsDaily(c *gin.Context) {
	if !canSeeSiteWideAnalytics(c) {
		days := parseStatsDays(c, 14)
		series, ok, err := walletusage.DailySeriesForAPIKeys(getStatsCallerKeyIDs(c), days)
		if err != nil {
			resp.InternalError(c)
			return
		}
		if !ok {
			resp.Error(c, http.StatusServiceUnavailable, "usage history is unavailable (relay log keeping is disabled)")
			return
		}
		resp.Success(c, series)
		return
	}
	statsDaily, err := st.GetDaily(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, statsDaily)
}

func getStatsHourly(c *gin.Context) {
	if !canSeeSiteWideAnalytics(c) {
		buckets, ok, err := walletusage.HourlySeriesForAPIKeys(getStatsCallerKeyIDs(c))
		if err != nil {
			resp.InternalError(c)
			return
		}
		if !ok {
			resp.Error(c, http.StatusServiceUnavailable, "usage history is unavailable (relay log keeping is disabled)")
			return
		}
		resp.Success(c, buckets)
		return
	}
	resp.Success(c, st.HourlyGet())
}

func getStatsTotal(c *gin.Context) {
	if !canSeeSiteWideAnalytics(c) {
		// 保留期只有 7 天，"累计"查不出 → 用 per-key 累计（getUsage 同款）。
		m := st.APIKeysAggregate(getStatsCallerKeyIDs(c))
		resp.Success(c, m)
		return
	}
	resp.Success(c, st.TotalGet())
}

// getStatsCallerKeyIDs returns the API key IDs owned by the caller.
// A non-staff caller with zero keys gets an empty slice, which the rebuild
// helpers turn into legitimately empty data.
func getStatsCallerKeyIDs(c *gin.Context) []int {
	keys, err := ak.ListByUser(uint(c.GetInt("user_id")), c.Request.Context())
	if err != nil || len(keys) == 0 {
		return nil
	}
	ids := make([]int, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.ID)
	}
	return ids
}

// parseStatsDays reads ?days (default 14, the walletusage default, capped 90).
func parseStatsDays(c *gin.Context, def int) int {
	if v := c.Query("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 90 {
			return n
		}
	}
	return def
}

func getStatsChannel(c *gin.Context) {
	// WO-042：渠道名属于 upstream config，对端客户无论怎样收窄都会泄露
	// （拍板走 B：关门优先体验）。非 staff 返空集；前端排行榜自然空掉。
	if !canSeeSiteWideAnalytics(c) {
		resp.Success(c, nil)
		return
	}
	stats := st.ChannelList()
	statsByChannelID := make(map[int]model.StatsChannel, len(stats))
	for _, item := range stats {
		statsByChannelID[item.ChannelID] = item
	}

	channels, err := ch.List(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}

	result := make([]channelStatsResponse, 0, len(channels))
	for _, channel := range channels {
		channelStats, ok := statsByChannelID[channel.ID]
		if !ok {
			channelStats = model.StatsChannel{ChannelID: channel.ID}
		} else {
			delete(statsByChannelID, channel.ID)
		}

		result = append(result, channelStatsResponse{
			StatsChannel: channelStats,
			ChannelName:  channel.Name,
			Enabled:      channel.Enabled,
		})
	}

	for channelID, item := range statsByChannelID {
		result = append(result, channelStatsResponse{
			StatsChannel: item,
			ChannelName:  fmt.Sprintf("Channel #%d", channelID),
			Enabled:      false,
		})
	}

	resp.Success(c, result)
}

func getStatsAPIKey(c *gin.Context) {
	stats := st.APIKeyList()

	// Multi-tenant isolation: non-staff users only see their own API keys.
	var apiKeys []model.APIKey
	var err error
	if isStaff(c) {
		apiKeys, err = ak.List(c.Request.Context())
	} else {
		apiKeys, err = ak.ListByUser(uint(c.GetInt("user_id")), c.Request.Context())
	}
	if err != nil {
		resp.InternalError(c)
		return
	}

	apiKeyNames := make(map[int]string, len(apiKeys))
	for _, apiKey := range apiKeys {
		apiKeyNames[apiKey.ID] = apiKey.Name
	}

	statsByAPIKeyID := make(map[int]model.StatsAPIKey, len(stats))
	for _, item := range stats {
		statsByAPIKeyID[item.APIKeyID] = item
	}

	result := make([]apiKeyStatsResponse, 0, len(apiKeys)+len(stats))
	for _, apiKey := range apiKeys {
		item, ok := statsByAPIKeyID[apiKey.ID]
		if !ok {
			item = model.StatsAPIKey{APIKeyID: apiKey.ID}
		} else {
			delete(statsByAPIKeyID, apiKey.ID)
		}
		result = append(result, apiKeyStatsResponse{
			StatsAPIKey: item,
			Name:        apiKey.Name,
		})
	}

	// For non-staff, skip orphan stats entries not belonging to the user's keys.
	if isStaff(c) {
		for apiKeyID, item := range statsByAPIKeyID {
			name, ok := apiKeyNames[item.APIKeyID]
			if !ok {
				name = fmt.Sprintf("Key #%d", apiKeyID)
			}
			result = append(result, apiKeyStatsResponse{
				StatsAPIKey: item,
				Name:        name,
			})
		}
	}

	resp.Success(c, result)
}
