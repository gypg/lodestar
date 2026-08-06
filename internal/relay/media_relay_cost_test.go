package relay

/*
WO-012 — media billing (BUG-003) cost tests.

recordMediaRelayLog historically left StatsMetrics.InputCost/OutputCost at zero,
so media endpoints were billed $0 and MaxCost/MaxTokens never applied to pure
media keys. These tests lock in the fix: when a billing expression is configured
for the user-facing request model, the cost is computed from the request body via
param() and written back to both the RelayLog and the four stats updaters.
*/

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	billing "github.com/gypg/lodestar/internal/op/billing"
	"github.com/gypg/lodestar/internal/op/relaylog"
	"github.com/gypg/lodestar/internal/op/setting"
)

// initMediaRelayCostTestEnv 搭起最小 DB + 缓存 + billing_expr 设置，供计费测试用。
// billing_expr 会被显式设置成静态值，避免测试间互相污染。
func initMediaRelayCostTestEnv(t *testing.T) {
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
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
}

// TestRecordMediaRelayLog_exprPricing_writesToRelayLogAndStats
// 给 requestModel 挂 param('size') 表达式，请求体带 size，断言 cost 进入
// relayLog 与 stats.InputCost。删除 cost 计算块 → 此测试红。
func TestRecordMediaRelayLog_exprPricing_writesToRelayLogAndStats(t *testing.T) {
	initMediaRelayCostTestEnv(t)
	// requestModel 是用户可见名（对齐 metrics.go:195 的 key 口径）。
	const requestModel = "test-image-model"
	expr := `param('size') == '1024x1024' ? 5.0 : 10.0`
	if err := setting.SetString(model.SettingKeyBillingExpr,
		fmt.Sprintf(`{"%s":%q}`, requestModel, expr)); err != nil {
		t.Fatalf("set billing expr: %v", err)
	}

	body := []byte(`{"size":"1024x1024","prompt":"a cat"}`)
	rl := recordMediaRelayLogForTest(requestModel, body)

	// 媒体无输出 token，OutputCost 恒 0，故 ChargeKeyWithExpr 收到的 total cost
	// 即 InputCost。断言 ChargeCost==5.0 等价于 stats.InputCost==5.0。
	if rl.ChargeCost != 5.0 {
		t.Errorf("stats.InputCost (via charge): want 5.0, got %.9f", rl.ChargeCost)
	}
	if math.Abs(rl.RelayLogCost-5.0) > 1e-9 {
		t.Errorf("relayLog.Cost: want 5.0, got %.9f", rl.RelayLogCost)
	}
}

// TestRecordMediaRelayLog_exprPricing_paramBranch
// 请求体 size 不匹配 → 走 else 分支 10.0。防 param() 分支被写死。
func TestRecordMediaRelayLog_exprPricing_paramBranch(t *testing.T) {
	initMediaRelayCostTestEnv(t)
	const requestModel = "test-image-model"
	expr := `param('size') == '1024x1024' ? 5.0 : 10.0`
	if err := setting.SetString(model.SettingKeyBillingExpr,
		fmt.Sprintf(`{"%s":%q}`, requestModel, expr)); err != nil {
		t.Fatalf("set billing expr: %v", err)
	}

	body := []byte(`{"size":"1792x1024","prompt":"a cat"}`)
	rl := recordMediaRelayLogForTest(requestModel, body)

	if rl.ChargeCost != 10.0 {
		t.Errorf("stats.InputCost (via charge): want 10.0 (else branch), got %.9f", rl.ChargeCost)
	}
}

// TestRecordMediaRelayLog_exprPricing_usesRequestModelNotResolved
// 表达式挂在 requestModel（用户可见名）上生效，挂在 resolvedModel（上游名）上则
// 不生效 → 锁死 key 口径对齐（BUG-003 附带缺陷 ①）。这是变异测试 ③ 的靶子：
// 把 requestModel 改回 resolvedModel 后，此测试必须红。
func TestRecordMediaRelayLog_exprPricing_usesRequestModelNotResolved(t *testing.T) {
	initMediaRelayCostTestEnv(t)
	const requestModel = "user-facing-model"
	const resolvedModel = "upstream-internal-model"
	// 表达式只挂在 requestModel 上，用常量成本（不依赖 param 默认值）。
	if err := setting.SetString(model.SettingKeyBillingExpr,
		`{"user-facing-model":"6.0"}`); err != nil {
		t.Fatalf("set billing expr: %v", err)
	}

	body := []byte(`{"n":3}`)
	rl := recordMediaRelayLogForTest(requestModel, body, withResolvedModel(resolvedModel))
	_ = body

	if rl.ChargeCost != 6.0 {
		t.Errorf("stats.InputCost (via charge): want 6.0 from requestModel-keyed expr, got %.9f", rl.ChargeCost)
	}
}

