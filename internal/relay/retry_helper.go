package relay

import (
	"context"
	"fmt"

	dbmodel "github.com/gypg/lodestar/internal/model"
	ch "github.com/gypg/lodestar/internal/op/channel"
	"github.com/gypg/lodestar/internal/relay/balancer"
	"github.com/gypg/lodestar/internal/utils/log"
)

// PrepareCandidateResult 准备候选的结果
type PrepareCandidateResult struct {
	Channel       *dbmodel.Channel
	UsedKey       dbmodel.ChannelKey
	SkipReason    string
	SkipStatus    dbmodel.AttemptStatus
	ResolvedModel string
}

func PrepareCandidate(
	ctx context.Context,
	item dbmodel.GroupItem,
	iter *balancer.Iterator,
	ratelimitCooldown int,
	requestModel string,
	zenPreferredCheck func(channelType int) bool,
) PrepareCandidateResult {
	result := PrepareCandidateResult{}

	channel, err := ch.Get(item.ChannelID, ctx)
	if err != nil {
		log.Warnf("failed to get channel %d: %v", item.ChannelID, err)
		result.SkipReason = fmt.Sprintf("channel not found: %v", err)
		result.SkipStatus = dbmodel.AttemptSkipped
		return result
	}
	result.Channel = channel

	if !channel.Enabled {
		result.SkipReason = "channel disabled"
		result.SkipStatus = dbmodel.AttemptSkipped
		return result
	}

	usedKey := channel.GetChannelKeyWithCooldown(ratelimitCooldown)
	if usedKey.ChannelKey == "" {
		result.SkipReason = "no available key"
		result.SkipStatus = dbmodel.AttemptSkipped
		return result
	}
	result.UsedKey = usedKey

	resolvedModel := resolveCandidateModelName(requestModel, item)
	if resolvedModel == "" {
		result.SkipReason = "resolved upstream model is empty"
		result.SkipStatus = dbmodel.AttemptSkipped
		return result
	}
	result.ResolvedModel = resolvedModel

	if hint, ok := globalFailureHintCache.get(channel.ID, usedKey.ID, resolvedModel); ok {
		result.SkipReason = failureHintSkipReason(hint)
		result.SkipStatus = dbmodel.AttemptSkipped
		return result
	}

	if iter.SkipCircuitBreak(channel.ID, usedKey.ID, channel.Name, resolvedModel) {
		result.SkipReason = "circuit breaker tripped"
		result.SkipStatus = dbmodel.AttemptCircuitBreak
		return result
	}

	if zenPreferredCheck != nil && !zenPreferredCheck(int(channel.Type)) {
		result.SkipReason = "channel type not preferred for zen model prefix"
		result.SkipStatus = dbmodel.AttemptSkipped
		return result
	}

	return result
}

func PrepareCandidateForRetry(
	channel *dbmodel.Channel,
	failedKeyIDs []int,
	iter *balancer.Iterator,
	ratelimitCooldown int,
	modelName string,
) (dbmodel.ChannelKey, string) {
	usedKey := channel.GetChannelKeyExcludingWithCooldown(failedKeyIDs, ratelimitCooldown)
	if usedKey.ChannelKey == "" {
		return dbmodel.ChannelKey{}, "no more keys to retry"
	}

	if hint, ok := globalFailureHintCache.get(channel.ID, usedKey.ID, modelName); ok {
		return usedKey, failureHintSkipReason(hint)
	}

	if iter.SkipCircuitBreak(channel.ID, usedKey.ID, channel.Name, modelName) {
		return usedKey, "circuit breaker tripped on retry key"
	}

	return usedKey, ""
}

func IsRetryAllowed(decision RetryDecision) (continueRetry bool, switchChannel bool) {
	switch decision.Scope {
	case ScopeNone:
		return false, false
	case ScopeSameChannel:
		return true, false
	case ScopeNextChannel:
		return true, true
	case ScopeAbortAll:
		return false, false
	default:
		return false, false
	}
}
