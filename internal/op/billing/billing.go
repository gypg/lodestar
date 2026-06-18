package billing

/*
GGZERO commercial layer — request billing glue.

Ties octopus's relay cost accounting to per-user prepaid balance, gated by the
commercial_mode switch:
  - commercial_mode OFF (self-use): no billing; everything passes (admin uses freely).
  - commercial_mode ON: a request's API key must belong to a user with positive
    balance; after the request, its USD cost is deducted from that user.

Admin/legacy keys with UserID==0 are never billed (treated as house keys).

Logic ported from new-api's prepaid-quota model; balance kept in float USD to
match octopus's StatsMetrics cost (see internal/op/user/quota.go).
*/

import (
	"context"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/user"
)

// Enabled reports whether commercial billing is active.
func Enabled() bool {
	v, _ := setting.GetBool(model.SettingKeyCommercialMode)
	return v
}

// HasBalanceForKey reports whether a request on this key may proceed.
// Fail-open: billing off, unowned key, or any lookup error => allow (never break
// the relay hot path on a transient infra error; over-charging is avoided by the
// post-request deduction, under-charging here is acceptable).
func HasBalanceForKey(apiKeyID int, ctx context.Context) bool {
	if !Enabled() {
		return true
	}
	key, err := apikey.Get(apiKeyID, ctx)
	if err != nil || key.UserID == 0 {
		return true
	}
	remaining, _, err := user.GetQuota(key.UserID, ctx)
	if err != nil {
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
		return
	}
	_ = user.DeductQuota(key.UserID, cost, ctx)
}
