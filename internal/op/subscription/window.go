package subscription

/*
Lodestar — rolling spend windows on subscription pools (WO-054).

A plan may cap spend inside a 4-hour rolling window and/or a weekly one, on
top of the total pool. The windows anchor at the subscription's start time
(the operator spec: "from when your plan started"), not the calendar hour or
week. Exhausting a window does NOT fall through to the wallet — the request
is refused with a reset time instead. That is a real rate limit, which is the
point: a fixed-price plan must never cover unbounded burst spend.

Both windows are optional: limit 0 disables the window, and a plan with both
at 0 behaves exactly as before this file existed.
*/

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"

	"gorm.io/gorm"
)

var (
	// ErrNonFiniteWindowDraw mirrors ErrNonFinitePoolDraw: NaN written into
	// `used` would poison every later `limit - used` comparison.
	ErrNonFiniteWindowDraw = errors.New("non-finite subscription window draw amount")

	// ErrWindowContention mirrors ErrPoolContention.
	ErrWindowContention = errors.New("subscription window draw lost too many races")

	// errWindowCASLost is the internal sentinel that marks a lost
	// compare-and-swap inside the commit transaction; callers never see it.
	errWindowCASLost = errors.New("subscription window cas lost")

	// windowBlockMu guards windowBlockedUntil: a tiny per-user "when does the
	// tightest blocking window reset" cache for the admission gate.
	windowBlockMu      sync.RWMutex
	windowBlockedUntil = make(map[uint]int64)
)

// RecordWindowBlock notes that a window blocked this user's spend until the
// given unix time. The admission gate uses it to answer 429 + Retry-After
// without re-deriving window state on every request.
func RecordWindowBlock(userID uint, until int64) {
	windowBlockMu.Lock()
	defer windowBlockMu.Unlock()
	windowBlockedUntil[userID] = until
}

// WindowBlockUntil returns the recorded block expiry for the user (0 = none).
// Entries expire themselves: a stale past entry is returned as 0 and dropped.
func WindowBlockUntil(userID uint) int64 {
	windowBlockMu.RLock()
	until, ok := windowBlockedUntil[userID]
	windowBlockMu.RUnlock()
	if !ok {
		return 0
	}
	if until <= time.Now().Unix() {
		windowBlockMu.Lock()
		delete(windowBlockedUntil, userID)
		windowBlockMu.Unlock()
		return 0
	}
	return until
}

// WindowState is the operator-facing view of one rolling window.
type WindowState struct {
	SubscriptionID int     `json:"subscription_id"`
	Kind           string  `json:"kind"`
	LimitUSD       float64 `json:"limit_usd"`
	WindowStart    int64   `json:"window_start"`
	Used           float64 `json:"used"`
	Remaining      float64 `json:"remaining"`
	ResetsAt       int64   `json:"resets_at"`
	UpdatedAt      int64   `json:"updated_at"`
}

// ListWindows returns the rolling-window state for one subscription (or all
// when subscriptionID is 0), with rollover applied read-only so the panel
// shows the effective current period even before the next draw commits.
func ListWindows(ctx context.Context, subscriptionID int) ([]WindowState, error) {
	q := db.GetDB().WithContext(ctx).Model(&model.SubscriptionWindow{})
	if subscriptionID > 0 {
		q = q.Where("subscription_id = ?", subscriptionID)
	}
	var rows []model.SubscriptionWindow
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]WindowState, 0, len(rows))
	for _, row := range rows {
		period, ok := windowPeriod(row.Kind)
		if !ok {
			continue
		}
		start, used, _ := rolledWindow(&row, period, now)
		remaining := row.LimitUSD - used
		if remaining < 0 {
			remaining = 0
		}
		out = append(out, WindowState{
			SubscriptionID: row.SubscriptionID,
			Kind:           row.Kind,
			LimitUSD:       row.LimitUSD,
			WindowStart:    start,
			Used:           used,
			Remaining:      remaining,
			ResetsAt:       start + int64(period.Seconds()),
			UpdatedAt:      row.UpdatedAt,
		})
	}
	return out, nil
}

