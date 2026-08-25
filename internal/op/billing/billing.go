package billing

/*
Lodestar commercial layer — request billing glue.

Ties Lodestar's relay cost accounting to per-user prepaid balance, gated by the
commercial_mode switch:
  - commercial_mode OFF (self-use): no billing; everything passes (admin uses freely).
  - commercial_mode ON: a request's API key must belong to a user who can pay —
    either a positive wallet balance or room in an active subscription quota
    pool. After the request, its USD cost is drawn from the pool first and the
    remainder from the wallet.

Admin/legacy keys with UserID==0 are never billed (treated as house keys).

Logic ported from new-api's prepaid-quota model; balance kept in float USD to
match Lodestar's StatsMetrics cost (see internal/op/user/quota.go). The
subscription pool half is Lodestar's own (see internal/op/subscription/pool.go).
*/

import (
	"context"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/subscription"
	"github.com/gypg/lodestar/internal/op/user"
	"github.com/gypg/lodestar/internal/utils/log"
)

// Enabled reports whether commercial billing is active.
func Enabled() bool {
	v, _ := setting.GetBool(model.SettingKeyCommercialMode)
	return v
}

// CallRecorder is an optional test-only hook that observes every ChargeKey
// invocation, including when billing is off. It is nil in production. Relay
// wiring tests (WO-011/WO-013) install a recorder to assert that the relay
// request actually reached the charge call site. It fires before the
// Enabled/cost shortcuts so a present-but-no-op charge stays observable.
// modelName/tokens are empty for direct ChargeKey calls (media path); only
// apiKeyID and cost are meaningful there.
var CallRecorder func(apiKeyID int, modelName string, inputTokens, outputTokens int, upstreamCost float64)

// HasBalanceForKey reports whether a request on this key may proceed.
// Fail-open: billing off, unowned key, or any lookup error => allow (never break
// the relay hot path on a transient infra error). When billing is on, requires
// either a strictly positive wallet balance or room left in an active
// subscription quota pool.
//
// A positive balance is NOT a guarantee that the request is affordable — the cost
// is unknown until the response arrives. Overspend is therefore settled as debt
// (user.SettleUsage lets the balance go negative), and this gate is what closes
// the loop: once the balance is negative the next request gets 402. Exposure per
// incident is one request's cost per concurrent request, not unbounded.
//
// The pool is checked only when the wallet cannot pay, so a subscriber whose
// wallet is empty still gets the usage they bought. Note the deliberate
// asymmetry: a pool lookup error fails CLOSED, unlike every other error here.
// Failing open there would mean "cannot verify a pool ⇒ serve for free", which is
// the unlimited-overdraft hole re-opened through an error path. Refusing costs us
// nothing legitimate, because we already know the wallet is empty.
func HasBalanceForKey(apiKeyID int, ctx context.Context) bool {
	if !Enabled() {
		return true
	}
	key, err := apikey.Get(apiKeyID, ctx)
	if err != nil || key.UserID == 0 {
		if err != nil {
			log.Errorf("billing fail-open: apikey lookup failed, api_key_id=%d err=%v — allowing request", apiKeyID, err)
		}
		return true
	}
	remaining, _, err := user.GetQuota(key.UserID, ctx)
	if err != nil {
		log.Errorf("billing fail-open: quota lookup failed, user_id=%d api_key_id=%d err=%v — allowing request", key.UserID, apiKeyID, err)
		return true
	}
	if remaining > 0 {
		return true
	}
	pool, poolErr := subscription.PoolRemaining(key.UserID, ctx)
	if poolErr != nil {
		log.Errorf("billing: subscription pool lookup failed, user_id=%d api_key_id=%d err=%v — refusing (wallet is empty and the pool cannot be verified)", key.UserID, apiKeyID, poolErr)
		return false
	}
	return pool > 0
}

// ChargeKey deducts the request's USD cost from the key owner, drawing on their
// active subscription quota pool first and billing only the remainder to their
// wallet balance. No-op when billing is off, cost is zero, or the key is unowned.
//
// Pool first, wallet second, is what makes a purchased plan worth buying: the
// subscriber consumes what they already paid for before their own balance is
// touched. A cost larger than the pool's remainder splits across both.
//
// If the pool lookup or draw fails, the full cost is billed to the wallet. That
// direction is deliberate: over-charging the wallet is recoverable by an admin
// credit, whereas skipping the charge is silent revenue loss, and the pool's own
// accounting stays consistent because a failed draw writes nothing.
func ChargeKey(apiKeyID int, cost float64, ctx context.Context) {
	// Test-only observation hook (WO-011/WO-013): fires before the shortcuts so
	// wiring tests can assert the relay reached this call site even when billing
	// is off or the charge is otherwise a no-op.
	if CallRecorder != nil {
		CallRecorder(apiKeyID, "", 0, 0, cost)
	}
	if !Enabled() || cost <= 0 {
		return
	}
	key, err := apikey.Get(apiKeyID, ctx)
	if err != nil || key.UserID == 0 {
		if err != nil {
			log.Errorf("billing charge: apikey lookup failed, api_key_id=%d err=%v", apiKeyID, err)
		}
		return
	}

	drawn, poolErr := subscription.DrawFromPool(key.UserID, cost, ctx)
	if poolErr != nil {
		log.Errorf("billing: subscription pool draw failed, user_id=%d api_key_id=%d cost=%.6f err=%v — billing the full cost to the wallet", key.UserID, apiKeyID, cost, poolErr)
		drawn = 0
	}
	remainder := cost - drawn
	if remainder <= 0 {
		// Fully covered by the subscription pool; the wallet is untouched.
		return
	}

	if err := user.SettleUsage(key.UserID, remainder, ctx); err != nil {
		// 结算失败不再有"余额不够"这一档 —— 用量已经交付，欠款一定记得下。
		// 剩下的都是真异常：成本算成了 NaN/±Inf（上游 usage 或定价出了问题），
		// 或者用户行没了。两者都必须响，否则就是静默漏收。
		log.Errorf("billing settle failed, user_id=%d api_key_id=%d cost=%.6f pool_drawn=%.6f wallet_remainder=%.6f err=%v — usage delivered but NOT fully charged", key.UserID, apiKeyID, cost, drawn, remainder, err)
	}
}

// ChargeKeyWithExpr is like ChargeKey but checks for expression-based billing first.
// If a billing expression exists for the model, uses that to compute cost;
// otherwise falls back to the provided upstream USD cost.
// CallRecorder fires inside ChargeKey (the common funnel), so this wrapper does
// not fire it again — a call observed there is already counted exactly once.
func ChargeKeyWithExpr(apiKeyID int, modelName string, inputTokens, outputTokens int, upstreamCost float64, ctx context.Context) {
	if !Enabled() {
		return
	}
	cost := upstreamCost
	if exprCost, _, ok := ComputeExprCost(modelName, inputTokens, outputTokens); ok {
		cost = exprCost
	}
	ChargeKey(apiKeyID, cost, ctx)
}