// TestRecordMediaRelayLog_noExpr_costStaysZero
// 无表达式配置 → cost 全为 0（现有行为保持，防回归）。若默认计费被误开 → 红。
func TestRecordMediaRelayLog_noExpr_costStaysZero(t *testing.T) {
	initMediaRelayCostTestEnv(t)
	// 不设置 billing_expr（或设为空），模型无表达式 → cost 保持 0。
	if err := setting.SetString(model.SettingKeyBillingExpr, ""); err != nil {
		t.Fatalf("clear billing expr: %v", err)
	}

	body := []byte(`{"size":"1024x1024"}`)
	rl := recordMediaRelayLogForTest("no-expr-model", body)

	if rl.ChargeCost != 0 || rl.RelayLogCost != 0 {
		t.Errorf("no-expr model must stay $0, got Charge=%.9f RelayLog=%.9f",
			rl.ChargeCost, rl.RelayLogCost)
	}
}

// TestRecordMediaRelayLog_emptyResolvedModel_usesRequestModel
// resolvedModel 传空串（模拟 OnExhausted 从未进入 ForwardRequest）→ 不 panic，
// ActualModelName 回退到 requestModel。这是变异测试 ② 的靶子：删除空串守卫（在
// OnExhausted 内）→ 直接传空串给本函数 → ActualModelName 为空 → 红。
func TestRecordMediaRelayLog_emptyResolvedModel_usesRequestModel(t *testing.T) {
	initMediaRelayCostTestEnv(t)

	rl := recordMediaRelayLogForTest("req-model", []byte(`{"n":1}`), withResolvedModel(""))

	if rl.RelayLogActualModel != "req-model" {
		t.Errorf("ActualModelName: want %q (fallback to requestModel), got %q", "req-model", rl.RelayLogActualModel)
	}
}

// ---- 测试辅助：捕获 recordMediaRelayLog 的副作用 ----

type mediaRelayCostResult struct {
	RelayLogCost        float64
	RelayLogActualModel string
	ChargeCost          float64
}

type mediaRelayOpt func(*mediaRelayCostResult, *int, *string)

func withResolvedModel(m string) mediaRelayOpt {
	return func(_ *mediaRelayCostResult, _ *int, resolved *string) {
		*resolved = m
	}
}

// recordMediaRelayLogForTest 调用 recordMediaRelayLog，通过 hooks（setting 缓存 +
// billing.CallRecorder）捕获 cost 与模型名副作用。
func recordMediaRelayLogForTest(requestModel string, body []byte, opts ...mediaRelayOpt) mediaRelayCostResult {
	res := mediaRelayCostResult{}
	apiKeyID := 99001
	resolvedModel := "gpt-image-1"
	for _, o := range opts {
		o(&res, &apiKeyID, &resolvedModel)
	}

	// 拦截 ChargeKey 的实参，观察实际传给扣费的 cost。若调用方已安装了
	// CallRecorder（如 WO-013 的计数/余额测试），链式转发而不是覆盖，避免
	// 本辅助函数吞掉外部断言所需的事件。
	prev := billing.CallRecorder
	billing.CallRecorder = func(id int, model string, in, out int, cost float64) {
		res.ChargeCost = cost
		if prev != nil {
			prev(id, model, in, out, cost)
		}
	}
	defer func() { billing.CallRecorder = prev }()

	recordMediaRelayLog(apiKeyID, requestModel, "images", body, 5, "media-channel", resolvedModel, time.Millisecond, nil, nil, "127.0.0.1")

	// relayLog 进入内存缓存（relaylog.RelayLogAdd 追加到 relayLogCache），取最新一条。
	logs, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	if n := len(logs); n > 0 {
		last := logs[n-1]
		res.RelayLogCost = last.Cost
		res.RelayLogActualModel = last.ActualModelName
	}
	lock.Unlock()

	return res
}
