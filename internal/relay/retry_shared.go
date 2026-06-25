package relay

import (
	"context"
	"fmt"

	dbmodel "github.com/gypg/lodestar/internal/model"
	ch "github.com/gypg/lodestar/internal/op/channel"
	"github.com/gypg/lodestar/internal/relay/balancer"
	"github.com/gypg/lodestar/internal/utils/log"
)

// retryRoundInfo carries metadata about the current retry iteration.
type retryRoundInfo struct {
	RouteRound        int
	KeyRound          int
	MaxKeyRetries     int
	UsedKey           dbmodel.ChannelKey
	MaxTotalAttempts  int
	AttemptCount      int
	Iter              *balancer.Iterator
}

// retryForwardResult is returned by the per-attempt forward callback.
type retryForwardResult struct {
	Decision RetryDecision
	Err      error
}

// retryCallbacks contains the callbacks that differ between LLM relay and
// media relay. The shared retry loop calls these at the appropriate points.
type retryCallbacks struct {
	// Ctx returns the context.Context for channel lookups (e.g. c.Request.Context()
	// for media, req.operationCtx for LLM).
	Ctx context.Context

	// CheckContext is called at the top of each iteration to check whether
	// the client or operation context has been cancelled. Return a non-nil
	// error to break out of the loop immediately.
	CheckContext func() error

	// FilterChannel is called after a channel is loaded and enabled.
	// Return true to skip this channel (the callback should call iter.Skip).
	// Return false to proceed. May be nil to skip filtering.
	FilterChannel func(item dbmodel.GroupItem, channel *dbmodel.Channel, iter *balancer.Iterator) bool

	// ResolveModel overrides the default resolveCandidateModelName call.
	// Return the resolved model name, or empty string to skip the channel.
	// May be nil to use the default resolution.
	ResolveModel func(item dbmodel.GroupItem) string

	// LogAttempt is called before each forward attempt for logging.
	LogAttempt func(channel *dbmodel.Channel, resolvedModel string, round retryRoundInfo)

	// ForwardRequest performs the actual upstream request for one attempt.
	ForwardRequest func(channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, resolvedModel string, round retryRoundInfo) retryForwardResult

	// OnSuccess is called when a forward attempt succeeds.
	OnSuccess func(channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, resolvedModel string, round retryRoundInfo)

	// OnFinalFailure is called for ScopeNone / ScopeAbortAll / default failure
	// after all adapters tried. Return true if the caller handled the response
	// (metrics saved, error sent to client) and the loop should return immediately.
	OnFinalFailure func(channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, resolvedModel string, round retryRoundInfo, result retryForwardResult) bool

	// OnFailure is called when ScopeNextChannel or ScopeAbortAll is reached,
	// to record circuit breaker stats.
	OnFailure func(channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, resolvedModel string)

	// OnExhausted is called when all retry capacity is consumed or all
	// channels have been exhausted.
	OnExhausted func(allAttempts []dbmodel.ChannelAttempt, lastErr error)

	// UseFailureHints controls whether the failure-hint cache is consulted
	// before each attempt (LLM relay: true, media relay: false).
	UseFailureHints bool

	// UsePrepareCandidateForRetry controls whether PrepareCandidateForRetry
	// is used for key-round > 1 (LLM relay: true) vs direct
	// GetChannelKeyExcludingWithCooldownForModel (media relay: false).
	UsePrepareCandidateForRetry bool
}

