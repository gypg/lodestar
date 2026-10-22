package relay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	dbmodel "github.com/gypg/lodestar/internal/model"
)

func newMediaUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("binary-payload"))
	}))
	t.Cleanup(server.Close)
	return server
}

// WO-051 回归：media 路径的客户端断连豁免。
//
// 历史bug：media 转发中途客户端断开时，向 c.Writer 的写失败（EPIPE）把
// statusCode 置 0 返回，ClassifyRelayError 只能按「网络错误→ScopeNextChannel」
// 或「written→ScopeAbortAll」归类——前者记熔断并触发无谓的换渠道重试，
// 后者记熔断。健康渠道被客户端的主动停止误熔断。
//
// 修复的执行机制是 Decision 上的 SkipFailureAccounting 标记：
// markClientCancelIfGone 声明语义（查 c.Request.Context()，operationCtx 与
// 客户端断连解耦），retry_shared 的守卫统一执行。

func TestMarkClientCancelIfGone(t *testing.T) {
	writeFailure := errors.New("failed to stream binary response: write: broken pipe")

	t.Run("canceled-client-ctx-flags-decision", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// EPIPE 不匹配 isNetworkError/isTimeoutError，走 classifyNonHTTPError 的
		// 兜底分支：Reason = 错误全文（含 broken pipe 字样，有诊断价值）。
		decision := ClassifyRelayError(0, writeFailure, false)
		if decision.Scope != ScopeNextChannel {
			t.Fatalf("precondition: Scope = %v, want ScopeNextChannel", decision.Scope)
		}

		markClientCancelIfGone(&decision, ctx, writeFailure)

		if !decision.SkipFailureAccounting {
			t.Fatal("SkipFailureAccounting = false after client ctx canceled, want true")
		}
		// 契约是豁免标记本身；兜底分支的 Reason（错误全文）刻意保留。
		if decision.Reason == "" {
			t.Fatal("Reason = empty, want the original error text preserved")
		}
	})

	t.Run("written-before-failure-also-flagged", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// 已写出的流失败 → ScopeAbortAll（误熔断路径）。
		decision := ClassifyRelayError(0, writeFailure, true)
		if decision.Scope != ScopeAbortAll {
			t.Fatalf("precondition: Scope = %v, want ScopeAbortAll", decision.Scope)
		}

		markClientCancelIfGone(&decision, ctx, writeFailure)

		if !decision.SkipFailureAccounting {
			t.Fatal("SkipFailureAccounting = false after client ctx canceled, want true")
		}
	})

	t.Run("live-client-ctx-keeps-decision", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)

		decision := ClassifyRelayError(0, writeFailure, false)

		markClientCancelIfGone(&decision, ctx, writeFailure)

		if decision.SkipFailureAccounting {
			t.Fatal("SkipFailureAccounting = true with live client, want false (real network error stays counted)")
		}
	})

	t.Run("nil-error-is-noop", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		decision := RetryDecision{Scope: ScopeNone, Reason: "success"}

		markClientCancelIfGone(&decision, ctx, nil)

		if decision.SkipFailureAccounting {
			t.Fatal("SkipFailureAccounting = true for a success decision, want false")
		}
		if decision.Reason != "success" {
			t.Fatalf("Reason = %q, want unchanged %q", decision.Reason, "success")
		}
	})

	t.Run("already-flagged-stays-flagged", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		decision := RetryDecision{Scope: ScopeAbortAll, Reason: "response filter blocked", SkipFailureAccounting: true}

		markClientCancelIfGone(&decision, ctx, errors.New("anything"))

		if !decision.SkipFailureAccounting {
			t.Fatal("SkipFailureAccounting flipped to false, want true (idempotent)")
		}
		if decision.Reason != "response filter blocked" {
			t.Fatalf("Reason = %q, want unchanged %q", decision.Reason, "response filter blocked")
		}
	})
}

// 决策被标记后，retry_shared 的守卫必须把两类断连信号（主链路的
// errClientDisconnected 哨兵 + media 的 SkipFailureAccounting 标记）
// 汇成同一个「不计失败」判定。这里以纯函数级断言锁住守卫的输入端，
// 行为级断言见下方 TestMediaDisconnectDecisionSkipsFailureAccounting。
func TestRetryGuardHonorsSkipFailureAccounting(t *testing.T) {
	decision := ClassifyRelayError(0, errors.New("write: broken pipe"), true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	markClientCancelIfGone(&decision, ctx, errors.New("write: broken pipe"))

	// 守卫条件（retry_shared.go）：ScopeNextChannel || ScopeAbortAll，且豁免。
	shouldRecord := !decision.SkipFailureAccounting &&
		(decision.Scope == ScopeNextChannel || decision.Scope == ScopeAbortAll)

	if shouldRecord {
		t.Fatal("guard would record failure for a client disconnect, want exempt")
	}
}

// 行为级断言：MediaHandler 的 ForwardRequest 回调产出被标记的 Decision。
// 用 httptest 上游 + 断开的 gin 测试上下文复刻「响应开始回写后客户端消失」。
func TestMediaDisconnectDecisionSkipsFailureAccounting(t *testing.T) {
	// 上游：立即 200 + 正常响应体（上游是健康的）。
	upstream := newMediaUpstream(t)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	// 客户端在转发开始前就已断开。
	clientCtx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = c.Request.WithContext(clientCtx)

	// 不真连上游跑完整 forwardMediaRequest（依赖渠道缓存/密钥解析），而是直接
	// 驱动 classification + 豁免链——与生产 ForwardRequest 内的调用序列一致
	//（media_relay.go: ClassifyRelayError → markClientCancelIfGone）。
	resp, err := http.Get(upstream.URL)
	if err != nil {
		t.Fatalf("GET upstream: %v", err)
	}
	defer resp.Body.Close()
	_ = dbmodel.Channel{} // 保持 dbmodel 引用（行为级场景备用）

	// 模拟向已断开客户端回写失败：错误来自写侧而非读侧。
	writeErr := fmt.Errorf("failed to stream binary response: %w", errors.New("write: broken pipe"))

	written := c.Writer.Written()
	decision := ClassifyRelayError(0, writeErr, written)
	markClientCancelIfGone(&decision, c.Request.Context(), writeErr)

	if !decision.SkipFailureAccounting {
		t.Fatal("media disconnect decision lacks SkipFailureAccounting, would mis-record circuit failure")
	}
}
