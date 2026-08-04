package ratelimitstore

import (
	"sync"
	"testing"

	"github.com/gypg/lodestar/internal/utils/ratelimit"
)

// resetBuckets clears the package-level stores so each test starts from a
// clean slate. The buckets otherwise persist across the whole process, which
// would make time- and count-dependent assertions non-deterministic.
func resetBuckets() {
	requestBuckets = sync.Map{}
	tokenBuckets = sync.Map{}
}

func TestRateLimitKey(t *testing.T) {
	// Unexported formatter, but it backs the public sync.Map keys; verify via
	// distinct IDs that collisions don't happen.
	if got := rateLimitKey(7, "gpt-4"); got != "7:gpt-4" {
		t.Errorf("rateLimitKey(7, gpt-4) = %q, want %q", got, "7:gpt-4")
	}
	if got := rateLimitKey(0, ""); got != "0:" {
		t.Errorf("rateLimitKey(0, \"\") = %q, want %q", got, "0:")
	}
}

func TestCheckRateLimitRPMOnly(t *testing.T) {
	resetBuckets()
	const apiID = 1001
	const model = "rpm-model"

	// rpm=2 → burst=2. Two requests allowed, third denied.
	allowed1, rem1, retry1 := CheckRateLimit(apiID, model, 2, 0, 0)
	if !allowed1 || rem1 != 1 || retry1 != 0 {
		t.Errorf("1st call: allowed=%v rem=%d retry=%d, want true/1/0", allowed1, rem1, retry1)
	}
	allowed2, rem2, _ := CheckRateLimit(apiID, model, 2, 0, 0)
	if !allowed2 || rem2 != 0 {
		t.Errorf("2nd call: allowed=%v rem=%d, want true/0", allowed2, rem2)
	}
	allowed3, _, retry3 := CheckRateLimit(apiID, model, 2, 0, 0)
	if allowed3 {
		t.Errorf("3rd call: allowed=true, want false (rpm exhausted)")
	}
	if retry3 != 60 {
		t.Errorf("3rd call: retryAfter=%d, want 60 (delta seconds after full drain)", retry3)
	}
}

func TestCheckRateLimitTPMOnly(t *testing.T) {
	resetBuckets()
	const apiID = 2001
	const model = "tpm-model"

	// tpm=100 → burst=100. tokenCount=60 first, 40 second, 3rd must deny.
	allowed1, _, _ := CheckRateLimit(apiID, model, 0, 100, 60)
	if !allowed1 {
		t.Errorf("1st call (60 tokens): allowed=false, want true")
	}
	allowed2, _, _ := CheckRateLimit(apiID, model, 0, 100, 40)
	if !allowed2 {
		t.Errorf("2nd call (40 tokens): allowed=false, want true")
	}
	allowed3, _, retry3 := CheckRateLimit(apiID, model, 0, 100, 1)
	if allowed3 {
		t.Errorf("3rd call: allowed=true, want false (tpm exhausted)")
	}
	if retry3 != 60 {
		t.Errorf("3rd call: retryAfter=%d, want 60 (delta seconds after full drain)", retry3)
	}
}

func TestCheckRateLimitZeroLimitsAlwaysAllows(t *testing.T) {
	resetBuckets()
	// rpm=0 and tpm=0 means no limits configured — everything passes.
	allowed, rem, retry := CheckRateLimit(3001, "unlimited", 0, 0, 9999)
	if !allowed || rem != 0 || retry != 0 {
		t.Errorf("no limits: allowed=%v rem=%d retry=%d, want true/0/0", allowed, rem, retry)
	}
}

func TestCheckRateLimitTokenCountZeroTreatedAsOne(t *testing.T) {
	resetBuckets()
	const apiID = 3002
	const model = "zero-token"
	// tpm=1, tokenCount=0 → must be treated as 1, so 1st call allowed, 2nd denied.
	allowed1, _, _ := CheckRateLimit(apiID, model, 0, 1, 0)
	if !allowed1 {
		t.Errorf("1st call with tokenCount=0: allowed=false, want true")
	}
	allowed2, _, _ := CheckRateLimit(apiID, model, 0, 1, 0)
	if allowed2 {
		t.Errorf("2nd call with tokenCount=0: allowed=true, want false (bucket had only 1 token)")
	}
}

func TestCheckRateLimitRPMTakesPrecedenceOverTPM(t *testing.T) {
	resetBuckets()
	const apiID = 4001
	const model = "precedence"
	// Exhaust RPM (1) on first call; second call must deny before even
	// checking the (ample) TPM bucket.
	CheckRateLimit(apiID, model, 1, 100000, 1)
	allowed2, _, _ := CheckRateLimit(apiID, model, 1, 100000, 1)
	if allowed2 {
		t.Errorf("2nd call: allowed=true, want false — RPM denial must precede TPM check")
	}
}

func TestCheckRateLimitIsolatesByApiKeyAndModel(t *testing.T) {
	resetBuckets()
	// Same API key, different model → independent buckets.
	CheckRateLimit(5001, "a", 1, 0, 0) // exhaust "5001:a"
	if allowed, _, _ := CheckRateLimit(5001, "a", 1, 0, 0); allowed {
		t.Errorf("5001:a 2nd call: allowed=true, want false")
	}
	if allowed, _, _ := CheckRateLimit(5001, "b", 1, 0, 0); !allowed {
		t.Errorf("5001:b 1st call: allowed=false, want true (isolated bucket)")
	}
	// Different API key, same model → independent buckets.
	if allowed, _, _ := CheckRateLimit(5002, "a", 1, 0, 0); !allowed {
		t.Errorf("5002:a 1st call: allowed=false, want true (isolated bucket)")
	}
}

