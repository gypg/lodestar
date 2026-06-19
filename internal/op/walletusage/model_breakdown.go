package walletusage

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/apikey"
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
	enabled, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil || !enabled {
		return nil, false, nil
	}
	conn := db.GetLogDB()
	if conn == nil {
		return nil, false, nil
	}
	keys, err := apikey.ListByUser(uid, ctx)
	if err != nil {
		return nil, false, err
	}
	if len(keys) == 0 {
		return []ModelUsageRow{}, true, nil
	}
	ids := make([]int, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.ID)
	}
	cutoff := time.Now().AddDate(0, 0, -days).Unix()
	logDBType := conf.AppConfig.Database.LogType
	if logDBType == "" {
		logDBType = conf.AppConfig.Database.Type
	}

	type aggRow struct {
		Model    string  `gorm:"column:model"`
		Requests int64   `gorm:"column:requests"`
		Tokens   int64   `gorm:"column:tokens"`
		Cost     float64 `gorm:"column:cost"`
	}
	var rows []aggRow
	q := conn.WithContext(ctx).Model(&model.RelayLog{}).
		Select(`COALESCE(NULLIF(TRIM(request_model_name), ''), actual_model_name, 'unknown') as model,
			COUNT(*) as requests,
			COALESCE(SUM(input_tokens + output_tokens), 0) as tokens,
			COALESCE(SUM(cost), 0) as cost`).
		Where("request_api_key_id IN ?", ids).
		Where("time >= ?", cutoff).
		Group("model").
		Order("requests DESC").
		Limit(limit)
	if err := q.Scan(&rows).Error; err != nil {
		return nil, false, err
	}
	out := make([]ModelUsageRow, 0, len(rows))
	for _, r := range rows {
		m := strings.TrimSpace(r.Model)
		if m == "" {
			m = "unknown"
		}
		out = append(out, ModelUsageRow{Model: m, Requests: r.Requests, Tokens: r.Tokens, Cost: r.Cost})
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
		Day      string `gorm:"column:day"`
		Total    int64  `gorm:"column:total"`
		Success  int64  `gorm:"column:success"`
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