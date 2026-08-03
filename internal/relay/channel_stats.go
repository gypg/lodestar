package relay

import (
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/stats"
)

func updateChannelSuccessStats(channelID int, waitTimeMs int64, metrics dbmodel.StatsMetrics) {
	stats.ChannelUpdate(channelID, dbmodel.StatsMetrics{
		WaitTime:       waitTimeMs,
		InputToken:     metrics.InputToken,
		OutputToken:    metrics.OutputToken,
		InputCost:      metrics.InputCost,
		OutputCost:     metrics.OutputCost,
		RequestSuccess: 1,
	})
}
