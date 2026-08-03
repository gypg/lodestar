package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAllowConsumesTokens locks in the basic happy path of Allow() on a low-rate
// bucket, so the consumed count is stable within the test window.
func TestAllowConsumesTokens(t *testing.T) {
	tb := NewTokenBucket(60, 5) // 1 token/sec, burst 5
	allowed := 0
	for i := 0; i < 5; i++ {
		if tb.Allow() {
			allowed++
		}
	}
	if allowed != 5 {
		t.Errorf("Allow() allowed %d consecutive, want 5 (burst)", allowed)
	}
	if tb.Allow() {
		t.Error("Allow() after burst exhausted returned true, want false")
	}
}

// TestAllowNDrawsDownToZero locks in that AllowN drains the bucket and that a
// subsequent Allow returns false once the burst is exhausted.
func TestAllowNDrawsDownToZero(t *testing.T) {
	tb := NewTokenBucket(60, 5)
	if !tb.AllowN(5) {
		t.Fatal("AllowN(5) on full burst returned false, want true")
	}
	if tb.AllowN(1) {
		t.Error("AllowN(1) after exhausting burst returned true, want false")
	}
}

// TestAllowNExactBoundary locks in the `>=` boundary: a bucket with exactly n
// tokens left lets AllowN(n) pass but rejects AllowN(n+1).
func TestAllowNExactBoundary(t *testing.T) {
	tb := NewTokenBucket(60, 3)
	if !tb.AllowN(3) {
		t.Fatal("AllowN(3) on burst 3 returned false, want true (>= boundary)")
	}
	if tb.AllowN(1) {
		t.Error("AllowN(1) after AllowN(3) returned true, want false")
	}
}

// TestAllowNZero locks in the degenerate case: AllowN(0) always succeeds because
// tokens >= 0 is always true, regardless of remaining tokens.
func TestAllowNZero(t *testing.T) {
	tb := NewTokenBucket(60, 2)
	if !tb.AllowN(0) {
		t.Error("AllowN(0) on full bucket returned false, want true")
	}
	tb.AllowN(2) // drain
	if !tb.AllowN(0) {
		t.Error("AllowN(0) on empty bucket returned false, want true (tokens >= 0 always holds)")
	}
}

// TestNewTokenBucketZeroZero locks in the fallback: (0,0) yields burst=1 and a
// rate of 0, so the bucket starts with 1 token and the first Allow passes.
func TestNewTokenBucketZeroZero(t *testing.T) {
	tb := NewTokenBucket(0, 0)
	if !tb.Allow() {
		t.Error("Allow() on (0,0) returned false, want true (burst falls back to 1)")
	}
}

// TestNewTokenBucketNegativeRateZeroBurst locks in the same fallback for a
// negative rate: burst still lands on 1, so the first Allow passes.
func TestNewTokenBucketNegativeRateZeroBurst(t *testing.T) {
	tb := NewTokenBucket(-5, 0)
	if !tb.Allow() {
		t.Error("Allow() on (-5,0) returned false, want true (burst falls back to 1)")
	}
}

// TestNewTokenBucketPositiveRateNegativeBurst locks in the burst clamp: a
// positive rate with burst <= 0 clamps burst to the rate, so (100,-1) -> burst 100.
func TestNewTokenBucketPositiveRateNegativeBurst(t *testing.T) {
	tb := NewTokenBucket(100, -1)
	// Stable: burst is 100 and the rate is modest, so the remaining count is
	// pinned at 100 right after construction.
	if got := tb.TokensRemaining(); got != 100 {
		t.Errorf("TokensRemaining() on (100,-1) = %d, want 100 (burst clamped to rate)", got)
	}
}

// TestTokensRemainingStaysInRange locks in that TokensRemaining never exceeds
// the burst and never goes negative, even with a negative rate.
func TestTokensRemainingStaysInRange(t *testing.T) {
	tb := NewTokenBucket(-5, 3)
	if got := tb.TokensRemaining(); got > 3 || got < 0 {
		t.Errorf("TokensRemaining() on (-5,3) = %d, want 0..3", got)
	}
}

// TestResetAtMatchesLastUpdateForNonPositiveRate locks in the ResetAt fallback:
// when rate <= 0, ResetAt returns lastUpdate (the creation time), not now.
func TestResetAtMatchesLastUpdateForNonPositiveRate(t *testing.T) {
	tb := NewTokenBucket(0, 5)
	if !tb.ResetAt().Equal(tb.LastUpdate()) {
		t.Errorf("ResetAt() = %v, LastUpdate() = %v, want equal (rate<=0 fallback returns lastUpdate)", tb.ResetAt(), tb.LastUpdate())
	}
}

