package walletusage

import (
	"context"
	"strings"
	"time"

	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/op/setting"
)

// DailyPoint is per-calendar-day usage for a user's API keys (from relay logs).
type DailyPoint struct {
	Date     string  `json:"date"` // YYYYMMDD
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	Cost     float64 `json:"cost"`
}

// DailySeriesForUser returns up to `days` daily buckets for all keys owned by uid.
// Requires relay_log_keep_enabled and persisted logs; otherwise ok=false, empty series.
func DailySeriesForUser(uid uint, days int, ctx context.Context) (series []DailyPoint, chartAvailable bool, err error) {
	if days < 1 {
		days = 14
	}
	if days > 90 {
		days = 90
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
		return fillEmptyDays(days, nil), true, nil
	}
	ids := make([]int, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.ID)
	}
	cutoff := time.Now().AddDate(0, 0, -days).Unix()

	type aggRow struct {
		Day      string  `gorm:"column:day"`
		Requests int64   `gorm:"column:requests"`
		Tokens   int64   `gorm:"column:tokens"`
		Cost     float64 `gorm:"column:cost"`
	}
	var rows []aggRow
	logDBType := conf.AppConfig.Database.LogType
	if logDBType == "" {
		logDBType = conf.AppConfig.Database.Type
	}
	dayExpr := dayBucketSQL(logDBType)
	q := conn.WithContext(ctx).Model(&model.RelayLog{}).
		Select(dayExpr+` as day,
			COUNT(*) as requests,
			COALESCE(SUM(input_tokens + output_tokens), 0) as tokens,
			COALESCE(SUM(cost), 0) as cost`).
		Where("request_api_key_id IN ?", ids).
		Where("time >= ?", cutoff).
		Group("day").
		Order("day ASC")
	if err := q.Scan(&rows).Error; err != nil {
		return nil, false, err
	}
	byDay := make(map[string]DailyPoint, len(rows))
	for _, r := range rows {
		if r.Day == "" {
			continue
		}
		byDay[r.Day] = DailyPoint{Date: r.Day, Requests: r.Requests, Tokens: r.Tokens, Cost: r.Cost}
	}
	return fillEmptyDays(days, byDay), true, nil
}

func fillEmptyDays(days int, byDay map[string]DailyPoint) []DailyPoint {
	out := make([]DailyPoint, 0, days)
	now := time.Now()
	for i := days - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i)
		key := d.Format("20060102")
		if byDay != nil {
			if p, ok := byDay[key]; ok {
				out = append(out, p)
				continue
			}
		}
		out = append(out, DailyPoint{Date: key})
	}
	return out
}

func dayBucketSQL(dbType string) string {
	switch strings.ToLower(dbType) {
	case "postgres", "postgresql":
		return `to_char(to_timestamp(time), 'YYYYMMDD')`
	default:
		return `strftime('%Y%m%d', time, 'unixepoch')`
	}
}