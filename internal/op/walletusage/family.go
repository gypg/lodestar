package walletusage

/*
WO-042：/api/v1/stats/* 租户收窄的重建层。

/home 的 today/hourly/daily 图表消费 model.StatsMetrics 的原生 JSON 字段
（request_success / input_cost / wait_time / ftut_* / histogram_* / …）。
本文件把用户的 relay_logs（内存 + DB 合并，见 loadLogsMergedByIDs）重建成
完全同形的 StatsMetrics，分桶维度分别为"今天"（FamilyForAPIKeys today）、
当天 24 小时（HourlySeriesForAPIKeys）、近 N 天（DailySeriesForAPIKeys）。

成功判据照抄仓库既有写法（relaylog.go:756/758、model_breakdown.go:163）：
error 为 NULL **或** 空串都算成功 —— 只判其一会把一半请求归错类。
保留期关闭时返回 ok=false（前端显示不可用），绝不静默全零。
延迟/FTUT 分位数与直方图按 UseTime/Ftut 落五档；n=0 时全零（照
analytics_build.go 的 requestCount>0 守卫形状，分母不除零）。
*/

import (
	"context"
	"time"

	"github.com/gypg/lodestar/internal/model"
)

const defaultFamilyDays = 7

// familyPercentile: nearest-rank style pick from a sorted slice; zero when empty.
func familyPercentile(sorted []int64, rank int64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	if rank < 1 {
		rank = 1
	}
	idx := int(rank) - 1
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// familySortAsc returns an ascending copy of the sample slice.
func familySortAsc(in []int64) []int64 {
	out := make([]int64, len(in))
	copy(out, in)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// familyMetricsFromLogs folds a log slice into one StatsMetrics.
func familyMetricsFromLogs(logs []model.RelayLog) model.StatsMetrics {
	var m model.StatsMetrics
	latencies := make([]int64, 0, len(logs))
	ftuts := make([]int64, 0, len(logs))
	for _, l := range logs {
		m.InputToken += int64(l.InputTokens)
		m.OutputToken += int64(l.OutputTokens)
		m.InputCost += l.Cost
		m.WaitTime += int64(l.UseTime)
		if l.Error == "" || l.Error == "\x00" {
			m.RequestSuccess++
		} else {
			m.RequestFailed++
		}
		latencies = append(latencies, int64(l.UseTime))
		if l.Ftut > 0 {
			ftuts = append(ftuts, int64(l.Ftut))
		}
		lat := int64(l.UseTime)
		switch {
		case lat < 100:
			m.HistogramLt100++
		case lat < 500:
			m.Histogram100to500++
		case lat < 1000:
			m.Histogram500to1k++
		case lat < 5000:
			m.Histogram1kto5k++
		default:
			m.HistogramGt5k++
		}
	}
	sLat := familySortAsc(latencies)
	m.LatencyP50 = familyPercentile(sLat, int64((len(sLat)+1)/2))
	m.LatencyP95 = familyPercentile(sLat, int64((len(sLat)*95+99)/100))
	m.LatencyP99 = familyPercentile(sLat, int64((len(sLat)*99+99)/100))
	sFt := familySortAsc(ftuts)
	m.FtutAvg = familyPercentile(sFt, 1)
	m.FtutP50 = familyPercentile(sFt, int64((len(sFt)+1)/2))
	m.FtutP95 = familyPercentile(sFt, int64((len(sFt)*95+99)/100))
	m.FtutP99 = familyPercentile(sFt, int64((len(sFt)*99+99)/100))
	return m
}

// FamilyForAPIKeys folds the user's logs for one bucket scope.
// scope "today": only logs from today (local time). Any other scope value is
// treated as the whole retained window (used by /stats/total's legacy shape —
// callers who want the lifetime figure should use stats.APIKeysAggregate).
func FamilyForAPIKeys(apiKeyIDs []int, scope string) (model.StatsMetrics, bool, error) {
	cutoff := time.Now().Truncate(24 * time.Hour).Unix()
	if scope != "today" {
		// Non-today scopes have no narrower semantics here; callers use the
		// hourly/daily series or the aggregate. Keep the window broad.
		cutoff = 0
	}
	logs, ok, err := loadLogsMergedByIDs(apiKeyIDs, cutoff, context.Background())
	if err != nil || !ok {
		return model.StatsMetrics{}, ok, err
	}
	if scope == "today" {
		todayKey := time.Now().Format("20060102")
		filtered := make([]model.RelayLog, 0, len(logs))
		for _, l := range logs {
			if time.Unix(l.Time, 0).Format("20060102") == todayKey {
				filtered = append(filtered, l)
			}
		}
		logs = filtered
	}
	return familyMetricsFromLogs(logs), true, nil
}

// HourlySeriesForAPIKeys rebuilds today's 24 hourly buckets from the user's logs.
func HourlySeriesForAPIKeys(apiKeyIDs []int) ([]model.StatsHourly, bool, error) {
	cutoff := time.Now().Truncate(24 * time.Hour).Unix()
	logs, ok, err := loadLogsMergedByIDs(apiKeyIDs, cutoff, context.Background())
	if err != nil || !ok {
		return nil, ok, err
	}
	now := time.Now()
	todayKey := now.Format("20060102")
	byHour := make(map[int]model.StatsMetrics, 24)
	for _, l := range logs {
		at := time.Unix(l.Time, 0)
		if at.Format("20060102") != todayKey {
			continue
		}
		m := byHour[at.Hour()]
		folded := familyMetricsFromLogs([]model.RelayLog{l})
		m.RequestSuccess += folded.RequestSuccess
		m.RequestFailed += folded.RequestFailed
		m.InputToken += folded.InputToken
		m.OutputToken += folded.OutputToken
		m.InputCost += folded.InputCost
		m.OutputCost += folded.OutputCost
		m.WaitTime += folded.WaitTime
		byHour[at.Hour()] = m
	}
	// 保持与 st.HourlyGet() 相同的形状：24 桶（未来小时为当天零值）。
	out := make([]model.StatsHourly, 0, 24)
	for hour := 0; hour < 24; hour++ {
		out = append(out, model.StatsHourly{Hour: hour, Date: todayKey, StatsMetrics: byHour[hour]})
	}
	return out, true, nil
}

// DailySeriesForAPIKeys rebuilds up to `days` daily buckets (max 90, default 7).
// Days with no logs are zero-filled; days beyond the retention window are
// genuinely empty — that is the data-retention truth, not a bug to paper over.
func DailySeriesForAPIKeys(apiKeyIDs []int, days int) ([]model.StatsDaily, bool, error) {
	if days < 1 {
		days = defaultFamilyDays
	}
	if days > 90 {
		days = 90
	}
	cutoff := time.Now().AddDate(0, 0, -days).Unix()
	logs, ok, err := loadLogsMergedByIDs(apiKeyIDs, cutoff, context.Background())
	if err != nil || !ok {
		return nil, ok, err
	}
	byDay := make(map[string]model.StatsMetrics, days)
	for _, l := range logs {
		key := time.Unix(l.Time, 0).Format("20060102")
		folded := familyMetricsFromLogs([]model.RelayLog{l})
		m := byDay[key]
		m.RequestSuccess += folded.RequestSuccess
		m.RequestFailed += folded.RequestFailed
		m.InputToken += folded.InputToken
		m.OutputToken += folded.OutputToken
		m.InputCost += folded.InputCost
		m.OutputCost += folded.OutputCost
		m.WaitTime += folded.WaitTime
		byDay[key] = m
	}
	now := time.Now()
	out := make([]model.StatsDaily, 0, days)
	for i := days - 1; i >= 0; i-- {
		key := now.AddDate(0, 0, -i).Format("20060102")
		out = append(out, model.StatsDaily{Date: key, StatsMetrics: byDay[key]})
	}
	return out, true, nil
}
