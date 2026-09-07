package relay

import (
	"errors"
	"testing"

	"github.com/gypg/lodestar/internal/transformer/model"
)

func fallbackResult(scope RetryScope) attemptResult {
	return attemptResult{
		Err:      errors.New("upstream error: 400: context_length_exceeded"),
		Decision: RetryDecision{Scope: scope, Code: 400, IsError: true},
	}
}

// TestShouldTryAdapterFallback_OnlyNextChannel
// 只有路由级失败（ScopeNextChannel）才该换出站 adapter 格式重打。
// ScopeNone 换 adapter 会再挨一次同样的 400，还推迟了把上游错误体回给下游 —— 这是 R-3。
func TestShouldTryAdapterFallback_OnlyNextChannel(t *testing.T) {
	tests := []struct {
		name  string
		scope RetryScope
		want  bool
	}{
		{name: "ScopeNextChannel: route-level failure, worth another adapter", scope: ScopeNextChannel, want: true},
		{name: "ScopeNone: 400 client error, must not re-attempt", scope: ScopeNone, want: false},
		{name: "ScopeSameChannel: key-level failure, same key anyway", scope: ScopeSameChannel, want: false},
		{name: "ScopeAbortAll: bytes already written", scope: ScopeAbortAll, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldTryAdapterFallback(fallbackResult(tt.scope), 0, 2); got != tt.want {
				t.Errorf("shouldTryAdapterFallback(%v) = %v, want %v", tt.scope, got, tt.want)
			}
		})
	}
}

func TestShouldTryAdapterFallback_StopConditions(t *testing.T) {
	t.Run("success never falls back", func(t *testing.T) {
		r := fallbackResult(ScopeNextChannel)
		r.Success = true
		if shouldTryAdapterFallback(r, 0, 2) {
			t.Error("fell back after a successful attempt")
		}
	})
	t.Run("written never falls back", func(t *testing.T) {
		r := fallbackResult(ScopeNextChannel)
		r.Written = true
		if shouldTryAdapterFallback(r, 0, 2) {
			t.Error("fell back after bytes were written to the client")
		}
	})
	t.Run("last adapter exhausted", func(t *testing.T) {
		if shouldTryAdapterFallback(fallbackResult(ScopeNextChannel), 1, 2) {
			t.Error("fell back past the last adapter index")
		}
	})
	t.Run("single adapter has nothing to fall back to", func(t *testing.T) {
		if shouldTryAdapterFallback(fallbackResult(ScopeNextChannel), 0, 1) {
			t.Error("fell back with only one adapter type")
		}
	})
}

// TestShouldTryAdapterFallback_AllowsResponsesToolHistoryMismatch — WO-043。
// Responses tool 历史转换后，严格 Responses 网关报 "No tool output found for tool call"
// 的格式性 400：不是用户 prompt 写错，换下一个 adapter（Response→Chat）可能立刻恢复。
// 错误串用 relay 真实格式（handleForwardResponse 的 upstream error: %d: %s）。
func TestShouldTryAdapterFallback_AllowsResponsesToolHistoryMismatch(t *testing.T) {
	result := attemptResult{
		Success:  false,
		Written:  false,
		Decision: RetryDecision{Scope: ScopeNone, Reason: "bad request, client error", Code: 400, IsError: true},
		Err:      errors.New(`channel foo adapter=response attempt 1/4: upstream error: 400: {"error":{"message":"No tool output found for tool call call_01_abc.","type":"invalid_request_error"}}`),
	}

	if !shouldTryAdapterFallback(result, 0, 2) {
		t.Fatal("expected Responses tool-history 400 to allow adapter fallback to chat")
	}
	if shouldTryAdapterFallback(result, 1, 2) {
		t.Fatal("expected last adapter attempt to stop even on format mismatch")
	}
}

// TestShouldTryAdapterFallback_SkipsGenericInvalidRequest — WO-043。
// 普通 400 invalid_request（如 context_length_exceeded、未知字段）必须保持终态，
// 立刻把上游错误体回给下游，不允许因格式性回退被重试。
func TestShouldTryAdapterFallback_SkipsGenericInvalidRequest(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "context length exceeded", err: errors.New(`upstream error: 400: {"error":{"message":"输入内容过长","code":"context_length_exceeded"}}`)},
		{name: "unknown field", err: errors.New(`upstream error: 400: {"error":{"message":"invalid_request_error: unknown field","type":"invalid_request_error"}}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := attemptResult{
				Success:  false,
				Written:  false,
				Decision: RetryDecision{Scope: ScopeNone, Reason: "bad request, client error", Code: 400, IsError: true},
				Err:      tt.err,
			}
			if shouldTryAdapterFallback(result, 0, 2) {
				t.Error("expected generic 400 invalid_request to stay terminal")
			}
		})
	}
}

// TestIsOutboundAdapterFormatMismatch — WO-043。窄匹配判据本身：钉死 needle 集
// 与「非 400 一律不匹配」，防止判据被加宽成任意 invalid_request 重试。
func TestIsOutboundAdapterFormatMismatch(t *testing.T) {
	if !isOutboundAdapterFormatMismatch(400, errors.New("No tool output found for function call abc")) {
		t.Fatal("expected function_call wording to match")
	}
	if !isOutboundAdapterFormatMismatch(400, errors.New("Invalid 'input[3].call_id': empty")) {
		t.Fatal("expected Invalid 'input[ to match")
	}
	if !isOutboundAdapterFormatMismatch(400, errors.New(`upstream error: 400: {"error":{"message":"function_call_output missing"}}`)) {
		t.Fatal("expected function_call_output wording to match")
	}
	if isOutboundAdapterFormatMismatch(400, errors.New("context_length_exceeded")) {
		t.Fatal("context length must not match format mismatch")
	}
	if isOutboundAdapterFormatMismatch(500, errors.New("No tool output found for tool call x")) {
		t.Fatal("non-400 must not match")
	}
	if isOutboundAdapterFormatMismatch(400, nil) {
		t.Fatal("nil error must not match")
	}
}

// TestIsLLMRequestFormat_IncludesAnthropic — R-6。
// Anthropic Messages 用的是同一套内部 Messages/Content 结构，对 OpenAI 类渠道
// 完全可以做 chat↔responses 降级。漏判它会让 Claude Code 这类 Anthropic 客户端
// 在 OpenAI 渠道上彻底拿不到 adapter fallback。
func TestIsLLMRequestFormat_IncludesAnthropic(t *testing.T) {
	tests := []struct {
		name   string
		format model.APIFormat
		want   bool
	}{
		{name: "openai chat completions", format: model.APIFormatOpenAIChatCompletion, want: true},
		{name: "openai responses", format: model.APIFormatOpenAIResponse, want: true},
		{name: "anthropic messages", format: model.APIFormatAnthropicMessage, want: true},
		{name: "openai embedding is not a chat format", format: model.APIFormatOpenAIEmbedding, want: false},
		{name: "unknown format", format: "some-future-format", want: false},
		{name: "empty format", format: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &model.InternalLLMRequest{RawAPIFormat: tt.format}
			if got := isLLMRequestFormat(req); got != tt.want {
				t.Errorf("isLLMRequestFormat(%q) = %v, want %v", tt.format, got, tt.want)
			}
		})
	}
}

func TestIsLLMRequestFormat_NilRequest(t *testing.T) {
	if isLLMRequestFormat(nil) {
		t.Error("isLLMRequestFormat(nil) = true, want false")
	}
}