// EffectiveWindowRemaining returns the tightest rolling-window headroom for
// the user's active subscription, plus the unix time at which the tightest
// EXHAUSTED window resets (0 when nothing blocks).
//
// This is the admission gate's source of truth — it reads the DB rows with
// rollover applied, so it is correct after a restart, on any instance, and for
// windows seeded out-of-band. The in-memory RecordWindowBlock cache only
// pre-answers the exact case DrawFromPool already saw.
func EffectiveWindowRemaining(userID uint, ctx context.Context) (remaining float64, blockedUntil int64, err error) {
	sub, err := activePoolSubscription(userID, ctx)
	if err != nil || sub == nil {
		return math.Inf(1), 0, err
	}
	var plan model.SubscriptionPlan
	if err := db.GetDB().WithContext(ctx).First(&plan, sub.PlanID).Error; err != nil {
		return math.Inf(1), 0, nil // plan lookup failure must not block the wallet
	}
	if plan.Window4hLimit <= 0 && plan.WindowWeeklyLimit <= 0 {
		return math.Inf(1), 0, nil
	}
	now := time.Now()
	remaining = math.Inf(1)
	for _, kind := range []string{model.SubWindow4h, model.SubWindowWeekly} {
		if planWindowLimit(&plan, kind) <= 0 {
			continue
		}
		var row model.SubscriptionWindow
		err := db.GetDB().WithContext(ctx).
			Where("subscription_id = ? AND kind = ?", sub.ID, kind).
			First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue // window row appears on first draw; no spend yet means full headroom
		}
		if err != nil {
			return math.Inf(1), 0, nil // read failure must not block the wallet
		}
		period, _ := windowPeriod(kind)
		start, used, _ := rolledWindow(&row, period, now)
		headroom := row.LimitUSD - used
		if headroom < remaining {
			remaining = headroom
		}
		if headroom <= 0 {
			if resetAt := start + int64(period.Seconds()); resetAt > blockedUntil {
				blockedUntil = resetAt
			}
		}
	}
	return remaining, blockedUntil, nil
}