// retryWithChannels is the shared retry loop extracted from executeRelay and
// MediaHandler. It implements:
//
//	for routeRound -> for channel -> for keyRound -> forward -> decision switch
//
// All varying behavior is delegated to callbacks.
func retryWithChannels(
	group dbmodel.Group,
	requestModel string,
	apiKeyID int,
	excludedChannels string,
	maxKeyRetriesPerRoute int,
	maxRouteRetries int,
	ratelimitCooldown int,
	maxTotalAttempts int,
	cbs retryCallbacks,
) {
	var allAttempts []dbmodel.ChannelAttempt
	var lastErr error
	var lastIter *balancer.Iterator

	for routeRound := 1; routeRound <= maxRouteRetries; routeRound++ {
		if ctxErr := cbs.CheckContext(); ctxErr != nil {
			lastErr = ctxErr
			cbs.OnExhausted(allAttempts, lastErr)
			return
		}

		routeIter := balancer.NewIterator(group, apiKeyID, requestModel, parseExcludedChannels(excludedChannels))
		lastIter = routeIter

		for routeIter.Next() {
			if maxTotalAttempts > 0 && len(allAttempts) >= maxTotalAttempts {
				lastErr = fmt.Errorf("reached relay max total attempts: %d", maxTotalAttempts)
				goto exhausted
			}
			if ctxErr := cbs.CheckContext(); ctxErr != nil {
				lastErr = ctxErr
				cbs.OnExhausted(allAttempts, lastErr)
				return
			}

			item := routeIter.Item()
			channel, err := ch.Get(item.ChannelID, cbs.Ctx)
			if err != nil {
				log.Warnf("failed to get channel %d: %v", item.ChannelID, err)
				routeIter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), fmt.Sprintf("channel not found: %v", err))
				continue
			}
			if !channel.Enabled {
				routeIter.Skip(channel.ID, 0, channel.Name, "channel disabled")
				continue
			}

			if cbs.FilterChannel != nil && cbs.FilterChannel(item, channel, routeIter) {
				continue
			}

			var resolvedModel string
			if cbs.ResolveModel != nil {
				resolvedModel = cbs.ResolveModel(item)
			} else {
				resolvedModel = resolveCandidateModelName(requestModel, item)
			}
			if resolvedModel == "" {
				routeIter.Skip(channel.ID, 0, channel.Name, "resolved upstream model is empty")
				continue
			}

			// Key-level retry within this channel
			var failedKeyIDs []int
			for keyRound := 1; keyRound == 1 || keyRound <= maxKeyRetriesPerRoute; keyRound++ {
				if maxTotalAttempts > 0 && len(allAttempts) >= maxTotalAttempts {
					lastErr = fmt.Errorf("reached relay max total attempts: %d", maxTotalAttempts)
					goto exhausted
				}
				if ctxErr := cbs.CheckContext(); ctxErr != nil {
					lastErr = ctxErr
					cbs.OnExhausted(allAttempts, lastErr)
					return
				}

				var usedKey dbmodel.ChannelKey
				if keyRound == 1 {
					usedKey = channel.GetChannelKeyExcludingWithCooldownForModel(nil, ratelimitCooldown, resolvedModel)
				} else if cbs.UsePrepareCandidateForRetry {
					usedKey, _ = PrepareCandidateForRetry(channel, failedKeyIDs, routeIter, ratelimitCooldown, resolvedModel)
				} else {
					usedKey = channel.GetChannelKeyExcludingWithCooldownForModel(failedKeyIDs, ratelimitCooldown, resolvedModel)
				}
				if usedKey.ChannelKey == "" {
					if keyRound == 1 {
						routeIter.Skip(channel.ID, usedKey.ID, channel.Name, "no available key (all keys in cooldown or disabled)")
						lastErr = fmt.Errorf("channel %s: no available key (all keys in cooldown or disabled)", channel.Name)
					}
					break
				}

				// Failure-hint skip (LLM relay only)
				if cbs.UseFailureHints {
					if hint, ok := globalFailureHintCache.get(channel.ID, usedKey.ID, resolvedModel); ok {
						failedKeyIDs = append(failedKeyIDs, usedKey.ID)
						routeIter.Skip(channel.ID, usedKey.ID, channel.Name, failureHintSkipReason(hint))
						keyRound--
						continue
					}
				}

				if routeIter.SkipCircuitBreak(channel.ID, usedKey.ID, channel.Name, resolvedModel) {
					failedKeyIDs = append(failedKeyIDs, usedKey.ID)
					keyRound--
					continue
				}

				round := retryRoundInfo{
					RouteRound:       routeRound,
					KeyRound:         keyRound,
					MaxKeyRetries:    maxKeyRetriesPerRoute,
					UsedKey:          usedKey,
					MaxTotalAttempts: maxTotalAttempts,
					AttemptCount:     len(allAttempts),
					Iter:             routeIter,
				}

				if cbs.LogAttempt != nil {
					cbs.LogAttempt(channel, resolvedModel, round)
				}

				fwdResult := cbs.ForwardRequest(channel, usedKey, resolvedModel, round)

				// Success
				if fwdResult.Decision.Scope == ScopeNone && !fwdResult.Decision.IsError {
					cbs.OnSuccess(channel, usedKey, resolvedModel, round)
					allAttempts = append(allAttempts, routeIter.Attempts()...)
					return
				}

				// Record failure stats
				if fwdResult.Decision.Scope == ScopeNextChannel || fwdResult.Decision.Scope == ScopeAbortAll {
					cbs.OnFailure(channel, usedKey, resolvedModel)
				}

				recordFailureHint(channel.ID, usedKey.ID, resolvedModel, fwdResult.Decision, fwdResult.Err, ratelimitCooldown)

				switch fwdResult.Decision.Scope {
				case ScopeNone, ScopeAbortAll:
					lastErr = fwdResult.Err
					allAttempts = append(allAttempts, routeIter.Attempts()...)
					if cbs.OnFinalFailure != nil && cbs.OnFinalFailure(channel, usedKey, resolvedModel, round, fwdResult) {
						return
					}
					cbs.OnExhausted(allAttempts, lastErr)
					return
				case ScopeSameChannel:
					lastErr = fwdResult.Err
					failedKeyIDs = append(failedKeyIDs, usedKey.ID)
				case ScopeNextChannel:
					lastErr = fwdResult.Err
					failedKeyIDs = append(failedKeyIDs, usedKey.ID)
					break
				default:
					lastErr = fwdResult.Err
					allAttempts = append(allAttempts, routeIter.Attempts()...)
					if cbs.OnFinalFailure != nil && cbs.OnFinalFailure(channel, usedKey, resolvedModel, round, fwdResult) {
						return
					}
					cbs.OnExhausted(allAttempts, lastErr)
					return
				}
			}
		}
		allAttempts = append(allAttempts, routeIter.Attempts()...)
	}

