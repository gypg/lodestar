package relay

import (
	"context"
	"fmt"
	"time"

	dbmodel "github.com/gypg/lodestar/internal/model"
	ch "github.com/gypg/lodestar/internal/op/channel"
	"github.com/gypg/lodestar/internal/relay/balancer"
	"github.com/gypg/lodestar/internal/utils/log"
)

// retryRoundInfo carries metadata about the current retry iteration.
type retryRoundInfo struct {
	RouteRound       int
	KeyRound         int
	MaxKeyRetries    int
	UsedKey          dbmodel.ChannelKey
	MaxTotalAttempts int
	AttemptCount     int
	Iter             *balancer.Iterator
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

	// HoldCtx is the context that rate-limit holds (rate_limit_hold.go) wait
	// on. It must be bound to the client connection so a disconnect
	// interrupts an in-progress hold: the LLM relay passes its client request
	// context here because Ctx (operationCtx) is not cancelled when the
	// client goes away. Nil means waits fall back to Ctx, which for the
	// media relay is already the request context.
	HoldCtx context.Context

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
// All varying behavior is delegated to callbacks. The optional 429 rate-limit
// hold (rate_limit_hold.go) also lives here so both the LLM and media relays
// get it; it is disabled by default and leaves every other decision untouched.
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

	rateLimitHoldCfg := getRateLimitHoldConfig()
	holdCtx := cbs.Ctx
	if cbs.HoldCtx != nil {
		holdCtx = cbs.HoldCtx
	}

	// forwardedBefore counts real upstream forwards made in *completed* route
	// rounds. Every route round builds a fresh Iterator below, so
	// routeIter.ForwardedAttempts() only knows about the current round; the
	// running total is forwardedBefore + the current iterator's count.
	//
	// This must not be len(allAttempts): allAttempts is only appended at the
	// end of a route round and on the terminal paths that return immediately,
	// so it stays at 0 for the whole of route round 1 and the cap below never
	// fires inside a round. It is also the wrong unit — Iterator.Attempts()
	// includes AttemptSkipped and AttemptCircuitBreak records, which cost no
	// upstream call and must not consume the budget.
	var forwardedBefore int
	var lastIter *balancer.Iterator

	// capReached reports whether the configured budget of real upstream
	// forwards is used up. Skips and circuit-breaker rejections are excluded
	// by ForwardedAttempts (balancer/iterator.go:157), matching the
	// countForwardedAttempts accounting used for logging (metrics.go:243).
	capReached := func(iter *balancer.Iterator) bool {
		return maxTotalAttempts > 0 && forwardedBefore+iter.ForwardedAttempts() >= maxTotalAttempts
	}

	for routeRound := 1; routeRound <= maxRouteRetries; routeRound++ {
		if ctxErr := cbs.CheckContext(); ctxErr != nil {
			lastErr = ctxErr
			cbs.OnExhausted(allAttempts, lastErr)
			return
		}

		routeIter := balancer.NewIterator(group, apiKeyID, requestModel, parseExcludedChannels(excludedChannels))
		lastIter = routeIter

		for routeIter.Next() {
			if capReached(routeIter) {
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
			// 429 hold budget is per channel: each new channel gets a
			// fresh MaxWait window.
			rateLimitHoldWaited := time.Duration(0)
			for keyRound := 1; keyRound == 1 || keyRound <= maxKeyRetriesPerRoute; keyRound++ {
				if capReached(routeIter) {
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
					// failedKeyIDs is nil on genuine first entry, so this is the plain
					// "pick the best key" call. It is non-empty only when a skip below
					// rewound keyRound back to 1; those keys must stay excluded or the
					// same key gets re-picked, re-skipped and rewound forever (spin).
					usedKey = channel.GetChannelKeyExcludingWithCooldownForModel(failedKeyIDs, ratelimitCooldown, resolvedModel)
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
					// Real upstream forwards so far, same unit as
					// MaxTotalAttempts. len(allAttempts) would be 0 for all of
					// route round 1 and would count skips.
					AttemptCount: forwardedBefore + routeIter.ForwardedAttempts(),
					Iter:         routeIter,
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

				// holdingRateLimit: this 429 will be retried inside the same
				// channel after a delay (rate_limit_hold.go). While holding,
				// the key must stay selectable, so the failure hint — which
				// would mark it unselectable on the next pick — is skipped
				// now and recorded only if the wait budget later runs out.
				holdingRateLimit := shouldHoldOnRateLimit(rateLimitHoldCfg, fwdResult.Decision) &&
					canContinueRateLimitHold(rateLimitHoldCfg, rateLimitHoldWaited)
				if !holdingRateLimit {
					recordFailureHint(channel.ID, usedKey.ID, resolvedModel, fwdResult.Decision, fwdResult.Err, ratelimitCooldown)
				}

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
					// Optional: on 429, delay-retry within the current channel
					// instead of switching keys/channels immediately. Disabled
					// by default; the historical failover behavior is kept.
					if holdingRateLimit {
						// attempt()/ForwardRequest just recorded a per-(key,
						// model) 429 cooldown; clear it so the re-pick below
						// can still select this key once the wait elapses.
						dbmodel.ClearKeyModelCooldown(usedKey.ID, resolvedModel)
						if waitRateLimitHold(holdCtx, rateLimitHoldCfg, channel.Name, rateLimitHoldWaited) {
							rateLimitHoldWaited += rateLimitHoldCfg.Interval
							if rateLimitHoldWaited > rateLimitHoldCfg.MaxWait {
								rateLimitHoldWaited = rateLimitHoldCfg.MaxWait
							}
							// The hold is time-dimensional persistence in the
							// same channel, not a key switch: it must not
							// consume a keyRound, and the wait itself is not
							// an upstream forward (R-5 counts real forwards
							// only, via ForwardedAttempts).
							keyRound--
							continue
						}
						// Wait interrupted (client disconnect / operation
						// end). Fall through to the normal key failure path;
						// the loop-top CheckContext below runs the canonical
						// cancel exit for both relays.
					}
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
		// Carry this round's real forwards into the running total before the
		// next round replaces routeIter with a fresh (zero-count) one.
		forwardedBefore += routeIter.ForwardedAttempts()
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
