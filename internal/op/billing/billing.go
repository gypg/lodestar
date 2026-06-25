package billing

/*
Lodestar commercial layer — request billing glue.

Ties Lodestar's relay cost accounting to per-user prepaid balance, gated by the
commercial_mode switch:
  - commercial_mode OFF (self-use): no billing; everything passes (admin uses freely).
  - commercial_mode ON: a request's API key must belong to a user with positive
    balance; after the request, its USD cost is deducted from that user.

Admin/legacy keys with UserID==0 are never billed (treated as house keys).

Logic ported from new-api's prepaid-quota model; balance kept in float USD to
match Lodestar's StatsMetrics cost (see internal/op/user/quota.go).
*/

import (
	"context"
	"errors"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/user"
	"github.com/gypg/lodestar/internal/utils/log"
)

// Enabled reports whether commercial billing is active.
func Enabled() bool {
	v, _ := setting.GetBool(model.SettingKeyCommercialMode)
	return v
}

// HasBalanceForKey reports whether a request on this key may proceed.
// Fail-open: billing off, unowned key, or any lookup error => allow (never break
// the relay hot path on a transient infra error). When billing is on, requires
// strictly positive balance; post-request ChargeKey uses atomic DeductQuota with
// a WHERE guard so concurrent overspend is rejected (not silently under-charged).
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
	return remaining > 0
}

// ChargeKey deducts the request's USD cost from the key owner's balance.
// No-op when billing is off, cost is zero, or the key is unowned.
func ChargeKey(apiKeyID int, cost float64, ctx context.Context) {
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
	if err := user.DeductQuota(key.UserID, cost, ctx); err != nil {
		if errors.Is(err, user.ErrInsufficientBalance) {
			log.Warnf("billing charge: insufficient balance, user_id=%d api_key_id=%d cost=%.6f", key.UserID, apiKeyID, cost)
		} else {
			log.Errorf("billing charge: deduct failed, user_id=%d api_key_id=%d cost=%.6f err=%v", key.UserID, apiKeyID, cost, err)
		}
	}
}

// ChargeKeyWithExpr is like ChargeKey but checks for expression-based billing first.
// If a billing expression exists for the model, uses that to compute cost;
// otherwise falls back to the provided upstream USD cost.
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
