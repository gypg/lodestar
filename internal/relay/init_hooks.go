package relay

import (
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/relay/balancer"
)

func init() {
	// 注册渠道删除时的清理钩子：清除熔断器和 Auto 策略统计中的残留条目，
	// 防止 globalBreaker / globalAutoStats 无限增长。
	op.OnChannelDeletedHooks = append(op.OnChannelDeletedHooks, func(channelID int) {
		balancer.RemoveChannelEntries(channelID)
		balancer.RemoveChannelStats(channelID)
	})

	// 注册 API Key 删除时的清理钩子：清除粘性会话条目，
	// 防止 globalSession 无限增长。
	apikey.DeleteSessionFunc = func(id int) {
		balancer.RemoveAPIKeySticky(id)
	}
}
