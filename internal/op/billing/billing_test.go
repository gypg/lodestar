package billing

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/user"
	"github.com/gypg/lodestar/internal/pkg/billingexpr"
)

// initBillingTestDB sets up an in-memory SQLite DB with User, APIKey, Setting tables,
// creates one user with the given quota, and one owned API key.
// Returns (userID, keyID).
func initBillingTestDB(t *testing.T, quota float64) (uint, int) {
	t.Helper()
	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()),
	)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// QuotaLedger：WO-017 起充值走 user.MutateQuota 漏斗（同事务写流水），缺表会报错。
	if err := db.GetDB().AutoMigrate(
		&model.User{}, &model.APIKey{}, &model.Setting{}, &model.UserSubscription{}, &model.QuotaLedger{},
	); err != nil {
		t.Fatal(err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Enable commercial mode so billing gates are active.
	if err := setting.SetString(model.SettingKeyCommercialMode, "true"); err != nil {
		t.Fatal(err)
	}

	u := model.User{Username: "testuser", Password: "x", Quota: quota}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	key := model.APIKey{UserID: u.ID, APIKey: "sk-test-" + t.Name()}
	if err := apikey.Create(&key, context.Background()); err != nil {
		t.Fatal(err)
	}
	return u.ID, key.ID
}

// ---------------------------------------------------------------------------
// WO-009 §2.4 — 已作废并反转：原契约「余额不足 ⇒ ChargeKey 只记 warn、余额不变」
// 正是无限白嫖的来源。新契约见 overdraft_test.go 的两条测试。
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// WO-009 §2.4 — Normal charge: balance decremented correctly
// ---------------------------------------------------------------------------

func TestChargeKey_normalCharge_deductsBalance(t *testing.T) {
	uid, kid := initBillingTestDB(t, 10.0)
	ctx := context.Background()

	ChargeKey(kid, 3.5, ctx)

	rem, used, err := user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(rem-6.5) > 1e-9 {
		t.Errorf("balance wrong: want 6.5, got %.17g", rem)
	}
	if math.Abs(used-3.5) > 1e-9 {
		t.Errorf("used_quota wrong: want 3.5, got %.17g", used)
	}
}

// ---------------------------------------------------------------------------
// WO-009 §2.4 — Zero cost: no deduction (cost <= 0 guard)
// ---------------------------------------------------------------------------

func TestChargeKey_zeroCost_noOp(t *testing.T) {
	uid, kid := initBillingTestDB(t, 5.0)
	ctx := context.Background()

	ChargeKey(kid, 0, ctx)

	rem, _, err := user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(rem-5.0) > 1e-9 {
		t.Errorf("zero cost modified balance: rem=%.17g", rem)
	}
}

// ---------------------------------------------------------------------------
// WO-009 §2.4 — Negative cost: no deduction (cost <= 0 guard)
// ---------------------------------------------------------------------------

func TestChargeKey_negativeCost_noOp(t *testing.T) {
	uid, kid := initBillingTestDB(t, 5.0)
	ctx := context.Background()

	ChargeKey(kid, -2.0, ctx)

	rem, _, err := user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(rem-5.0) > 1e-9 {
		t.Errorf("negative cost modified balance: rem=%.17g", rem)
	}
}

// ---------------------------------------------------------------------------
// WO-009 §2.5 — ComputeExprCost: NaN result is rejected (BUG-001 fix)
//
// BUG-001 (WO-010): RunExpr can return NaN (e.g. expr "0/0" in the expr-lang
// engine). Previously ComputeExprCost returned (NaN, "", true), letting NaN
// propagate into ChargeKeyWithExpr / ChargeKey where SQLite's WHERE guard
// (NaN comparisons are false) silently dropped the charge — free usage.
//
// Fixed: runProgram now rejects non-finite results with an error, so
// ComputeExprCost falls back (ok=false, cost=0) and no NaN reaches billing.
// ---------------------------------------------------------------------------

func TestComputeExprCost_nanExprRejected(t *testing.T) {
	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()),
	)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Install a NaN-producing expression for "gpt-4o" (0/0 in expr-lang).
	if err := setting.SetString(model.SettingKeyBillingExpr, `{"gpt-4o":"0/0"}`); err != nil {
		t.Fatal(err)
	}

	cost, tier, ok := ComputeExprCost("gpt-4o", 1000, 500)

	t.Logf("ComputeExprCost(0/0): cost=%v tier=%q ok=%v", cost, tier, ok)
	// BUG-001 fix: NaN must be rejected, not returned as a valid charge.
	if ok {
		t.Errorf("want ok=false for NaN expr, got ok=true cost=%v", cost)
	}
	if cost != 0 || tier != "" {
		t.Errorf("want (0, \"\", false), got (%v, %q, %v)", cost, tier, ok)
	}
}

