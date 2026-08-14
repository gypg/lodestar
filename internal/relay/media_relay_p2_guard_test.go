package relay

/*
P2 guard test — media billing guard against charging on relay failure.

The guard at media_relay.go:387-390 wraps billing.ChargeKey in an `if relayErr == nil`
check to prevent charging when the relay fails (e.g., OnExhausted returns 502 after
all retries exhausted). This test locks in that behavior.

Mutation targets:
- M1: Remove the `if relayErr == nil` guard → test must fail (charge happens on error)
- M2: Invert condition to `if relayErr != nil` → test must fail (never charges on success)
- M3: Change to `if relayErr == nil || true` → test must fail (always charges)
*/

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	billing "github.com/gypg/lodestar/internal/op/billing"
	"github.com/gypg/lodestar/internal/op/setting"
)

// initMediaRelayP2TestEnv sets up minimal DB + billing_expr for P2 guard tests.
func initMediaRelayP2TestEnv(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := db.GetDB().AutoMigrate(&model.Setting{}); err != nil {
		t.Fatalf("migrate setting: %v", err)
	}
	if err := setting.RefreshCache(nil); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
}

// TestMediaRelay_P2Guard_noChargeOnError verifies that billing.ChargeKey is NOT
// called when recordMediaRelayLog receives a non-nil relayErr. This is the core
// guard behavior: relay failure → no charge.
//
// Mutation M1: Remove `if relayErr == nil` guard → test fails (charge happens).
func TestMediaRelay_P2Guard_noChargeOnError(t *testing.T) {
	initMediaRelayP2TestEnv(t)

	const requestModel = "test-image-model"
	expr := `10.0` // Fixed cost for simplicity
	if err := setting.SetString(model.SettingKeyBillingExpr,
		fmt.Sprintf(`{"%s":%q}`, requestModel, expr)); err != nil {
		t.Fatalf("set billing expr: %v", err)
	}

	var chargeCalled bool
	var chargedCost float64
	prev := billing.CallRecorder
	billing.CallRecorder = func(id int, model string, in, out int, cost float64) {
		chargeCalled = true
		chargedCost = cost
		if prev != nil {
			prev(id, model, in, out, cost)
		}
	}
	defer func() { billing.CallRecorder = prev }()

	body := []byte(`{"size":"1024x1024"}`)
	relayErr := fmt.Errorf("upstream exhausted")

	// Simulate the defer block in media_relay.go that calls recordMediaRelayLog
	// with relayErr != nil. Pass nil for attempts (no channels were tried).
	recordMediaRelayLog(99001, requestModel, "images", body, 5, "ch1", "resolved-model", 0, nil, relayErr, "127.0.0.1", mediaUsage{})

	if chargeCalled {
		t.Errorf("P2 guard failed: billing.ChargeKey was called with cost=%.2f despite relayErr != nil", chargedCost)
	}
}

// TestMediaRelay_P2Guard_chargeOnSuccess verifies that billing.ChargeKey IS
// called when recordMediaRelayLog receives relayErr == nil. This ensures the
// guard doesn't block legitimate charges.
//
// Mutation M2: Invert condition to `if relayErr != nil` → test fails (no charge on success).
func TestMediaRelay_P2Guard_chargeOnSuccess(t *testing.T) {
	initMediaRelayP2TestEnv(t)

	const requestModel = "test-image-model"
	expr := `15.0`
	if err := setting.SetString(model.SettingKeyBillingExpr,
		fmt.Sprintf(`{"%s":%q}`, requestModel, expr)); err != nil {
		t.Fatalf("set billing expr: %v", err)
	}

	var chargeCalled bool
	var chargedCost float64
	prev := billing.CallRecorder
	billing.CallRecorder = func(id int, model string, in, out int, cost float64) {
		chargeCalled = true
		chargedCost = cost
		if prev != nil {
			prev(id, model, in, out, cost)
		}
	}
	defer func() { billing.CallRecorder = prev }()

	body := []byte(`{"size":"1024x1024"}`)

	// relayErr == nil (success case)
	recordMediaRelayLog(99001, requestModel, "images", body, 5, "ch1", "resolved-model", 0, nil, nil, "127.0.0.1", mediaUsage{})

	if !chargeCalled {
		t.Errorf("P2 guard over-blocked: billing.ChargeKey was NOT called despite relayErr == nil")
	}
	if chargedCost != 15.0 {
		t.Errorf("charged cost: want 15.0, got %.2f", chargedCost)
	}
}

// TestMediaRelay_P2Guard_502OnExhausted simulates the OnExhausted scenario:
// all retries failed, OnExhausted returns 502, relayErr is set. Guard must block charge.
//
// Mutation M3: Change guard to `if relayErr == nil || true` → test fails (always charges).
func TestMediaRelay_P2Guard_502OnExhausted(t *testing.T) {
	initMediaRelayP2TestEnv(t)

	const requestModel = "exhausted-model"
	expr := `20.0`
	if err := setting.SetString(model.SettingKeyBillingExpr,
		fmt.Sprintf(`{"%s":%q}`, requestModel, expr)); err != nil {
		t.Fatalf("set billing expr: %v", err)
	}

	var chargeCalled bool
	prev := billing.CallRecorder
	billing.CallRecorder = func(id int, model string, in, out int, cost float64) {
		chargeCalled = true
		if prev != nil {
			prev(id, model, in, out, cost)
		}
	}
	defer func() { billing.CallRecorder = prev }()

	body := []byte(`{"prompt":"test"}`)
	// Simulate OnExhausted returning error after all retries failed
	exhaustedErr := fmt.Errorf("all channels exhausted")

	recordMediaRelayLog(99001, requestModel, "images", body, 3, "last-ch", "resolved", 0, nil, exhaustedErr, "127.0.0.1", mediaUsage{})

	if chargeCalled {
		t.Errorf("P2 guard failed in OnExhausted scenario: charged $20 for a 502 response")
	}
}
