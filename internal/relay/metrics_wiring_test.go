package relay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/op/ratelimitstore"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/user"
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

// ---------------------------------------------------------------------------
// WO-011 — relay 对话扣费接线断言（metrics.go:195 的 ChargeKeyWithExpr 调用）
//
// 目标：防止重构时误删"请求完成后确实扣了用户余额"这条接线。删除
// metrics.go:195 的 billing.ChargeKeyWithExpr(...) 调用后，这两个测试必须红。
//
// 策略：走真实扣费调用链（Save → billCharge → billing.ChargeKeyWithExpr →
// user.DeductQuota），用「用户余额变了多少」作为导线断言，而不是 spy 包装。
// 这样连 ChargeKeyWithExpr 内部退化（如把 cost 清零、恒走 upstream）也一并守卫。
// ---------------------------------------------------------------------------

// initCommercialRelayDB 搭建内存 SQLite + 启用 commercial_mode，创建带指定余额的
// 用户和其名下 API key。返回 (userID, keyID)。
func initCommercialRelayDB(t *testing.T, quota float64) (uint, int) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// 开启商业模式，让扣费闸门生效。
	if err := setting.SetString(model.SettingKeyCommercialMode, "true"); err != nil {
		t.Fatalf("enable commercial mode: %v", err)
	}
	u := model.User{Username: "wire-user-" + t.Name(), Password: "x", Quota: quota}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	key := model.APIKey{UserID: u.ID, Name: "wire-key", APIKey: "sk-test-" + t.Name()}
	if err := apikey.Create(&key, context.Background()); err != nil {
		t.Fatalf("create key: %v", err)
	}
	return u.ID, key.ID
}

// TestMetricsWiring_normalCharge_callsBillingWithCorrectParams
// 场景：gpt-4 输入 100、输出 50。走真实 Save 收尾路径，断言用户余额被扣了死期望值
// 0.006（gpt-4 费率 Input=30, Output=60，见 price/presets.go:49）。若
// metrics.go:195 的 ChargeKeyWithExpr 被删 → 余额不变 → 红。
func TestMetricsWiring_normalCharge_callsBillingWithCorrectParams(t *testing.T) {
	uid, kid := initCommercialRelayDB(t, 10.0)
	ctx := context.Background()

	m := NewRelayMetrics(kid, "gpt-4", "chat", "chat", "127.0.0.1", nil)
	m.SetInternalResponse(&transmodel.InternalLLMResponse{
		Usage: &transmodel.Usage{PromptTokens: 100, CompletionTokens: 50},
	}, "gpt-4")
	m.Save(true, nil, nil)

	rem, used, err := user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatalf("get quota: %v", err)
	}
	// 死期望值：gpt-4 费率 (100*30 + 50*60)*1e-6 = 0.006。
	if math.Abs(rem-(10.0-0.006)) > 1e-9 {
		t.Errorf("balance after charge: want %.9f, got %.9f — ChargeKeyWithExpr likely not wired", 10.0-0.006, rem)
	}
	if math.Abs(used-0.006) > 1e-9 {
		t.Errorf("used after charge: want 0.006, got %.17g", used)
	}
}

// TestMetricsWiring_exprBilling_chargesDeadExprValue
// 场景：模型挂了常量计费表达式 "5"。走真实 Save，断言扣除的是死期望值 5（表达式
// 用 tap 生效，而非 upstream=0）。若 ChargeKeyWithExpr 被删或退化成恒走 upstream
// → 余额不变 → 红。余额 10 → 扣 5 → 剩 5。
func TestMetricsWiring_exprBilling_chargesDeadExprValue(t *testing.T) {
	uid, kid := initCommercialRelayDB(t, 10.0)
	ctx := context.Background()
	// 常量表达式：无论 token 多少都收 $5。模型名必须与 RequestModel 一致，
	// 因为 metrics.go:195 传的是 m.RequestModel。
	if err := setting.SetString(model.SettingKeyBillingExpr, `{"wiring-model-const":"5"}`); err != nil {
		t.Fatalf("set billing expr: %v", err)
	}

	m := NewRelayMetrics(kid, "wiring-model-const", "chat", "chat", "127.0.0.1", nil)
	m.SetInternalResponse(&transmodel.InternalLLMResponse{
		Usage: &transmodel.Usage{PromptTokens: 100, CompletionTokens: 50},
	}, "wiring-model-const")
	m.Save(true, nil, nil)

	rem, used, err := user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatalf("get quota: %v", err)
	}
	if math.Abs(rem-5.0) > 1e-9 {
		t.Errorf("balance after expr charge: want 5.0, got %.17g — expr billing not wired to Save", rem)
	}
	if math.Abs(used-5.0) > 1e-9 {
		t.Errorf("used after expr charge: want 5.0, got %.17g", used)
	}
}

// TestSaveNonZeroCostOnFailureIsStillCharged guards the failure path: a request that
// recorded real token usage before the upstream broke is still debited (the user
// consumed upstream tokens). Only a zero-cost failure is free.
func TestSaveNonZeroCostOnFailureIsStillCharged(t *testing.T) {
	uid, kid := initCommercialRelayDB(t, 10.0)
	ctx := context.Background()

	m := NewRelayMetrics(kid, "gpt-4", "chat", "chat", "127.0.0.1", nil)
	m.SetInternalResponse(&transmodel.InternalLLMResponse{
		Usage: &transmodel.Usage{PromptTokens: 100, CompletionTokens: 50},
	}, "gpt-4")
	// 失败但已有真实用量：仍应扣 0.006。
	m.Save(false, errors.New("upstream broke mid-stream"), nil)

	rem, used, err := user.GetQuota(uid, ctx)
	if err != nil {
		t.Fatalf("get quota: %v", err)
	}
	if math.Abs(rem-(10.0-0.006)) > 1e-9 {
		t.Errorf("balance after failed-but-used Save: want %.9f, got %.17g", 10.0-0.006, rem)
	}
	if math.Abs(used-0.006) > 1e-9 {
		t.Errorf("used after failed-but-used Save: want 0.006, got %.17g", used)
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
