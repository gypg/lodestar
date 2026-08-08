package relay

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/server/resp"
)

// upstreamErrorDetailPrefix 是 handleForwardResponse / forwardMediaRequest 生成的
// 上游错误前缀。attempt() 之后还会再包一层 "channel X adapter=Y attempt N/M: "，
// 所以抽取时按前缀定位而不是按整串匹配。
const upstreamErrorDetailPrefix = "upstream error: "

// extractUpstreamErrorDetail 从层层包裹的 relay 错误里取出上游原始的 "<状态码>: <body>"。
// 没有上游前缀（如 transformer 自身报错）时原样返回错误文本。
func extractUpstreamErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if idx := strings.Index(s, upstreamErrorDetailPrefix); idx >= 0 {
		return s[idx+len(upstreamErrorDetailPrefix):]
	}
	return s
}

// clientErrorType 为非 JSON 的上游错误体合成 OpenAI 兼容 envelope 时选择 type 字段。
func clientErrorType(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized, http.StatusForbidden:
		return "authentication_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	default:
		return "api_error"
	}
}

// writeClientTerminalError 在 ScopeNone 这类「不再重试」的终态下，把上游错误尽量原样
// 回给下游客户端，而不是吞成网关风格的 502 "upstream service unavailable"。
//
// 为什么必须原样回：下游客户端要靠上游的状态码和错误码（如 400 +
// context_length_exceeded / "prompt is too long"）判断该压缩上下文还是换模型。
// 统一伪装成 502 会让它以为是网关抖动而盲目重试。
//
// 策略：
//  1. 状态码 >= 400 且带上游 JSON body → 原样状态码 + 原样 body；
//  2. 带上游纯文本 body → 包成 OpenAI 兼容 {"error":{...}}，状态码仍用上游的；
//  3. 无可用 body → 合成最小 OpenAI 兼容错误；
//  4. 非 HTTP 错误（状态码 0/无效）→ 回退 BadGateway。
func writeClientTerminalError(c *gin.Context, statusCode int, err error) {
	if c == nil || c.Writer.Written() {
		return
	}
	if statusCode < 400 {
		resp.BadGateway(c)
		return
	}

	bodyText := strings.TrimSpace(extractUpstreamErrorDetail(err))
	if prefix := fmt.Sprintf("%d: ", statusCode); strings.HasPrefix(bodyText, prefix) {
		bodyText = strings.TrimSpace(bodyText[len(prefix):])
	}

	// "response body too large" 是 handleForwardResponse 的哨兵串，不是上游说的话，
	// 原样回给客户端会冒充成上游的错误消息。
	if bodyText != "" && bodyText != "response body too large" {
		if body := []byte(bodyText); json.Valid(body) {
			c.Data(statusCode, "application/json", body)
			c.Abort()
			return
		}
		if writeSyntheticClientError(c, statusCode, bodyText) {
			return
		}
	}

	message := http.StatusText(statusCode)
	if message == "" {
		message = "request failed"
	}
	if writeSyntheticClientError(c, statusCode, message) {
		return
	}
	resp.Error(c, statusCode, message)
}

// writeSyntheticClientError 把一段纯文本包成 OpenAI 兼容错误 envelope 写回。
// 返回 false 表示序列化失败，调用方需要走自己的兜底。
func writeSyntheticClientError(c *gin.Context, statusCode int, message string) bool {
	payload, err := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    clientErrorType(statusCode),
			"code":    "",
			"param":   "",
		},
	})
	if err != nil {
		return false
	}
	c.Data(statusCode, "application/json", payload)
	c.Abort()
	return true
}
