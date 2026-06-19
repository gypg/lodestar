package walletusage

import (
	"testing"
)

func TestHeatmapDayFormatFromSeries(t *testing.T) {
	series := []DailyPoint{
		{Date: "20260617", Requests: 3, Tokens: 100},
		{Date: "20260618", Requests: 0, Tokens: 0},
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
	if len(out) != 2 {
		t.Fatalf("len=%d", len(out))
	}
	if out[0].Day != "2026-06-17" || out[0].Requests != 3 {
		t.Fatalf("got %+v", out[0])
	}
}