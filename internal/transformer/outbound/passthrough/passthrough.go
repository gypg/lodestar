// Package passthrough 实现原样透传出站适配器。
//
// passthrough 保留客户端原始请求体（InternalLLMRequest.RawRequest），仅重写顶层
// model 字段为分组解析后的上游模型名；可选保留客户端原始请求路径（RawPath）。
// 不做任何格式转换或回退——对 omp / 自定义客户端等需要原样路径的场景有用。
//
// 响应仍走标准 relay 链路（outbound.TransformResponse → InternalLLMResponse →
// fake200 校验 → inbound.TransformResponse），因此计费层的假 200 纵深防御
// （metrics.go:isUnbillableFake200Response）对 passthrough 路径同样生效。
package passthrough

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	transformermodel "github.com/gypg/lodestar/internal/transformer/model"
)

// Outbound 原样透传出站适配器。
//
// 与其它出站 adapter 不同，passthrough 不构造结构化请求体，而是直接复用客户端
// 原始 JSON 字节，只重写顶层 model 字段。这避免了对客户端自定义字段（inbound
// 结构未建模的字段）的丢失。
type Outbound struct{}

// doneMarker 是 SSE 流结束标记。用 \x5b/\x5d 转义方括号，避免源码文本里的
// 方括号被工具链误处理。
const doneMarker = "\x5bDONE\x5d"

// rewriteTopLevelModel 把原始请求体解析为 map，只重写顶层 model 字段为上游模型名，
// 其余字段原样保留（字节序不保证，但 JSON 语义不变）。
//
// 若原始 body 不是合法 JSON 对象（如数组、字符串），返回错误——passthrough 只支持
// 对象型请求体。
func rewriteTopLevelModel(rawBody []byte, upstreamModel string) ([]byte, error) {
	var bodyMap map[string]json.RawMessage
	if err := json.Unmarshal(rawBody, &bodyMap); err != nil {
		return nil, fmt.Errorf("passthrough: request body is not a JSON object: %w", err)
	}

	modelBytes, err := json.Marshal(upstreamModel)
	if err != nil {
		return nil, fmt.Errorf("passthrough: failed to marshal upstream model: %w", err)
	}
	bodyMap["model"] = modelBytes

	return json.Marshal(bodyMap)
}

// resolveUpstreamURL 决定上游 URL。
//
// 若 request.RawPath 非空（客户端原始路径已填充），保留原始路径：baseURL + RawPath。
// 否则回退到 baseURL（调用方应配置完整上游地址）。
//
// 保留原始路径模式用于 omp / 自定义客户端等需要原样路径转发的场景。
func resolveUpstreamURL(baseURL, rawPath string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if path := strings.TrimSpace(rawPath); path != "" {
		return trimmed + path
	}
	return trimmed
}

// TransformRequest 构造原样透传请求。
//
// 请求体：取 request.RawRequest（客户端原始字节），只重写顶层 model 为
// request.Model（分组解析后的上游模型名）。若 RawRequest 为空（非标准入口），
// 回退到序列化 request 本身。
//
// URL：baseURL + request.RawPath（保留原始路径），或 baseURL（无原始路径时）。
//
// 鉴权：Authorization: Bearer {key}，与既有 chat adapter 一致。
func (o *Outbound) TransformRequest(ctx context.Context, request *transformermodel.InternalLLMRequest, baseURL, key string) (*http.Request, error) {
	if request == nil {
		return nil, fmt.Errorf("passthrough: request is nil")
	}

	upstreamModel := strings.TrimSpace(request.Model)
	if upstreamModel == "" {
		return nil, fmt.Errorf("passthrough: model is required")
	}

	rawBody := request.RawRequest
	if len(rawBody) == 0 {
		// 非标准入口未填充 RawRequest：回退到结构化序列化，保底不崩。
		var err error
		rawBody, err = json.Marshal(request)
		if err != nil {
			return nil, fmt.Errorf("passthrough: failed to marshal fallback request: %w", err)
		}
	}

	body, err := rewriteTopLevelModel(rawBody, upstreamModel)
	if err != nil {
		return nil, err
	}

	upstreamURL := resolveUpstreamURL(baseURL, request.RawPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("passthrough: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	return req, nil
}

// TransformResponse 将上游响应转为内部通用响应。
//
// 不做格式转换——直接把上游响应体 unmarshal 进 InternalLLMResponse。这样：
//   - 上游返回合法 OpenAI/Anthropic 结构 → Choices/Usage 正常填充，走标准计费。
//   - 上游 200 + 空响应体 / 非法 JSON → Choices 与 EmbeddingData 均空，被
//     relay 层 isFake200Response 拦为失败，计费层 isUnbillableFake200Response
//     兜底不扣费。这条纵深防御对 passthrough 路径同样生效。
func (o *Outbound) TransformResponse(ctx context.Context, response *http.Response) (*transformermodel.InternalLLMResponse, error) {
	if response == nil {
		return nil, fmt.Errorf("passthrough: response is nil")
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("passthrough: failed to read response body: %w", err)
	}

	if response.StatusCode >= 400 {
		var errResp struct {
			Error transformermodel.ErrorDetail `json:"error"`
		}
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			return nil, &transformermodel.ResponseError{
				StatusCode: response.StatusCode,
				Detail:     errResp.Error,
			}
		}
		return nil, fmt.Errorf("passthrough: HTTP error %d: %s", response.StatusCode, string(body))
	}

	// 空响应体或无法解析为合法 JSON → 返回空 InternalLLMResponse，交由 fake200
	// 守卫拦截。不在此处返回 error，否则 fake200 路径无法触发（handleResponse
	// 在 TransformResponse 报错时直接返回，跳过 isFake200Response 判定）。
	var resp transformermodel.InternalLLMResponse
	if len(body) > 0 {
		_ = json.Unmarshal(body, &resp)
	}
	return &resp, nil
}

// TransformStream 将上游流式 chunk 原样转为内部通用流式响应。
//
// 与 TransformResponse 同理：直接 unmarshal 进 InternalLLMResponse，让流式
// 聚合 / usage 提取 / 计费链路正常工作。错误体（{"error":...}）单独识别。
func (o *Outbound) TransformStream(ctx context.Context, eventData []byte) (*transformermodel.InternalLLMResponse, error) {
	if bytes.HasPrefix(eventData, []byte(doneMarker)) {
		return &transformermodel.InternalLLMResponse{Object: doneMarker}, nil
	}

	var errCheck struct {
		Error *transformermodel.ErrorDetail `json:"error"`
	}
	if err := json.Unmarshal(eventData, &errCheck); err == nil && errCheck.Error != nil {
		return nil, &transformermodel.ResponseError{Detail: *errCheck.Error}
	}

	var resp transformermodel.InternalLLMResponse
	if err := json.Unmarshal(eventData, &resp); err != nil {
		return nil, fmt.Errorf("passthrough: failed to unmarshal stream chunk: %w", err)
	}
	return &resp, nil
}
