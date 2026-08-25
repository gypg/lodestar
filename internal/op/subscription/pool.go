package subscription

/*
Lodestar commercial layer — subscription quota pool drawdown.

A paid plan grants a USD pool (UserSubscription.AmountTotal). Request billing
draws from that pool first and bills only the remainder to the user's wallet
balance, so a subscriber gets what they paid for before their own money is
touched. See internal/op/billing.ChargeKey for the call site.

Accounting split, deliberately kept separate:
  - UserSubscription.AmountUsed — cumulative pool spend for this period.
  - User.UsedQuota            — cumulative WALLET spend.
So `quota + used_quota` stays a closed wallet ledger (it was one before the pool
existed and still is), and the pool has its own. A request funded entirely by the
pool therefore does not move the wallet at all.
*/

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"

	"gorm.io/gorm"
)

// ErrNonFinitePoolDraw is returned when a draw amount is NaN or ±Inf.
//
// Mirrors user.ErrNonFiniteAmount and exists for the same reason: writing NaN
// into amount_used poisons the column permanently, because every later
// `amount_total - amount_used > 0` comparison against NaN is false. The pool
// would silently stop funding anything and no admin action could arithmetically
// repair it.
var ErrNonFinitePoolDraw = errors.New("non-finite pool draw amount")

// ErrPoolContention is returned when the compare-and-swap below kept losing.
// The caller treats this as "pool drew nothing" and bills the wallet instead, so
// a hot pool degrades to wallet billing rather than serving usage for free.
var ErrPoolContention = errors.New("subscription pool draw lost too many races")

// poolDrawMaxAttempts bounds the compare-and-swap retry loop. Generous relative
// to any realistic per-user concurrency: contention is per-subscription, and one
// user's parallel requests are few.
const poolDrawMaxAttempts = 50

// activePoolSubscription returns the subscription that should fund this user's
// usage, or (nil, nil) when there is none.
//
// "Active" means all three of: status active, not past expires_at, and a pool
// with room left. Ordering by expires_at DESC matches GetUserSubscription so the
// pool that funds a request is the same one the user sees in the UI.
func activePoolSubscription(userID uint, ctx context.Context) (*model.UserSubscription, error) {
	var sub model.UserSubscription
	err := db.GetDB().WithContext(ctx).
		Where("user_id = ? AND status = ? AND expires_at > ? AND amount_total > amount_used",
			userID, model.SubStatusActive, time.Now().Unix()).
		Order("expires_at DESC, id DESC").
		First(&sub).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// PoolRemaining returns the USD still available in the user's active pool, or 0
// when there is no usable pool.
//
// The request gate uses this so a subscriber with an empty wallet is still let
// through: refusing them would be selling a pool and then not honouring it.
func PoolRemaining(userID uint, ctx context.Context) (float64, error) {
	sub, err := activePoolSubscription(userID, ctx)
	if err != nil || sub == nil {
		return 0, err
	}
	remaining := sub.AmountTotal - sub.AmountUsed
	if remaining <= 0 {
		return 0, nil
	}
	return remaining, nil
}

// DrawFromPool takes up to `amount` USD from the user's active subscription pool
// and reports how much it actually took. The caller bills `amount - drawn` to the
// wallet.
//
// A pool of 0 (AmountTotal == 0) is NO pool, not an unlimited one: it draws
// nothing and the whole cost falls through to the wallet. Selling an uncapped
// plan would make a fixed monthly price cover unbounded upstream cost, so the
// purchase path refuses such plans outright (see ErrPlanGrantsNoQuota).
//
// Concurrency: this is a compare-and-swap on amount_used, not a read-then-write.
// Two parallel requests that both read `used=0` on a pool of 10 would each think
// 10 is available; the CAS makes the loser re-read and draw only what is really
// left. A plain read-then-write hands out more pool than was ever sold.
func DrawFromPool(userID uint, amount float64, ctx context.Context) (float64, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, fmt.Errorf("%w: %v", ErrNonFinitePoolDraw, amount)
	}
	if amount <= 0 {
		return 0, nil
	}

	for attempt := 0; attempt < poolDrawMaxAttempts; attempt++ {
		sub, err := activePoolSubscription(userID, ctx)
		if err != nil {
			return 0, err
		}
		if sub == nil {
			return 0, nil
		}
		remaining := sub.AmountTotal - sub.AmountUsed
		if remaining <= 0 {
			return 0, nil
		}
		draw := math.Min(amount, remaining)

		// The `amount_used = ?` predicate is the compare half of the CAS: the row
		// only updates if nobody moved the pool since the read above.
		res := db.GetDB().WithContext(ctx).Model(&model.UserSubscription{}).
			Where("id = ? AND amount_used = ?", sub.ID, sub.AmountUsed).
			Update("amount_used", gorm.Expr("amount_used + ?", draw))
		if res.Error != nil {
			return 0, res.Error
		}
		if res.RowsAffected == 1 {
			return draw, nil
		}
		// Lost the race — another request drew from the same pool. Re-read.
	}
	return 0, ErrPoolContention
}
