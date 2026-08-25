package billing

/*
Lodestar commercial layer — concurrency bound on delivered-but-unpaid usage.

The balance gate can only ask "can this account pay anything at all", because a
request's real cost is unknown until the response arrives. Settlement therefore
books delivered usage as debt and the gate refuses the NEXT request. That bound
is one request's cost — but only for requests that arrive one at a time.

Measured before this file existed: a user holding $0.005 fired 20 requests at
once against an upstream with realistic latency and ALL 20 were served, ending at
-$0.205 — 41x the prepaid amount. Every request had passed the `remaining > 0`
gate before the first one settled. The exposure was not one request; it was
concurrency x cost, with concurrency chosen by the caller.

(The same burst against a zero-latency upstream serves exactly 1, because request
1 settles before request 2 is admitted. Any concurrency test here MUST use a slow
upstream or it proves nothing — see scripts/verify-payment-chain.mjs step 11.)

The fix is an admission rule, not a pre-charge:

	headroom = max(walletRemaining, 0) + poolRemaining
	admit iff headroom > inflight * maxExpectedRequestCost

With inflight == 0 this is exactly `headroom > 0`, i.e. the previous behaviour, so
a thin-but-positive account still gets its one request and still owes for it.
With requests already in flight, an account must show headroom to cover them at
the assumed worst case before another is admitted. Accounts that cannot are
serialized rather than refused, so nobody loses access to usage they can pay for.

Deliberately NOT a pre-deduction (new-api's model): reserving money up front makes
the failure mode "customer funds locked by a missed refund", and it refuses
affordable requests whenever the estimate overshoots. This moves no money at all,
so a leaked release only over-restricts one account temporarily, and a restart
clears it.

Counters are per-process and in-memory. Lodestar runs single-instance (see
docs/DEPLOY.md); with multiple instances the bound becomes per-instance, which is
still bounded, just by instances x maxExpectedRequestCost.
*/

import (
	"context"
	"math"
	"sync"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/subscription"
	"github.com/gypg/lodestar/internal/op/user"
	"github.com/gypg/lodestar/internal/utils/log"
)

var (
	inflightMu sync.Mutex
	// inflightByUser counts requests admitted but not yet released, per user.
	// Entries are deleted at zero so an idle instance holds nothing.
	inflightByUser = make(map[uint]int)
)

// noopRelease is handed back on every path that admits without counting
// (billing off, unowned key, lookup failure), so callers can always defer it.
func noopRelease() {}

// maxExpectedRequestCost returns the assumed worst-case USD cost of one request.
// 0 (or an unparsable value) disables the concurrency bound, in which case the
// admission rule degenerates to `headroom > 0`.
func maxExpectedRequestCost() float64 {
	v, err := setting.GetFloat(model.SettingKeyMaxExpectedRequestCost)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// InflightForUser reports the current in-flight count. Test-only observability;
// production code has no reason to read it.
func InflightForUser(userID uint) int {
	inflightMu.Lock()
	defer inflightMu.Unlock()
	return inflightByUser[userID]
}

// AcquireForKey decides whether a request on this key may proceed and reserves an
// in-flight slot if so. The returned release MUST be called on every exit path —
// callers defer it immediately.
//
// It replaces the old HasBalanceForKey predicate: a pure predicate cannot bound
// concurrency, because the thing being bounded is how many requests are between
// the gate and settlement.
//
// Fail-open on infra errors (billing off, unowned key, apikey/quota lookup
// failure) so a transient fault never breaks the relay. The one exception is the
// subscription pool lookup, which fails closed — see HasBalanceForKey.
func AcquireForKey(apiKeyID int, ctx context.Context) (release func(), ok bool) {
	if !Enabled() {
		return noopRelease, true
	}
	key, err := apikey.Get(apiKeyID, ctx)
	if err != nil || key.UserID == 0 {
		if err != nil {
			log.Errorf("billing fail-open: apikey lookup failed, api_key_id=%d err=%v — allowing request", apiKeyID, err)
		}
		return noopRelease, true
	}

	headroom, ok := headroomForUser(key.UserID, apiKeyID, ctx)
	if !ok {
		return noopRelease, false
	}

	limit := maxExpectedRequestCost()

	inflightMu.Lock()
	defer inflightMu.Unlock()
	inflight := inflightByUser[key.UserID]
	if headroom <= float64(inflight)*limit {
		return noopRelease, false
	}
	inflightByUser[key.UserID] = inflight + 1

	userID := key.UserID
	var once sync.Once
	return func() {
		once.Do(func() {
			inflightMu.Lock()
			defer inflightMu.Unlock()
			if n := inflightByUser[userID] - 1; n > 0 {
				inflightByUser[userID] = n
			} else {
				delete(inflightByUser, userID)
			}
		})
	}, true
}

// headroomForUser returns how much this user can currently cover: their wallet
// balance (floored at 0, so debt does not eat into a paid pool) plus whatever
// remains in an active subscription pool.
//
// ok=false means "refuse". Infra errors return +Inf, which admits unconditionally
// regardless of how many requests are already in flight — that is the fail-open
// contract, expressed as a value rather than a second return flag. Only an
// unverifiable pool on an empty wallet refuses.
func headroomForUser(userID uint, apiKeyID int, ctx context.Context) (float64, bool) {
	remaining, _, err := user.GetQuota(userID, ctx)
	if err != nil {
		log.Errorf("billing fail-open: quota lookup failed, user_id=%d api_key_id=%d err=%v — allowing request", userID, apiKeyID, err)
		return math.Inf(1), true
	}

	pool, poolErr := subscription.PoolRemaining(userID, ctx)
	if poolErr != nil {
		if remaining > 0 {
			// The wallet alone can pay; an unverifiable pool need not block it.
			return remaining, true
		}
		log.Errorf("billing: subscription pool lookup failed, user_id=%d api_key_id=%d err=%v — refusing (wallet is empty and the pool cannot be verified)", userID, apiKeyID, poolErr)
		return 0, false
	}

	wallet := remaining
	if wallet < 0 {
		wallet = 0
	}
	return wallet + pool, true
}
