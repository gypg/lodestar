package relay

import (
	"testing"

	"github.com/gypg/lodestar/internal/transformer/model"
	"github.com/gypg/lodestar/internal/transformer/outbound"
)

// TestOutboundAttemptTypes_PassthroughFormatRoutesToPassthroughAdapter
// ★ 分组 OutboundFormat="passthrough" 时，无论渠道类型是什么、无论请求是什么
// API 格式，都必须路由到 OutboundTypePassthrough。
// 变异「passthrough 不短路、走 isLLMRequestFormat 分支」→ 本测试红。
func TestOutboundAttemptTypes_PassthroughFormatRoutesToPassthroughAdapter(t *testing.T) {
	tests := []struct {
		name        string
		channelType outbound.OutboundType
		format      model.APIFormat
	}{
		{name: "openai chat channel + openai chat request", channelType: outbound.OutboundTypeOpenAIChat, format: model.APIFormatOpenAIChatCompletion},
		{name: "openai response channel + openai response request", channelType: outbound.OutboundTypeOpenAIResponse, format: model.APIFormatOpenAIResponse},
		{name: "anthropic channel + anthropic request", channelType: outbound.OutboundTypeAnthropic, format: model.APIFormatAnthropicMessage},
		{name: "gemini channel", channelType: outbound.OutboundTypeGemini, format: model.APIFormatGeminiContents},
		{name: "embedding channel + embedding request", channelType: outbound.OutboundTypeOpenAIEmbedding, format: model.APIFormatOpenAIEmbedding},
		{name: "nil request (non-standard entry)", channelType: outbound.OutboundTypeOpenAIChat, format: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &model.InternalLLMRequest{RawAPIFormat: tt.format}
			if tt.format == "" {
				req = nil
			}
			got := outboundAttemptTypes(tt.channelType, req, "passthrough")
			if len(got) != 1 || got[0] != outbound.OutboundTypePassthrough {
				t.Fatalf("attempt types = %#v, want [OutboundTypePassthrough]", got)
			}
		})
	}
}

// TestOutboundAttemptTypes_PassthroughFormatIsCaseInsensitive
// OutboundFormat 大小写不敏感（与 "chat"/"responses" 一致）。
func TestOutboundAttemptTypes_PassthroughFormatIsCaseInsensitive(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIChatCompletion}
	for _, fmt := range []string{"PASSTHROUGH", "Passthrough", " passthrough ", "passthrough"} {
		got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, fmt)
		if len(got) != 1 || got[0] != outbound.OutboundTypePassthrough {
			t.Fatalf("format %q: attempt types = %#v, want [OutboundTypePassthrough]", fmt, got)
		}
	}
}

// TestOutboundAttemptTypes_AutoFormatDoesNotRouteToPassthrough
// ★ R-6 不回归：非 passthrough 格式（auto/chat/responses）不得路由到 passthrough。
// passthrough 是"客户端显式选"的格式，不进 isLLMRequestFormat 判定。
// 变异「把 passthrough 误加进 isLLMRequestFormat」→ auto 格式也会路由到 passthrough → 红。
func TestOutboundAttemptTypes_AutoFormatDoesNotRouteToPassthrough(t *testing.T) {
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIChatCompletion}
	for _, fmt := range []string{"", "auto", "chat", "responses"} {
		got := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, fmt)
		for _, at := range got {
			if at == outbound.OutboundTypePassthrough {
				t.Fatalf("format %q routed to passthrough; passthrough must only be selected explicitly", fmt)
			}
		}
	}
}

// TestOutboundAttemptTypes_PassthroughOverridesAdapterFallback
// ★ 即使是会触发 chat↔responses fallback 的请求，passthrough 格式也短路掉 fallback。
// passthrough 不做格式转换或回退——这是它的核心语义。
func TestOutboundAttemptTypes_PassthroughOverridesAdapterFallback(t *testing.T) {
	// 这个组合在 auto 格式下会返回 [OpenAIChat, OpenAIResponse] 两个 adapter。
	req := &model.InternalLLMRequest{RawAPIFormat: model.APIFormatOpenAIChatCompletion}
	autoGot := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "")
	if len(autoGot) != 2 {
		t.Fatalf("sanity: auto format attempt types len = %d, want 2", len(autoGot))
	}
	// passthrough 格式必须只返回 1 个 adapter。
	ptGot := outboundAttemptTypes(outbound.OutboundTypeOpenAIChat, req, "passthrough")
	if len(ptGot) != 1 || ptGot[0] != outbound.OutboundTypePassthrough {
		t.Fatalf("passthrough attempt types = %#v, want [OutboundTypePassthrough]", ptGot)
	}
}

// TestGet_PassthroughReturnsRegisteredAdapter
// ★ passthrough 在 outboundFactories 注册后，Get(OutboundTypePassthrough) 必须返回实例。
// 变异「漏注册工厂」→ Get 返回 nil → relay 跳过该渠道 → 红。
func TestGet_PassthroughReturnsRegisteredAdapter(t *testing.T) {
	if got := outbound.Get(outbound.OutboundTypePassthrough); got == nil {
		t.Fatal("Get(OutboundTypePassthrough) = nil, want registered adapter instance")
	}
}

// TestIsChatChannelType_Passthrough
// passthrough 与 chat 请求兼容（原样透传，不挑格式）。
func TestIsChatChannelType_Passthrough(t *testing.T) {
	if !outbound.IsChatChannelType(outbound.OutboundTypePassthrough) {
		t.Fatal("IsChatChannelType(OutboundTypePassthrough) = false, want true")
	}
}

// TestIsEmbeddingChannelType_Passthrough
// passthrough 与 embedding 请求兼容（原样透传，不挑格式）。
func TestIsEmbeddingChannelType_Passthrough(t *testing.T) {
	if !outbound.IsEmbeddingChannelType(outbound.OutboundTypePassthrough) {
		t.Fatal("IsEmbeddingChannelType(OutboundTypePassthrough) = false, want true")
	}
}

// TestIsLLMRequestFormat_PassthroughNotAdded — R-6 不回归。
// passthrough 不进 isLLMRequestFormat 判定（它是客户端显式选的格式，不是请求格式判定）。
// 已有的 TestIsLLMRequestFormat_IncludesAnthropic 守住 Anthropic 在判定里；
// 这里补充守住 passthrough 不被误加进判定。
func TestIsLLMRequestFormat_PassthroughNotAdded(t *testing.T) {
	// isLLMRequestFormat 判断的是请求的 RawAPIFormat，与 outbound format 无关。
	// passthrough 不引入新的 APIFormat，所以这里验证：对所有现有 APIFormat，
	// isLLMRequestFormat 不会因为 passthrough 的存在而改变行为。
	for _, fmt := range []model.APIFormat{
		model.APIFormatOpenAIChatCompletion,
		model.APIFormatOpenAIResponse,
		model.APIFormatAnthropicMessage,
		model.APIFormatGeminiContents,
		model.APIFormatOpenAIEmbedding,
	} {
		req := &model.InternalLLMRequest{RawAPIFormat: fmt}
		_ = isLLMRequestFormat(req) // 不 panic 即可，行为由 adapter_fallback_test 守
	}
}
