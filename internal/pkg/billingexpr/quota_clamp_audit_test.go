package billingexpr_test

import (
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/pkg/billingexpr"
)

// ---------------------------------------------------------------------------
// WO-009-续 §2.3 — QuotaFromFloat: truncates toward zero, saturates on overflow
// ---------------------------------------------------------------------------

func TestQuotaFromFloat_truncatesTowardZero(t *testing.T) {
	// Truncation, not rounding: 3.9 → 3, -3.9 → -3.
	if got := billingexpr.QuotaFromFloat(3.9); got != 3 {
		t.Errorf("QuotaFromFloat(3.9) = %d, want 3", got)
	}
	if got := billingexpr.QuotaFromFloat(-3.9); got != -3 {
		t.Errorf("QuotaFromFloat(-3.9) = %d, want -3", got)
	}
	if got := billingexpr.QuotaFromFloat(3.0); got != 3 {
		t.Errorf("QuotaFromFloat(3.0) = %d, want 3", got)
	}
}

func TestQuotaFromFloat_overflowSatSaturatesToMax(t *testing.T) {
	got, clamp := billingexpr.QuotaFromFloatChecked(2147483648.0) // MaxInt32 + 1
	if got != 2147483647 {
		t.Errorf("got %d, want 2147483647 (MaxInt32)", got)
	}
	if clamp == nil || clamp.Kind != billingexpr.QuotaClampOverflow {
		t.Errorf("want overflow clamp, got %+v", clamp)
	}
}

func TestQuotaFromFloat_underflowSaturatesToMin(t *testing.T) {
	got, clamp := billingexpr.QuotaFromFloatChecked(-2147483649.0) // MinInt32 - 1
	if got != -2147483648 {
		t.Errorf("got %d, want -2147483648 (MinInt32)", got)
	}
	if clamp == nil || clamp.Kind != billingexpr.QuotaClampUnderflow {
		t.Errorf("want underflow clamp, got %+v", clamp)
	}
}

// ---------------------------------------------------------------------------
// WO-009-续 §2.3 — QuotaClamp.Error() and AuditMap()
// ---------------------------------------------------------------------------

func TestQuotaClamp_Error_containsOverflow(t *testing.T) {
	c := &billingexpr.QuotaClamp{Op: "QuotaRound", Kind: billingexpr.QuotaClampOverflow, Original: 1e12, Clamped: 2147483647}
	err := c.Error()
	if !strings.Contains(err, "overflow") {
		t.Errorf("Error() %q missing 'overflow'", err)
	}
	if !strings.Contains(err, "1e+12") {
		t.Errorf("Error() %q missing original value", err)
	}
}

func TestQuotaClamp_Error_nil(t *testing.T) {
	var c *billingexpr.QuotaClamp
	if got := c.Error(); got != "" {
		t.Errorf("nil Error() = %q, want empty", got)
	}
}

func TestQuotaClamp_AuditMap_keys(t *testing.T) {
	c := &billingexpr.QuotaClamp{Op: "QuotaRound", Kind: billingexpr.QuotaClampOverflow, Original: 1e12, Clamped: 2147483647}
	m := c.AuditMap()
	for _, key := range []string{"op", "kind", "original", "clamped"} {
		if _, ok := m[key]; !ok {
			t.Errorf("AuditMap missing key %q: %v", key, m)
		}
	}
	if m["op"] != "QuotaRound" {
		t.Errorf("AuditMap op wrong: %v", m)
	}
	if kind, ok := m["kind"].(billingexpr.QuotaClampKind); !ok || kind != billingexpr.QuotaClampOverflow {
		t.Errorf("AuditMap kind wrong: %v", m)
	}
}

func TestQuotaClamp_AuditMap_nil(t *testing.T) {
	var c *billingexpr.QuotaClamp
	if got := c.AuditMap(); got != nil {
		t.Errorf("nil AuditMap() = %v, want nil", got)
	}
}

// ---------------------------------------------------------------------------
// WO-009-续 §2.3 — RunExprByHash: precomputed hash matches RunExpr
// ---------------------------------------------------------------------------

func TestRunExprByHash_matchesRunExpr(t *testing.T) {
	const exprStr = "p * 2.5 + c * 10"
	params := billingexpr.TokenParams{P: 1000, C: 500}

	direct, _, err := billingexpr.RunExpr(exprStr, params)
	if err != nil {
		t.Fatal(err)
	}
	byHash, _, err := billingexpr.RunExprByHash(exprStr, billingexpr.ExprHashString(exprStr), params)
	if err != nil {
		t.Fatal(err)
	}
	if direct != byHash {
		t.Errorf("RunExprByHash = %f, want %f (same as RunExpr)", byHash, direct)
	}
}

// ---------------------------------------------------------------------------
// WO-010 BUG-001 — RunExpr rejects non-finite (NaN/Inf) results
// ---------------------------------------------------------------------------

func TestRunExpr_nanRejected(t *testing.T) {
	// "0/0" in expr-lang yields NaN (float division by zero).
	_, _, err := billingexpr.RunExpr("0/0", billingexpr.TokenParams{})
	if err == nil {
		t.Fatal("want error for NaN result, got nil")
	}
}

func TestRunExpr_infRejected(t *testing.T) {
	// Division by zero on a positive numerator yields +Inf.
	_, _, err := billingexpr.RunExpr("1/0", billingexpr.TokenParams{})
	if err == nil {
		t.Fatal("want error for +Inf result, got nil")
	}
}

// ---------------------------------------------------------------------------
// WO-009-续 §2.3 — ExprVersion and UsedVars (compile.go)
// ---------------------------------------------------------------------------

func TestExprVersion(t *testing.T) {
	// Empty string → default version.
	if v := billingexpr.ExprVersion(""); v != billingexpr.DefaultExprVersion {
		t.Errorf("ExprVersion(\"\") = %d, want %d", v, billingexpr.DefaultExprVersion)
	}
	// v1: prefix → 1.
	if v := billingexpr.ExprVersion("v1:p * 2"); v != 1 {
		t.Errorf("ExprVersion(v1:...) = %d, want 1", v)
	}
	// Uncached non-empty → parsed from prefix (defaults to 1).
	if v := billingexpr.ExprVersion("p * 2"); v != 1 {
		t.Errorf("ExprVersion(no prefix) = %d, want 1", v)
	}
}

func TestUsedVars(t *testing.T) {
	billingexpr.InvalidateCache()
	vars := billingexpr.UsedVars("p * 2 + c * 3")
	if !vars["p"] || !vars["c"] {
		t.Errorf("UsedVars missing p/c: %v", vars)
	}
	if vars["q"] {
		t.Errorf("UsedVars should not contain q: %v", vars)
	}
	// Empty string → nil.
	if vars := billingexpr.UsedVars(""); vars != nil {
		t.Errorf("UsedVars(\"\") = %v, want nil", vars)
	}
}
