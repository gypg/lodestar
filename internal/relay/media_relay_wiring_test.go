package relay

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/op"
	billing "github.com/gypg/lodestar/internal/op/billing"
)

// ---------------------------------------------------------------------------
// WO-011 — relay 媒体扣费接线断言（media_relay.go:300 的 ChargeKeyWithExpr 调用）
//
// 目标：防止重构时误删"媒体（图床）请求完成后确实走到了扣费"这条接线。
//
// 现状（需在测试中验证）：media_relay.go:300 传给 ChargeKeyWithExpr 的
// stats.InputToken/OutputToken/InputCost/OutputCost 全部来自一个仅设置了
// WaitTime 的零值 StatsMetrics，因此实际扣费参数恒为 (0, 0, 0.0)——即"媒体扣费
// 当前是坏的，账单上消费为 $0"。这使余额型断言无法区分"调用被删"与"调用存在但
// 传零值"。
//
// 因此这里用 billing.CallRecorder 钩子直接观察调用是否发生，断言：
//  1. 调用确实触达了 ChargeKeyWithExpr（这条接线是存在的）；
//  2. 当前参数确实是 (0, 0, 0.0)（记录媒体扣费坏的现状，见 FIXME）。
//
// 若删除 media_relay.go:300 的调用 → recorder 不触发 → 红。
// ---------------------------------------------------------------------------

// initMediaRelayTestEnv 搭起最小环境（DB + 缓存），让 recordMediaRelayLog 的日志/
// 统计链路不 panic。调用方需自行设置 billing.CallRecorder 并在结束置回 nil。
func initMediaRelayTestEnv(t *testing.T) {
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
}

// TestMediaRelayWiring_normalCharge_reachesChargeKeyWithExpr
// 验证 media_relay.go:300 的扣费调用确实被 recordMediaRelayLog 触达。
func TestMediaRelayWiring_normalCharge_reachesChargeKeyWithExpr(t *testing.T) {
	initMediaRelayTestEnv(t)

	var (
		called        = false
		gotKeyID      int
		gotModel      string
		gotIn, gotOut int
		gotCost       float64
	)
	billing.CallRecorder = func(apiKeyID int, modelName string, in, out int, cost float64) {
		called = true
		gotKeyID = apiKeyID
		gotModel = modelName
		gotIn, gotOut, gotCost = in, out, cost
	}
	t.Cleanup(func() { billing.CallRecorder = nil })

	recordMediaRelayLog(
		77010,             // apiKeyID
		"images/generate", // requestModel
		"images",          // endpointType
		nil,               // bodyBytes
		5,                 // channelID
		"media-channel",   // channelName
		"gpt-image-1",     // resolvedModel
		time.Millisecond,  // duration
		nil,               // attempts
		nil,               // relayErr (nil = success)
		"127.0.0.1",       // clientIP
	)

	if !called {
		t.Fatalf("ChargeKeyWithExpr was not called — media billing wiring (media_relay.go:300) likely removed")
	}
	if gotKeyID != 77010 {
		t.Errorf("charge apiKeyID: want 77010, got %d", gotKeyID)
	}
	if gotModel != "gpt-image-1" {
		t.Errorf("charge model: want %q, got %q", "gpt-image-1", gotModel)
	}
	// FIXME: media billing broken — recordMediaRelayLog never sets
	// InputToken/OutputToken/InputCost/OutputCost, so the charge is always $0.
	if gotIn != 0 || gotOut != 0 || gotCost != 0.0 {
		t.Errorf("media charge args: want (0,0,0.0) documenting broken billing, got (%d,%d,%.6f)", gotIn, gotOut, gotCost)
	}
}

// TestMediaRelayWiring_ccBillingOffStillReachesCallSite
// 即使 commercial_mode 关闭（ChargeKeyWithExpr 会 no-op），调用点仍必须被触达。
// 这锁定"接线存在"与"商业模式开关"是两回事：改开关不能掩盖接线被删。
func TestMediaRelayWiring_ccBillingOffStillReachesCallSite(t *testing.T) {
	initMediaRelayTestEnv(t)

	called := false
	billing.CallRecorder = func(int, string, int, int, float64) { called = true }
	t.Cleanup(func() { billing.CallRecorder = nil })

	recordMediaRelayLog(77011, "images/generate", "images", nil, 5, "c", "gpt-image-1", time.Millisecond, nil, nil, "127.0.0.1")

	if !called {
		t.Fatalf("ChargeKeyWithExpr not reached even though billing is off — media wiring removed")
	}
}

// TestMediaRelayWiring_failedRequestStillCharges is a guard: a failed media request
// must still reach the charge call site (even though it will be a $0 charge today).
func TestMediaRelayWiring_failedRequestStillCharges(t *testing.T) {
	initMediaRelayTestEnv(t)

	called := false
	billing.CallRecorder = func(int, string, int, int, float64) { called = true }
	t.Cleanup(func() { billing.CallRecorder = nil })

	recordMediaRelayLog(77012, "images/generate", "images", nil, 5, "c", "gpt-image-1", time.Millisecond, nil, fmt.Errorf("upstream failed"), "127.0.0.1")

	if !called {
		t.Fatalf("failed media request did not reach ChargeKeyWithExpr — media wiring removed")
	}
}
