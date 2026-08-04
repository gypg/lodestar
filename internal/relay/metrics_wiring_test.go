package relay

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/ratelimitstore"
	transmodel "github.com/gypg/lodestar/internal/transformer/model"
)

// initRelayMetricsTestDB brings up an in-memory SQLite database so Save can run
// end to end. Save persists stats/relay logs and spawns async persistence
// goroutines that dereference the global DB handle; without this the test
// binary panics rather than failing. Mirrors initChannelGroupTestDB in
// internal/op.
func initRelayMetricsTestDB(t *testing.T) {
	t.Helper()

	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", testName)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
}

// newUsageMetrics builds a RelayMetrics carrying a real recorded token usage,
// exactly as a finished request would.
func newUsageMetrics(apiKeyID int, modelName string, tpm int, in, out int64) *RelayMetrics {
	m := NewRelayMetrics(apiKeyID, modelName, "chat", "chat", "127.0.0.1", nil)
	m.SetTPM(tpm)
	m.SetInternalResponse(&transmodel.InternalLLMResponse{
		Usage: &transmodel.Usage{PromptTokens: in, CompletionTokens: out},
	}, modelName)
	return m
}

// TestSaveDeductsRealUsageFromTPMBucket is the WO-008 wiring test. It drives the
// real terminal call site (Save) rather than consumeRateLimitTokens directly, so
// that deleting the deduction from Save is caught.
func TestSaveDeductsRealUsageFromTPMBucket(t *testing.T) {
	initRelayMetricsTestDB(t)

	const apiID = 77001
	const modelName = "wiring-tpm"
	const tpm = 100

	// Pre-check admission, as relay.go does before forwarding (deducts 1).
	if allowed, _, _ := ratelimitstore.CheckRateLimit(apiID, modelName, 0, tpm, 0); !allowed {
		t.Fatalf("pre-check: allowed=false, want true")
	}

	// A finished request that really used 30 + 20 = 50 tokens.
	newUsageMetrics(apiID, modelName, tpm, 30, 20).Save(true, nil, nil)

	// 100 - 1 (admission) - 50 (real usage) = 49 tokens left.
	if allowed, _, _ := ratelimitstore.CheckRateLimit(apiID, modelName, 0, tpm, 50); allowed {
		t.Errorf("50-token check after pre(1)+Save(50): allowed=true, want false (49 left)")
	}
	if allowed, _, _ := ratelimitstore.CheckRateLimit(apiID, modelName, 0, tpm, 49); !allowed {
		t.Errorf("49-token check (exactly remaining): allowed=false, want true")
	}
}

// TestSaveNoOpWhenTPMUnconfigured locks in the guard: no TPM configured means
// the bucket is never touched, however many tokens the request used.
func TestSaveNoOpWhenTPMUnconfigured(t *testing.T) {
	initRelayMetricsTestDB(t)

	const apiID = 77002
	const modelName = "wiring-noop"

	newUsageMetrics(apiID, modelName, 0, 500, 500).Save(true, nil, nil)

	// The bucket was never created or touched, so a full-quota check passes.
	if allowed, _, _ := ratelimitstore.CheckRateLimit(apiID, modelName, 0, 50, 50); !allowed {
		t.Errorf("50-token check after TPM=0 Save: allowed=false, want true (bucket untouched)")
	}
}

// TestSaveIsIdempotentForOneRequest locks in the double-collection fix. The
// client-disconnect path saves twice for a single request: once via
// handleClientDisconnect inside CheckContext (relay.go:1139 -> 1115), then again
// via OnExhausted (retry_shared.go:108/122/162). The second Save must not deduct
// the request's tokens a second time.
func TestSaveIsIdempotentForOneRequest(t *testing.T) {
	initRelayMetricsTestDB(t)

	const apiID = 77003
	const modelName = "wiring-double-save"
	const tpm = 1000

	m := newUsageMetrics(apiID, modelName, tpm, 100, 100)

	// Both saves belong to the SAME request, as on the disconnect path.
	m.Save(false, errors.New("client disconnected"), nil)
	m.Save(false, errors.New("client disconnected"), nil)

	// Exactly one deduction of 200 must have happened, leaving 800. If the
	// second Save also deducted, only 600 would remain and this check fails.
	if allowed, _, _ := ratelimitstore.CheckRateLimit(apiID, modelName, 0, tpm, 800); !allowed {
		t.Errorf("800-token check after two Saves of one request: allowed=false, want true (double deduction)")
	}
}

// TestSaveDeductsUsageRecordedOnFailurePath covers the stream-then-fail case: a
// streamed response that emitted tokens before the upstream broke is collected
// by collectResponse (relay.go:546-548) and then saved as a failure. Those
// tokens were really consumed upstream, so they must still be charged to the
// TPM bucket.
func TestSaveDeductsUsageRecordedOnFailurePath(t *testing.T) {
	initRelayMetricsTestDB(t)

	const apiID = 77004
	const modelName = "wiring-failed-usage"
	const tpm = 100

	newUsageMetrics(apiID, modelName, tpm, 40, 35).Save(false, errors.New("upstream broke mid-stream"), nil)

	// 100 - 75 = 25 tokens left.
	if allowed, _, _ := ratelimitstore.CheckRateLimit(apiID, modelName, 0, tpm, 26); allowed {
		t.Errorf("26-token check after failed Save(75 used): allowed=true, want false (25 left)")
	}
	if allowed, _, _ := ratelimitstore.CheckRateLimit(apiID, modelName, 0, tpm, 25); !allowed {
		t.Errorf("25-token check (exactly remaining): allowed=false, want true")
	}
}
