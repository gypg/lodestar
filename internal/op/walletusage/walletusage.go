package walletusage

import (
	"context"
	"strings"
	"time"
)

// DailyPoint is per-calendar-day usage for a user's API keys (from relay logs).
type DailyPoint struct {
	Date     string  `json:"date"` // YYYYMMDD
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	Cost     float64 `json:"cost"`
}

// DailySeriesForUser returns up to `days` daily buckets for all keys owned by uid.
//
// WO-023 缺陷 A：改为聚合 loadUserLogsMerged 返回的"内存未刷盘 + DB 已刷盘（按 id
// 去重）"日志，而非直查 DB。这样一笔刚扣费但尚未刷盘（最多滞后 200 条或 10 分钟
// 定时任务）的请求，会立刻进入"分日花费"，与 total_cost 同步——否则低流量站客户会
// 看到"总额变了、分日却没变"，误以为账目错了。chartAvailable 语义不变：未开启
// relay_log_keep_enabled 或日志库不可用时返回 ok=false。
func DailySeriesForUser(uid uint, days int, ctx context.Context) (series []DailyPoint, chartAvailable bool, err error) {
	if days < 1 {
		days = 14
	}
	if days > 90 {
		days = 90
	}
	cutoff := time.Now().AddDate(0, 0, -days).Unix()
	logs, ok, err := loadUserLogsMerged(uid, cutoff, ctx)
	if err != nil || !ok {
		return nil, ok, err
	}
	byDay := make(map[string]DailyPoint, days)
	for _, l := range logs {
		key := time.Unix(l.Time, 0).Format("20060102")
		p := byDay[key]
		p.Date = key
		p.Requests++
		p.Tokens += int64(l.InputTokens) + int64(l.OutputTokens)
		p.Cost += l.Cost
		byDay[key] = p
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

// HeatmapPoint is one calendar day for GitHub-style heatmap (YYYY-MM-DD).
type HeatmapPoint struct {
	Day      string `json:"day"`
	Requests int64  `json:"requests"`
	Tokens   int64  `json:"tokens"`
}

// HeatmapForUser returns up to `days` daily points (max 90) for heatmap UI.
func HeatmapForUser(uid uint, days int, ctx context.Context) ([]HeatmapPoint, bool, error) {
	series, ok, err := DailySeriesForUser(uid, days, ctx)
	if err != nil || !ok {
		return nil, ok, err
	}
	out := make([]HeatmapPoint, 0, len(series))
	for _, p := range series {
		if len(p.Date) != 8 {
			continue
		}
		out = append(out, HeatmapPoint{
			Day:      p.Date[0:4] + "-" + p.Date[4:6] + "-" + p.Date[6:8],
			Requests: p.Requests,
			Tokens:   p.Tokens,
		})
	}
	return out, true, nil
}

func dayBucketSQL(dbType string) string {
	switch strings.ToLower(dbType) {
	case "postgres", "postgresql":
		return `to_char(to_timestamp(time), 'YYYYMMDD')`
	default:
		return `strftime('%Y%m%d', time, 'unixepoch')`
	}
}
