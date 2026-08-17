package outbound

import (
	"github.com/gypg/lodestar/internal/transformer/model"
	"github.com/gypg/lodestar/internal/transformer/outbound/anthropic"
	"github.com/gypg/lodestar/internal/transformer/outbound/cloudflare"
	"github.com/gypg/lodestar/internal/transformer/outbound/gemini"
	"github.com/gypg/lodestar/internal/transformer/outbound/mimo"
	"github.com/gypg/lodestar/internal/transformer/outbound/openai"
	"github.com/gypg/lodestar/internal/transformer/outbound/passthrough"
	"github.com/gypg/lodestar/internal/transformer/outbound/volcengine"
)

type OutboundType int

const (
	OutboundTypeOpenAIChat OutboundType = iota
	OutboundTypeOpenAIResponse
	OutboundTypeAnthropic
	OutboundTypeGemini
	OutboundTypeVolcengine
	OutboundTypeOpenAIEmbedding
	OutboundTypeMimo
	OutboundTypeCloudflare
	// OutboundTypePassthrough 是客户端显式选择的"原样透传"出站格式：
	// 保留客户端原始请求体（只重写顶层 model），可选保留原始请求路径。
	// 它不进 isLLMRequestFormat 判定（R-6 不回归），也不参与 chat↔responses
	// 的 adapter fallback——只有分组 OutboundFormat="passthrough" 时才路由到此。
	OutboundTypePassthrough
)

func (t OutboundType) String() string {
	switch t {
	case OutboundTypeOpenAIChat:
		return "chat"
	case OutboundTypeOpenAIResponse:
		return "response"
	case OutboundTypeAnthropic:
		return "anthropic"
	case OutboundTypeGemini:
		return "gemini"
	case OutboundTypeVolcengine:
		return "volcengine"
	case OutboundTypeOpenAIEmbedding:
		return "embedding"
	case OutboundTypeMimo:
		return "mimo"
	case OutboundTypeCloudflare:
		return "cloudflare"
	case OutboundTypePassthrough:
		return "passthrough"
	default:
		return "unknown"
	}
}

// EmbeddingChannelTypes 定义支持 embedding 请求的 channel 类型集合。
// passthrough 原样透传，与 embedding 请求兼容。
var EmbeddingChannelTypes = map[OutboundType]bool{
	OutboundTypeOpenAIEmbedding: true,
	OutboundTypePassthrough:     true,
}

// ChatChannelTypes 定义支持 chat 请求的 channel 类型集合。
// passthrough 在此集合里：它原样透传客户端请求体，对 chat/embedding 不做格式
// 限制（IsChatChannelType / IsEmbeddingChannelType 的用途是过滤不兼容的渠道
// 类型，passthrough 与任意请求类型都兼容）。
var ChatChannelTypes = map[OutboundType]bool{
	OutboundTypeOpenAIChat:     true,
	OutboundTypeOpenAIResponse: true,
	OutboundTypeAnthropic:      true,
	OutboundTypeGemini:         true,
	OutboundTypeVolcengine:     true,
	OutboundTypeMimo:           true,
	OutboundTypeCloudflare:     true,
	OutboundTypePassthrough:    true,
}

// IsEmbeddingChannelType 判断 channel 类型是否支持 embedding 请求
func IsEmbeddingChannelType(channelType OutboundType) bool {
	return EmbeddingChannelTypes[channelType]
}

// IsChatChannelType 判断 channel 类型是否支持 chat 请求
func IsChatChannelType(channelType OutboundType) bool {
	return ChatChannelTypes[channelType]
}

var outboundFactories = map[OutboundType]func() model.Outbound{
	OutboundTypeOpenAIChat:      func() model.Outbound { return &openai.ChatOutbound{} },
	OutboundTypeOpenAIResponse:  func() model.Outbound { return &openai.ResponseOutbound{} },
	OutboundTypeOpenAIEmbedding: func() model.Outbound { return &openai.EmbeddingOutbound{} },
	OutboundTypeAnthropic:       func() model.Outbound { return &anthropic.MessageOutbound{} },
	OutboundTypeGemini:          func() model.Outbound { return &gemini.MessagesOutbound{} },
	OutboundTypeVolcengine:      func() model.Outbound { return &volcengine.ResponseOutbound{} },
	OutboundTypeMimo:            func() model.Outbound { return &mimo.ChatOutbound{} },
	OutboundTypeCloudflare:      func() model.Outbound { return &cloudflare.ChatOutbound{} },
	OutboundTypePassthrough:     func() model.Outbound { return &passthrough.Outbound{} },
}

func Get(outboundType OutboundType) model.Outbound {
	if factory, ok := outboundFactories[outboundType]; ok {
		return factory()
	}
	return nil
}