// ---------------------------------------------------------------------------
// WO-009 §2.5 — ComputeExprCost: valid expr returns correct cost and ok=true
// ---------------------------------------------------------------------------

func TestComputeExprCost_validExpr_returnsCorrectCost(t *testing.T) {
	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()),
	)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	// p * 2.5 + c * 10 (standard pricing formula)
	if err := setting.SetString(model.SettingKeyBillingExpr, `{"gpt-4o":"p * 2.5 + c * 10"}`); err != nil {
		t.Fatal(err)
	}

	cost, _, ok := ComputeExprCost("gpt-4o", 1000, 500)
	if !ok {
		t.Fatal("want ok=true for valid expr")
	}
	want := 1000*2.5 + 500*10.0
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost=%.6f, want %.6f", cost, want)
	}
}

// ---------------------------------------------------------------------------
// WO-009 §2.3 — Refund investigation: HasBalanceForKey is pre-check only
// ---------------------------------------------------------------------------

func TestHasBalanceForKey_positivBalance_returnsTrue(t *testing.T) {
	_, kid := initBillingTestDB(t, 1.0)
	ctx := context.Background()

	if !HasBalanceForKey(kid, ctx) {
		t.Error("want true for user with positive balance")
	}
}

func TestHasBalanceForKey_zeroBalance_returnsFalse(t *testing.T) {
	_, kid := initBillingTestDB(t, 0.0)
	ctx := context.Background()

	if HasBalanceForKey(kid, ctx) {
		t.Error("want false for user with zero balance")
	}
}

func TestHasBalanceForKey_billingOff_alwaysTrue(t *testing.T) {
	_, kid := initBillingTestDB(t, 0.0)
	ctx := context.Background()
	// Turn commercial mode off — fail-open.
	_ = setting.SetString(model.SettingKeyCommercialMode, "false")

	if !HasBalanceForKey(kid, ctx) {
		t.Error("want true when billing is off (fail-open)")
	}
}

// ---------------------------------------------------------------------------
// WO-009-续 §2.2 — ChargeKeyWithExpr: billing off → no-op
// ---------------------------------------------------------------------------

func TestChargeKeyWithExpr_billingOff_noOp(t *testing.T) {
	uid, kid := initBillingTestDB(t, 10.0)
	ctx := context.Background()
	_ = setting.SetString(model.SettingKeyCommercialMode, "false")

	ChargeKeyWithExpr(kid, "gpt-4o", 1000, 500, 9.99, ctx)

	rem, _, err := user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(rem-10.0) > 1e-9 {
		t.Errorf("billing off should not deduct: rem=%.17g", rem)
	}
}

// ---------------------------------------------------------------------------
// WO-009-续 §2.2 — ChargeKeyWithExpr: expr model → charge by expr cost
// ---------------------------------------------------------------------------

func TestChargeKeyWithExpr_exprModel_chargesExprCost(t *testing.T) {
	uid, kid := initBillingTestDB(t, 10000.0)
	ctx := context.Background()
	// expr: p*2.5 + c*10 → P=1000,C=500 → 2500+5000 = 7500
	if err := setting.SetString(model.SettingKeyBillingExpr, `{"gpt-4o":"p * 2.5 + c * 10"}`); err != nil {
		t.Fatal(err)
	}

	ChargeKeyWithExpr(kid, "gpt-4o", 1000, 500, 9.99, ctx)

	rem, used, err := user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Expr cost 7500 is used (not upstream 9.99). Balance 10000 → 2500.
	if math.Abs(rem-2500.0) > 1e-6 {
		t.Errorf("want rem 2500 after expr charge 7500, got %.17g", rem)
	}
	if math.Abs(used-7500.0) > 1e-6 {
		t.Errorf("want used 7500, got %.17g", used)
	}
}

// ---------------------------------------------------------------------------
// WO-009-续 §2.2 — ChargeKeyWithExpr: no expr model → fallback to upstream cost
// ---------------------------------------------------------------------------

func TestChargeKeyWithExpr_noExpr_fallsBackToUpstream(t *testing.T) {
	uid, kid := initBillingTestDB(t, 100.0)
	ctx := context.Background()
	// No billing expr installed for this model.
	if err := setting.SetString(model.SettingKeyBillingExpr, `{"other-model":"p * 1"}`); err != nil {
		t.Fatal(err)
	}

	ChargeKeyWithExpr(kid, "gpt-4o", 1000, 500, 30.0, ctx)

	rem, used, err := user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Falls back to upstreamCost 30.0. Balance 100 → 70.
	if math.Abs(rem-70.0) > 1e-6 {
		t.Errorf("want rem 70.0 after upstream charge 30, got %.17g", rem)
	}
	if math.Abs(used-30.0) > 1e-6 {
		t.Errorf("want used 30.0, got %.17g", used)
	}
}