exhausted:
	// Collect remaining attempts from the last iterator if we jumped here
	// via goto (max total attempts). The iterator's attempts haven't been
	// appended yet at the point of the goto.
	if lastIter != nil {
		lastAttempts := lastIter.Attempts()
		if len(lastAttempts) > 0 && !attemptsAlreadyCollected(allAttempts, lastAttempts) {
			allAttempts = append(allAttempts, lastAttempts...)
		}
	}
	cbs.OnExhausted(allAttempts, lastErr)
}

// attemptsAlreadyCollected checks whether the given lastAttempts are already
// present in allAttempts, to avoid double-counting when goto jumps to the
// exhausted label from inside the channel iteration loop.
func attemptsAlreadyCollected(allAttempts, lastAttempts []dbmodel.ChannelAttempt) bool {
	if len(lastAttempts) == 0 {
		return true
	}
	// Heuristic: if the last entry in allAttempts matches the first entry
	// in lastAttempts, they were already appended at the end of the outer loop.
	if len(allAttempts) > 0 && len(lastAttempts) > 0 {
		last := allAttempts[len(allAttempts)-1]
		first := lastAttempts[0]
		if last.ChannelID == first.ChannelID &&
			last.ChannelKeyID == first.ChannelKeyID &&
			last.ModelName == first.ModelName &&
			last.AttemptNum == first.AttemptNum &&
			last.Status == first.Status {
			return true
		}
	}
	return false
}