func TestConsumeTokensNoOpOnNonPositive(t *testing.T) {
	resetBuckets()
	const apiID = 6001
	const model = "noop"
	// tpm<=0 → no-op: a following check must still see a full bucket.
	ConsumeTokens(apiID, model, 0, 100)
	ConsumeTokens(apiID, model, 100, 0)
	ConsumeTokens(apiID, model, 100, -5)
	if allowed, _, _ := CheckRateLimit(apiID, model, 0, 50, 50); !allowed {
		t.Errorf("after no-op ConsumeTokens, 50-token check denied; bucket was wrongly modified")
	}
}

func TestConsumeTokensDepletesSharedBucket(t *testing.T) {
	resetBuckets()
	const apiID = 6002
	const model = "shared"
	// Consume 80 of tpm=100, then a 50-token check must deny (only 20 left).
	ConsumeTokens(apiID, model, 100, 80)
	allowed, _, retry := CheckRateLimit(apiID, model, 0, 100, 50)
	if allowed {
		t.Errorf("50-token check after consuming 80/100: allowed=true, want false")
	}
	if retry != 60 {
		t.Errorf("denied check: retryAfter=%d, want 60 (delta seconds after full drain)", retry)
	}
	// Remaining 20 tokens should still satisfy a 20-token check.
	allowed20, _, _ := CheckRateLimit(apiID, model, 0, 100, 20)
	if !allowed20 {
		t.Errorf("20-token check (exactly remaining): allowed=false, want true")
	}
}

// TestPreCheckPlusPostDeductionCombined locks in the WO-008 fix contract: the
// pre-check admission (tokenCount coerced to 1 when unknown) and the post-check
// deduction of real usage together account for the true token consumption. With
// tpm=100, one pre-check (deducts 1) + ConsumeTokens(60) leaves 39 tokens, so a
// 40-token request is denied while a 39-token request still passes.
func TestPreCheckPlusPostDeductionCombined(t *testing.T) {
	resetBuckets()
	const apiID = 6003
	const model = "combined"
	const tpm = 100

	// Pre-check admission, tokenCount unknown at this point (production passes 0).
	if allowed, _, _ := CheckRateLimit(apiID, model, 0, tpm, 0); !allowed {
		t.Fatalf("pre-check: allowed=false, want true")
	}
	// Post-check: deduct the request's real usage (60 tokens).
	ConsumeTokens(apiID, model, tpm, 60)

	// Only 100 - 1 - 60 = 39 tokens remain.
	if allowed, _, _ := CheckRateLimit(apiID, model, 0, tpm, 40); allowed {
		t.Errorf("40-token check after pre(1)+post(60): allowed=true, want false (39 left)")
	}
	allowed39, _, _ := CheckRateLimit(apiID, model, 0, tpm, 39)
	if !allowed39 {
		t.Errorf("39-token check (exactly remaining): allowed=false, want true")
	}
}

// TestRetryAfterSecondsIsDeltaNotEpoch locks in D1: retryAfterSeconds returns a
// small delta-seconds wait for a denied request, not a Unix epoch timestamp. Any
// value above 3600 is a regressed epoch timestamp; the exact value is 60 for a
// full bucket drained just now.
func TestRetryAfterSecondsIsDeltaNotEpoch(t *testing.T) {
	bucket := ratelimit.NewTokenBucket(1, 1)
	bucket.Allow() // drain
	got := retryAfterSeconds(bucket)
	// Semantic assertion: this is a seconds-to-wait for a human, not a timestamp.
	if got > 3600 {
		t.Errorf("retryAfterSeconds(NewTokenBucket(1,1)) = %d, want <= 3600 (epoch seconds would be ~1.7e9)", got)
	}
	if got != 60 {
		t.Errorf("retryAfterSeconds(NewTokenBucket(1,1)) = %d, want 60 (delta seconds after full drain)", got)
	}
}

// TestRetryAfterSecondsNeverBelowOne locks in the lower bound: a zero-rate
// bucket (ResetAt == LastUpdate, so d <= 0) still yields 1, never 0; and a
// sub-second refill (d < 1s) rounds up to 1 rather than dropping to 0.
func TestRetryAfterSecondsNeverBelowOne(t *testing.T) {
	got := retryAfterSeconds(ratelimit.NewTokenBucket(0, 1))
	if got != 1 {
		t.Errorf("retryAfterSeconds(NewTokenBucket(0,1)) = %d, want 1", got)
	}
	// (600,5) refills in 0.5s; after draining, d is in (0,1s) so the rounding
	// path must still floor at 1 and never return 0.
	sub := ratelimit.NewTokenBucket(600, 5)
	sub.Allow()
	if got := retryAfterSeconds(sub); got != 1 {
		t.Errorf("retryAfterSeconds(drained (600,5)) = %d, want 1 (sub-second refill floors at 1)", got)
	}
}