// ---------------------------------------------------------------------------
// WO-010 BUG-001 — relay integration: NaN expr must not silently free the user
//
// The relay hot path calls ChargeKeyWithExpr (metrics.go / media_relay.go).
// Before the fix, a NaN-producing billing expr made ComputeExprCost return
// (NaN, "", true), so ChargeKeyWithExpr "charged" NaN, which SQLite's WHERE
// guard (NaN comparisons are false) silently dropped — the user was never
// debited. After the fix, ComputeExprCost rejects NaN (ok=false) and the
// charge falls back to the upstream USD cost, so the user IS debited.
// ---------------------------------------------------------------------------

func TestChargeKeyWithExpr_nanExpr_fallsBackToUpstream(t *testing.T) {
	uid, kid := initBillingTestDB(t, 100.0)
	ctx := context.Background()
	// NaN-producing expr (0/0) for the model. BUG-001: previously this made
	// the charge silently vanish (free usage).
	if err := setting.SetString(model.SettingKeyBillingExpr, `{"gpt-4o":"0/0"}`); err != nil {
		t.Fatal(err)
	}

	// upstreamCost 30.0 is the price the relay computed from upstream pricing.
	ChargeKeyWithExpr(kid, "gpt-4o", 1000, 500, 30.0, ctx)

	rem, used, err := user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatal(err)
	}
	// NaN must NOT be charged; the user must be debited upstream 30.0 → 70.
	if math.Abs(rem-70.0) > 1e-6 {
		t.Errorf("want rem 70.0 (upstream charge applied), got %.17g — NaN silently dropped the charge", rem)
	}
	if math.Abs(used-30.0) > 1e-6 {
		t.Errorf("want used 30.0, got %.17g", used)
	}
}

// ---------------------------------------------------------------------------
// WO-009-续 §2.2 — ComputeExprCostFull: uses cr/cc dimensions
// ---------------------------------------------------------------------------

func TestComputeExprCostFull_usesCrCc(t *testing.T) {
	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()),
	)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	// expr uses cr and cc: p*2.5 + c*10 + cr*0.3
	if err := setting.SetString(model.SettingKeyBillingExpr, `{"gpt-4o":"p * 2.5 + c * 10 + cr * 0.3"}`); err != nil {
		t.Fatal(err)
	}

	cost, _, ok := ComputeExprCostFull("gpt-4o", billingexpr.TokenParams{P: 1000, C: 500, CR: 200})
	if !ok {
		t.Fatal("want ok=true for valid expr")
	}
	// 1000*2.5 + 500*10 + 200*0.3 = 2500 + 5000 + 60 = 7560
	want := 7560.0
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost=%.6f, want %.6f", cost, want)
	}
}

// ---------------------------------------------------------------------------
// WO-009-续 §2.2 — GetExprForModel: case-insensitive + missing model
// ---------------------------------------------------------------------------

func TestGetExprForModel_caseInsensitive(t *testing.T) {
	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()),
	)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := setting.SetString(model.SettingKeyBillingExpr, `{"gpt-4o":"p * 2.5"}`); err != nil {
		t.Fatal(err)
	}

	// "GPT-4O" should hit the lowercase "gpt-4o" key.
	expr, ok := GetExprForModel("GPT-4O")
	if !ok {
		t.Fatal("want ok=true for case-insensitive lookup")
	}
	if expr != "p * 2.5" {
		t.Errorf("expr=%q, want %q", expr, "p * 2.5")
	}
}

func TestGetExprForModel_missing(t *testing.T) {
	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()),
	)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := setting.SetString(model.SettingKeyBillingExpr, `{"gpt-4o":"p * 2.5"}`); err != nil {
		t.Fatal(err)
	}

	if expr, ok := GetExprForModel("nonexistent-model"); ok {
		t.Errorf("want ok=false for missing model, got expr=%q", expr)
	}
}

// ---------------------------------------------------------------------------
// WO-009-续 — HasBalanceForKey / ChargeKey fail-open error branches
// ---------------------------------------------------------------------------

func TestHasBalanceForKey_unownedKey_alwaysTrue(t *testing.T) {
	// House key (UserID==0) is never billed.
	_, houseKid := initBillingTestDB(t, 10.0)
	ctx := context.Background()
	// initBillingTestDB already set commercial mode on.
	key := model.APIKey{UserID: 0, APIKey: "sk-house-" + t.Name()}
	if err := apikey.Create(&key, ctx); err != nil {
		t.Fatal(err)
	}
	if !HasBalanceForKey(key.ID, ctx) {
		t.Error("want true for unowned (UserID==0) key (house key, never billed)")
	}
	_ = houseKid
}

