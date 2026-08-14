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
// WO-011/WO-013 — relay 媒体扣费接线断言（media_relay.go 的 ChargeKey 调用）
//
// 目标：防止重构时误删"媒体（图床）请求完成后确实走到了扣费"这条接线。
//
// WO-012 后 cost 已在 recordMediaRelayLog 内计算好（mediaCost）；WO-013 (BUG-004)
// 改为直接调 billing.ChargeKey(apiKeyID, mediaCost, ctx)——ChargeKeyWithExpr 会
// 无 body 重算表达式并覆盖 mediaCost，故媒体路径不再走它。modelName 不再随扣费
// 调用传递（ChargeKey 只有 apiKeyID+cost）。
//
// 因此这里用 billing.CallRecorder 钩子直接观察调用是否发生，断言：
//  1. 调用确实触达了 ChargeKey（这条接线是存在的）；
//  2. apiKeyID 正确（无表达式时 cost 为 0，记录无表达式媒体不扣费）。
//
// 若删除 media_relay.go 的 ChargeKey 调用 → recorder 不触发 → 红。
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

// TestMediaRelayWiring_normalCharge_reachesChargeKey
// 验证 media_relay.go 的扣费调用确实被 recordMediaRelayLog 触达。
func TestMediaRelayWiring_normalCharge_reachesChargeKey(t *testing.T) {
	initMediaRelayTestEnv(t)

	var (
		called   = false
		gotKeyID int
		gotCost  float64
	)
	billing.CallRecorder = func(apiKeyID int, _ string, _, _ int, cost float64) {
		called = true
		gotKeyID = apiKeyID
		gotCost = cost
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
		mediaUsage{},      // usage（零值 = 上游未报 usage，与 P1 #11 之前行为一致）
	)

	if !called {
		t.Fatalf("ChargeKey was not called — media billing wiring (media_relay.go) likely removed")
	}
	if gotKeyID != 77010 {
		t.Errorf("charge apiKeyID: want 77010, got %d", gotKeyID)
	}
	// 无 billing 表达式（默认环境）→ mediaCost 为 0，扣费应为 $0（不扣钱但调用存在）。
	if gotCost != 0.0 {
		t.Errorf("media charge cost: want 0.0 (no expr), got %.6f", gotCost)
	}
}

// TestMediaRelayWiring_ccBillingOffStillReachesCallSite
// 即使 commercial_mode 关闭（ChargeKey 会 no-op），调用点仍必须被触达。
// 这锁定"接线存在"与"商业模式开关"是两回事：改开关不能掩盖接线被删。
func TestMediaRelayWiring_ccBillingOffStillReachesCallSite(t *testing.T) {
	initMediaRelayTestEnv(t)

	called := false
	billing.CallRecorder = func(int, string, int, int, float64) { called = true }
	t.Cleanup(func() { billing.CallRecorder = nil })

	recordMediaRelayLog(77011, "images/generate", "images", nil, 5, "c", "gpt-image-1", time.Millisecond, nil, nil, "127.0.0.1", mediaUsage{})

	if !called {
		t.Fatalf("ChargeKey not reached even though billing is off — media wiring removed")
	}
}

// TestMediaRelayWiring_failedRequestStillCharges removed: it was testing the P2 bug itself
// (charging for failed requests). With P2 guard in place, failed requests must NOT charge.