// AdjustWindow moves a window's used amount by delta (negative = grant back).
// The window rolls first, so an adjustment lands in the current period.
// Returns the applied state.
func AdjustWindow(ctx context.Context, subscriptionID int, kind string, delta float64) (*WindowState, error) {
	if math.IsNaN(delta) || math.IsInf(delta, 0) {
		return nil, fmt.Errorf("%w: %v", ErrNonFiniteWindowDraw, delta)
	}
	var row model.SubscriptionWindow
	if err := db.GetDB().WithContext(ctx).
		Where("subscription_id = ? AND kind = ?", subscriptionID, kind).
		First(&row).Error; err != nil {
		return nil, err
	}
	period, ok := windowPeriod(kind)
	if !ok {
		return nil, fmt.Errorf("unknown window kind: %s", kind)
	}
	now := time.Now()
	start, used, _ := rolledWindow(&row, period, now)
	newUsed := used + delta
	if newUsed < 0 {
		newUsed = 0
	}
	res := db.GetDB().WithContext(ctx).Model(&model.SubscriptionWindow{}).
		Where("id = ? AND window_start = ? AND used = ?", row.ID, row.WindowStart, row.Used).
		Updates(map[string]any{
			"window_start": start,
			"used":         newUsed,
			"updated_at":   now.Unix(),
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrWindowContention
	}
	remaining := row.LimitUSD - newUsed
	if remaining < 0 {
		remaining = 0
	}
	return &WindowState{
		SubscriptionID: subscriptionID,
		Kind:           kind,
		LimitUSD:       row.LimitUSD,
		WindowStart:    start,
		Used:           newUsed,
		Remaining:      remaining,
		ResetsAt:       start + int64(period.Seconds()),
		UpdatedAt:      now.Unix(),
	}, nil
}

const windowDrawMaxAttempts = 50

func windowPeriod(kind string) (time.Duration, bool) {
	switch kind {
	case model.SubWindow4h:
		return 4 * time.Hour, true
	case model.SubWindowWeekly:
		return 7 * 24 * time.Hour, true
	}
	return 0, false
}

func planWindowLimit(plan *model.SubscriptionPlan, kind string) float64 {
	switch kind {
	case model.SubWindow4h:
		return plan.Window4hLimit
	case model.SubWindowWeekly:
		return plan.WindowWeeklyLimit
	}
	return 0
}

// loadOrCreateWindow returns the current window row for (sub, kind),
// creating it anchored at the subscription's start when absent. limit<=0
// returns (nil, nil): the window is disabled.
func loadOrCreateWindow(ctx context.Context, sub *model.UserSubscription, plan *model.SubscriptionPlan, kind string) (*model.SubscriptionWindow, error) {
	limit := planWindowLimit(plan, kind)
	if limit <= 0 {
		return nil, nil
	}
	var row model.SubscriptionWindow
	err := db.GetDB().WithContext(ctx).
		Where("subscription_id = ? AND kind = ?", sub.ID, kind).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = model.SubscriptionWindow{
			SubscriptionID: sub.ID,
			Kind:           kind,
			LimitUSD:       limit,
			WindowStart:    sub.StartsAt,
		}
		if err := db.GetDB().WithContext(ctx).Create(&row).Error; err != nil {
			return nil, err
		}
		return &row, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// rolledWindow advances a stale window to the period containing now. Idle
// subscriptions can skip many periods at once; the anchor lands on the
// period boundary at or before now, and used resets.
func rolledWindow(row *model.SubscriptionWindow, period time.Duration, now time.Time) (start int64, used float64, rolled bool) {
	start = row.WindowStart
	used = row.Used
	for now.Sub(time.Unix(start, 0)) >= period {
		start += int64(period.Seconds())
		used = 0
		rolled = true
	}
	return start, used, rolled
}

// DrawWindowed draws up to `amount` USD from the subscription, honouring the
// plan's rolling windows. It returns how much was actually drawn and, when a
// window blocked the remainder, the unix time at which the tightest blocking
// window resets.
//
// The caller (DrawFromPool's replacement path) bills only what was drawn;
// a window-blocked remainder is NOT billed to the wallet.
func DrawWindowed(ctx context.Context, sub *model.UserSubscription, amount float64) (drawn float64, blockedUntil int64, err error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, 0, fmt.Errorf("%w: %v", ErrNonFiniteWindowDraw, amount)
	}
	if amount <= 0 {
		return 0, 0, nil
	}
	if sub == nil {
		return 0, 0, nil
	}

	var plan model.SubscriptionPlan
	if err := db.GetDB().WithContext(ctx).First(&plan, sub.PlanID).Error; err != nil {
		return 0, 0, err
	}

	now := time.Now()
	kinds := []string{model.SubWindow4h, model.SubWindowWeekly}

	// CAS across both windows: read all rows, compute the allowed draw, then
	// update every row guarded by its (window_start, used) pair. Any lost
	// race retries the whole computation.
	for attempt := 0; attempt < windowDrawMaxAttempts; attempt++ {
		type windowUpdate struct {
			row      *model.SubscriptionWindow
			dbStart  int64
			dbUsed   float64
			newStart int64
			newUsed  float64
		}
		rows := make([]windowUpdate, 0, len(kinds))
		allowed := amount
		blockedUntil = 0
		// The total pool participates in the same allowed-amount computation
		// as the windows, so the pool CAS at commit time is a guard against a
		// race, not a source of double-charged retries: if the pool moved,
		// `allowed` was computed against a stale pool and the window rows have
		// NOT been written yet in this attempt.
		poolRemaining := sub.AmountTotal - sub.AmountUsed
		if poolRemaining < allowed {
			allowed = poolRemaining
		}
		for _, kind := range kinds {
			row, err := loadOrCreateWindow(ctx, sub, &plan, kind)
			if err != nil {
				return 0, 0, err
			}
			if row == nil {
				continue
			}
			period, _ := windowPeriod(kind)
			start, used, _ := rolledWindow(row, period, now)
			remaining := row.LimitUSD - used
			if remaining < allowed {
				allowed = remaining
				if resetAt := start + int64(period.Seconds()); resetAt > blockedUntil {
					blockedUntil = resetAt
				}
			}
			rows = append(rows, windowUpdate{
				row:      row,
				dbStart:  row.WindowStart,
				dbUsed:   row.Used,
				newStart: start,
				newUsed:  used,
			})
		}

		if allowed <= 0 {
			return 0, blockedUntil, nil
		}
		for i := range rows {
			rows[i].newUsed += allowed
		}

		// Commit pool + windows in ONE transaction: any CAS loss rolls the
		// whole attempt back, so a retry never double-charges a window and a
		// lost pool race never leaves a window unrecorded.
		committed := false
		txErr := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			poolRes := tx.Model(&model.UserSubscription{}).
				Where("id = ? AND amount_used = ?", sub.ID, sub.AmountUsed).
				Update("amount_used", gorm.Expr("amount_used + ?", allowed))
			if poolRes.Error != nil {
				return poolRes.Error
			}
			if poolRes.RowsAffected == 0 {
				return errWindowCASLost
			}
			for _, upd := range rows {
				winRes := tx.Model(&model.SubscriptionWindow{}).
					Where("id = ? AND window_start = ? AND used = ?", upd.row.ID, upd.dbStart, upd.dbUsed).
					Updates(map[string]any{
						"window_start": upd.newStart,
						"used":         upd.newUsed,
						"updated_at":   now.Unix(),
					})
				if winRes.Error != nil {
					return winRes.Error
				}
				if winRes.RowsAffected == 0 {
					return errWindowCASLost
				}
			}
			committed = true
			return nil
		})
		if txErr != nil {
			if errors.Is(txErr, errWindowCASLost) {
				// Pool or a window moved; refresh and retry the whole
				// computation. Nothing was written (single transaction).
				fresh, err := activePoolSubscription(sub.UserID, ctx)
				if err != nil {
					return 0, 0, err
				}
				if fresh == nil {
					return 0, 0, nil
				}
				*sub = *fresh
				continue
			}
			return 0, 0, txErr
		}
		if committed {
			return allowed, 0, nil
		}
	}
	return 0, 0, ErrWindowContention
}