func TestHasBalanceForKey_missingKey_alwaysTrue(t *testing.T) {
	// Fail-open: apikey lookup error → allow.
	_, kid := initBillingTestDB(t, 5.0)
	ctx := context.Background()
	if !HasBalanceForKey(999999, ctx) {
		t.Error("want true (fail-open) when apikey lookup fails")
	}
	_ = kid
}

func TestChargeKey_unownedKey_noOp(t *testing.T) {
	// House key: ChargeKey must not touch balance (no user to charge).
	uid, _ := initBillingTestDB(t, 10.0)
	ctx := context.Background()
	key := model.APIKey{UserID: 0, APIKey: "sk-house-" + t.Name()}
	if err := apikey.Create(&key, ctx); err != nil {
		t.Fatal(err)
	}

	ChargeKey(key.ID, 5.0, ctx)

	rem, _, err := user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(rem-10.0) > 1e-9 {
		t.Errorf("house key charge modified balance: rem=%.17g", rem)
	}
}

func TestChargeKey_missingKey_noPanic(t *testing.T) {
	// Fail-open: apikey lookup error → return without charging.
	ctx := context.Background()
	if err := setting.SetString(model.SettingKeyCommercialMode, "true"); err != nil {
		t.Fatal(err)
	}
	// Must not panic on a nonexistent key.
	ChargeKey(999999, 5.0, ctx)
}

// ---------------------------------------------------------------------------
// WO-009-续 — LoadBillingExprMap / ComputeExprCost fallback branches
// ---------------------------------------------------------------------------

func TestComputeExprCost_noExprForModel_returnsFalse(t *testing.T) {
	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()),
	)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	// No expr for "gpt-4o" → falls back, ok=false.
	if err := setting.SetString(model.SettingKeyBillingExpr, `{"other":"p * 1"}`); err != nil {
		t.Fatal(err)
	}
	cost, tier, ok := ComputeExprCost("gpt-4o", 100, 100)
	if ok {
		t.Errorf("want ok=false for model without expr, got cost=%v tier=%q", cost, tier)
	}
	if cost != 0 || tier != "" {
		t.Errorf("want (0, \"\", false), got (%v, %q, %v)", cost, tier, ok)
	}
}

func TestComputeExprCost_badExpr_fallsBack(t *testing.T) {
	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()),
	)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Unparseable expr → falls back to upstream, ok=false.
	if err := setting.SetString(model.SettingKeyBillingExpr, `{"gpt-4o":"invalid +-+ syntax"}`); err != nil {
		t.Fatal(err)
	}
	cost, tier, ok := ComputeExprCost("gpt-4o", 100, 100)
	if ok {
		t.Errorf("want ok=false for bad expr, got cost=%v tier=%q", cost, tier)
	}
	if cost != 0 || tier != "" {
		t.Errorf("want (0, \"\", false), got (%v, %q, %v)", cost, tier, ok)
	}
}

func TestComputeExprCostFull_noExprForModel_returnsFalse(t *testing.T) {
	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()),
	)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := setting.SetString(model.SettingKeyBillingExpr, `{"gpt-4o":"p * 2.5"}`); err != nil {
		t.Fatal(err)
	}
	cost, tier, ok := ComputeExprCostFull("missing-model", billingexpr.TokenParams{P: 100})
	if ok {
		t.Errorf("want ok=false for missing model, got cost=%v tier=%q", cost, tier)
	}
	if cost != 0 || tier != "" {
		t.Errorf("want (0, \"\", false), got (%v, %q, %v)", cost, tier, ok)
	}
}

func TestLoadBillingExprMap_emptySettings_returnsNil(t *testing.T) {
	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()),
	)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Ensure no billing_expr setting exists.
	if v, _ := setting.GetString(model.SettingKeyBillingExpr); v != "" {
		t.Logf("billing_expr already set, clearing")
		_ = setting.SetString(model.SettingKeyBillingExpr, "")
	}
	if m := LoadBillingExprMap(); m != nil {
		t.Errorf("want nil map for empty setting, got %v", m)
	}
}

func TestLoadBillingExprMap_jsonError_returnsNil(t *testing.T) {
	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()),
	)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Invalid JSON → nil.
	if err := setting.SetString(model.SettingKeyBillingExpr, `{"broken json`); err != nil {
		t.Fatal(err)
	}
	if m := LoadBillingExprMap(); m != nil {
		t.Errorf("want nil map for invalid JSON, got %v", m)
	}
}
