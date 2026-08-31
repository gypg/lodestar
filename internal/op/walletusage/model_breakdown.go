package walletusage

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
)

// ModelUsageRow is per-model usage for one user's API keys (from relay logs).
type ModelUsageRow struct {
	Model    string  `json:"model"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	Cost     float64 `json:"cost"`
}

// ModelBreakdownForUser aggregates relay logs by request_model_name for the user's keys.
//
// WO-023 缺陷 A：聚合现在走 loadUserLogsMerged（内存未刷盘日志 + DB 已刷盘日志，
// 按 id 去重），不再直查 DB。这样 total_cost（内存 stats）刚扣过费、但日志尚未
// 刷盘（最多滞后 200 条或 10 分钟定时任务）的那笔请求，能立刻出现在"分模型花费"
// 里——否则低流量站客户会看到"总额变了、分模型却没变"，误以为账目错了。
func ModelBreakdownForUser(uid uint, days int, limit int, ctx context.Context) ([]ModelUsageRow, bool, error) {
	if days < 1 {
		days = 30
	}
	if days > 90 {
		days = 90
	}
	if limit <= 0 {
		limit = 20
	}
	cutoff := time.Now().AddDate(0, 0, -days).Unix()
	logs, ok, err := loadUserLogsMerged(uid, cutoff, ctx)
	if err != nil || !ok {
		return nil, ok, err
	}
	if len(logs) == 0 {
		return []ModelUsageRow{}, true, nil
	}

	type agg struct {
		Requests int64
		Tokens   int64
		Cost     float64
	}
	byModel := make(map[string]*agg, 16)
	order := make([]string, 0, 16)
	for _, l := range logs {
		m := strings.TrimSpace(l.RequestModelName)
		if m == "" {
			m = strings.TrimSpace(l.ActualModelName)
		}
		if m == "" {
			m = "unknown"
		}
		a, exists := byModel[m]
		if !exists {
			a = &agg{}
			byModel[m] = a
			order = append(order, m)
		}
		a.Requests++
		a.Tokens += int64(l.InputTokens) + int64(l.OutputTokens)
		a.Cost += l.Cost
	}

	out := make([]ModelUsageRow, 0, len(byModel))
	for _, m := range order {
		a := byModel[m]
		out = append(out, ModelUsageRow{Model: m, Requests: a.Requests, Tokens: a.Tokens, Cost: a.Cost})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Requests > out[j].Requests })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, true, nil
}

// ChannelDayRate is one day's success rate for a channel (0–100).
type ChannelDayRate struct {
	Day         string  `json:"day"`
	SuccessRate float64 `json:"success_rate"`
	Requests    int64   `json:"requests"`
}

// ChannelSuccessSparkline returns up to `days` daily success rates per channel (from relay logs).
func ChannelSuccessSparkline(channelID int, days int, ctx context.Context) ([]ChannelDayRate, bool, error) {
	if channelID <= 0 {
		return nil, false, nil
	}
	if days < 1 {
		days = 7
	}
	if days > 14 {
		days = 14
	}
	enabled, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil || !enabled {
		return nil, false, nil
	}
	conn := db.GetLogDB()
	if conn == nil {
		return nil, false, nil
	}
	cutoff := time.Now().AddDate(0, 0, -days).Unix()
	logDBType := conf.AppConfig.Database.LogType
	if logDBType == "" {
		logDBType = conf.AppConfig.Database.Type
	}
	dayExpr := dayBucketSQL(logDBType)
	successExpr := successCountSQL(logDBType)

	type aggRow struct {
		Day     string `gorm:"column:day"`
		Total   int64  `gorm:"column:total"`
		Success int64  `gorm:"column:success"`
	}
	var rows []aggRow
	err = conn.WithContext(ctx).Model(&model.RelayLog{}).
		Select(dayExpr+` as day, COUNT(*) as total, `+successExpr+` as success`).
		Where("channel_id = ?", channelID).
		Where("time >= ?", cutoff).
		Group("day").
		Order("day ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, false, err
	}
	byDay := make(map[string]aggRow, len(rows))
	for _, r := range rows {
		if r.Day != "" {
			byDay[r.Day] = r
		}
	}
	now := time.Now()
	out := make([]ChannelDayRate, 0, days)
	for i := days - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i)
		key := d.Format("20060102")
		iso := key[0:4] + "-" + key[4:6] + "-" + key[6:8]
		row := byDay[key]
		rate := 0.0
		if row.Total > 0 {
			rate = float64(row.Success) / float64(row.Total) * 100
		}
		out = append(out, ChannelDayRate{Day: iso, SuccessRate: rate, Requests: row.Total})
	}
	return out, true, nil
}

func successCountSQL(dbType string) string {
	switch strings.ToLower(dbType) {
	case "postgres", "postgresql":
		return `SUM(CASE WHEN COALESCE(error, '') = '' THEN 1 ELSE 0 END)`
	default:
		return `SUM(CASE WHEN error IS NULL OR error = '' THEN 1 ELSE 0 END)`
	}
}

// SortModelsByRequests helper for tests.
func SortModelsByRequests(rows []ModelUsageRow) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].Requests > rows[j].Requests })
}