// TestResetAtAfterBurstForPositiveRate locks in ResetAt for a positive rate:
// it is lastUpdate plus the full-refill duration, so it is strictly after
// lastUpdate.
func TestResetAtAfterBurstForPositiveRate(t *testing.T) {
	tb := NewTokenBucket(60, 5) // 1 token/sec, refill 5s
	if !tb.ResetAt().After(tb.LastUpdate()) {
		t.Errorf("ResetAt() = %v, want after LastUpdate() = %v for positive rate", tb.ResetAt(), tb.LastUpdate())
	}
}

// TestLastUpdateIsSetOnCreation locks in that LastUpdate is set at creation
// time and is not zero.
func TestLastUpdateIsSetOnCreation(t *testing.T) {
	tb := NewTokenBucket(60, 5)
	if tb.LastUpdate().IsZero() {
		t.Error("LastUpdate() is zero, want non-zero creation timestamp")
	}
}

// TestConcurrentAllowCountsExactlyBurst locks in thread safety without -race:
// N goroutines each take one token from a low-rate bucket whose burst is
// consumed faster than it refills, so the number of successes must equal the
// burst exactly. The atomic counter makes the assertion deterministic.
func TestConcurrentAllowCountsExactlyBurst(t *testing.T) {
	tb := NewTokenBucket(60, 10) // 1 token/sec; burst 10 drains in microseconds, refill negligible
	const goroutines = 100
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tb.Allow() {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := successes.Load(); got != 10 {
		t.Errorf("concurrent Allow() successes = %d, want 10 (burst)", got)
	}
}

// TestResetAtSubSecondRefillIsNotTruncated locks in D2: ResetAt on a bucket
// whose full-refill duration is under one second must not be truncated to 0 by
// integer division. (600,5) refills at 10 tokens/sec, so the full bucket takes
// 0.5s to refill.
func TestResetAtSubSecondRefillIsNotTruncated(t *testing.T) {
	tb := NewTokenBucket(600, 5)
	if got := tb.ResetAt().Sub(tb.LastUpdate()); got != 500*time.Millisecond {
		t.Errorf("ResetAt()-LastUpdate() on (600,5) = %v, want 500ms (integer truncation would give 0s)", got)
	}
}

// TestResetAtWholeSecondRefillUnchanged locks in that D2's fix does not disturb
// whole-second refill durations: (60,5) still refills in 5s and (100,100) in 1m.
func TestResetAtWholeSecondRefillUnchanged(t *testing.T) {
	tb := NewTokenBucket(60, 5)
	if got := tb.ResetAt().Sub(tb.LastUpdate()); got != 5*time.Second {
		t.Errorf("ResetAt()-LastUpdate() on (60,5) = %v, want 5s", got)
	}
	tb2 := NewTokenBucket(100, 100)
	if got := tb2.ResetAt().Sub(tb2.LastUpdate()); got != time.Minute {
		t.Errorf("ResetAt()-LastUpdate() on (100,100) = %v, want 1m0s", got)
	}
}

// TestNegativeRateClampedToZero locks in the P2 fix: a negative rate is clamped
// to 0 at construction, so after an hour of elapsed time the bucket still holds
// its burst rather than draining far below zero.
func TestNegativeRateClampedToZero(t *testing.T) {
	tb := NewTokenBucket(-5, 3)
	tb.mu.Lock()
	tb.lastUpdate = tb.lastUpdate.Add(-time.Hour)
	tb.mu.Unlock()
	if got := tb.TokensRemaining(); got != 3 {
		t.Errorf("TokensRemaining() on (-5,3) after 1h = %d, want 3 (negative rate must be clamped, not -297)", got)
	}
}

// TestNegativeRateBucketNeverRefills locks in that a zero-rate bucket (the
// clamped form of a negative rate) drains to 0 and never refills, and its
// ResetAt equals LastUpdate.
func TestNegativeRateBucketNeverRefills(t *testing.T) {
	tb := NewTokenBucket(-5, 3)
	for i := 0; i < 3; i++ {
		if !tb.Allow() {
			t.Fatalf("Allow() #%d on (-5,3) returned false, want true (burst 3)", i+1)
		}
	}
	if got := tb.TokensRemaining(); got != 0 {
		t.Errorf("TokensRemaining() after draining (-5,3) = %d, want 0", got)
	}
	if tb.Allow() {
		t.Error("Allow() after draining (-5,3) returned true, want false (zero rate never refills)")
	}
	if !tb.ResetAt().Equal(tb.LastUpdate()) {
		t.Errorf("ResetAt() = %v, LastUpdate() = %v, want equal (rate clamped to 0 gives fallback)", tb.ResetAt(), tb.LastUpdate())
	}
}

// TestNewTokenBucketPerHourRefillRate locks in D3: a per-hour bucket refills
// one token per 1200s at 3 tokens/hour, so refilling from empty takes 1200s.
func TestNewTokenBucketPerHourRefillRate(t *testing.T) {
	tb := NewTokenBucketPerHour(3, 3)
	for i := 0; i < 3; i++ {
		tb.Allow()
	}
	if got := tb.TokensRemaining(); got != 0 {
		t.Fatalf("TokensRemaining() after draining PerHour(3,3) = %d, want 0", got)
	}
	tb.mu.Lock()
	tb.lastUpdate = tb.lastUpdate.Add(-1200 * time.Second)
	tb.mu.Unlock()
	if got := tb.TokensRemaining(); got != 1 {
		t.Errorf("TokensRemaining() on PerHour(3,3) after 1200s = %d, want 1", got)
	}
	tb.mu.Lock()
	tb.lastUpdate = tb.lastUpdate.Add(-1200 * time.Second)
	tb.mu.Unlock()
	if got := tb.TokensRemaining(); got != 2 {
		t.Errorf("TokensRemaining() on PerHour(3,3) after 2400s = %d, want 2", got)
	}
}

// TestNewTokenBucketPerHourNotDrainedByMinute is the key D3 regression test. It
// puts the buggy NewTokenBucket and the fixed NewTokenBucketPerHour side by
// side: after 60s from empty, the per-minute bucket is already full (the bug)
// while the per-hour bucket has refilled nothing. Anyone reverting the hourly
// constructor to the per-minute one will turn this test red.
func TestNewTokenBucketPerHourNotDrainedByMinute(t *testing.T) {
	hourly := NewTokenBucketPerHour(3, 3)
	for i := 0; i < 3; i++ {
		hourly.Allow()
	}
	if got := hourly.TokensRemaining(); got != 0 {
		t.Fatalf("drained PerHour(3,3) remaining = %d, want 0", got)
	}
	hourly.mu.Lock()
	hourly.lastUpdate = hourly.lastUpdate.Add(-60 * time.Second)
	hourly.mu.Unlock()
	if got := hourly.TokensRemaining(); got != 0 {
		t.Errorf("PerHour(3,3) after 60s from empty = %d, want 0 (hourly quota must not refill in a minute)", got)
	}

	perMinute := NewTokenBucket(3, 3)
	for i := 0; i < 3; i++ {
		perMinute.Allow()
	}
	if got := perMinute.TokensRemaining(); got != 0 {
		t.Fatalf("drained NewTokenBucket(3,3) remaining = %d, want 0", got)
	}
	perMinute.mu.Lock()
	perMinute.lastUpdate = perMinute.lastUpdate.Add(-60 * time.Second)
	perMinute.mu.Unlock()
	if got := perMinute.TokensRemaining(); got != 3 {
		t.Errorf("NewTokenBucket(3,3) after 60s from empty = %d, want 3 (per-minute bucket refills in a minute — the bug)", got)
	}
}

// TestNewTokenBucketPerHourResetAt locks in ResetAt for per-hour buckets. The
// (10,10) case divides exactly to 1h, while (3,3) cannot be represented exactly
// in float64 (3/3600 is a repeating decimal), so it lands just under 1h.
func TestNewTokenBucketPerHourResetAt(t *testing.T) {
	tb := NewTokenBucketPerHour(10, 10)
	if got := tb.ResetAt().Sub(tb.LastUpdate()); got != time.Hour {
		t.Errorf("ResetAt()-LastUpdate() on PerHour(10,10) = %v, want 1h0m0s", got)
	}
	tb3 := NewTokenBucketPerHour(3, 3)
	d := tb3.ResetAt().Sub(tb3.LastUpdate())
	// float64 rounding: 3/3600 is not exactly representable, so the value lands
	// just under 1h rather than exactly on it.
	if d < 59*time.Minute+59*time.Second || d > time.Hour {
		t.Errorf("ResetAt()-LastUpdate() on PerHour(3,3) = %v, want within [59m59s, 1h]", d)
	}
}

// TestNewTokenBucketPerHourBurstFallback locks in the burst fallback for the
// per-hour constructor: a non-positive burst falls back to the default burst
// (the rate), then to 1 if the rate is also non-positive.
func TestNewTokenBucketPerHourBurstFallback(t *testing.T) {
	cases := []struct {
		name       string
		rate       int
		burst      int
		wantTokens int
		wantAllow  bool
	}{
		{"zero-zero", 0, 0, 1, true},
		{"neg-rate-zero-burst", -5, 0, 1, true},
		{"pos-rate-zero-burst", 3, 0, 3, true},
	}
	for _, tc := range cases {
		tb := NewTokenBucketPerHour(tc.rate, tc.burst)
		if got := tb.TokensRemaining(); got != tc.wantTokens {
			t.Errorf("PerHour(%d,%d) TokensRemaining() = %d, want %d", tc.rate, tc.burst, got, tc.wantTokens)
		}
		if got := tb.Allow(); got != tc.wantAllow {
			t.Errorf("PerHour(%d,%d) Allow() = %v, want %v", tc.rate, tc.burst, got, tc.wantAllow)
		}
	}
}
