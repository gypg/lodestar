// Package cloudflare 实现 Cloudflare Workers AI 的出站适配器。
//
// Cloudflare Workers AI 的 chat 接口：
//   - 请求：POST {base}/ai/run/@cf/{model}，请求体为 OpenAI 风格 messages；
//     鉴权用 Authorization: Bearer {token}。
//   - 模型名由 URL 路径承载（@cf/{publisher}/{model}），而非请求体 model 字段。
//   - 非流式响应：{"result":{"response":"..."},"success":true,...}，response 为生成文本。
//   - 流式响应（stream:true）：SSE，每个 data 为 {"response":"增量文本"}。
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	transformermodel "github.com/gypg/lodestar/internal/transformer/model"
	openaioutbound "github.com/gypg/lodestar/internal/transformer/outbound/openai"
)

// ChatOutbound 将内部通用请求转发到 Cloudflare Workers AI。
type ChatOutbound struct{}

// TransformRequest 构造 Cloudflare Workers AI 请求。
// baseURL 应配置为账户根路径，例如：
//
//	https://api.cloudflare.com/client/v4/accounts/{ACCOUNT_ID}
//
// 实际请求 URL 为 {baseURL}/ai/run/@cf/{model}。
func (o *ChatOutbound) TransformRequest(ctx context.Context, request *transformermodel.InternalLLMRequest, baseURL, key string) (*http.Request, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}

	// Cloudflare 接受 OpenAI 风格 messages，复用 OpenAI compat 的消息规范化。
	compatRequest := openaioutbound.CloneRequestForOpenAICompat(request)
	openaioutbound.SanitizeRequestForOpenAICompat(compatRequest, baseURL, false)
	for i := range compatRequest.Messages {
		if compatRequest.Messages[i].Role == "developer" {
			compatRequest.Messages[i].Role = "system"
		}
	}
	openaioutbound.NormalizeMessagesForOpenAICompat(compatRequest.Messages)

	// stream_options 是 OpenAI 特有字段，Cloudflare 不识别，清掉避免干扰。
	compatRequest.StreamOptions = nil

	body, err := json.Marshal(compatRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// 模型名由 URL 路径承载（@cf/{publisher}/{model}），请求体不应包含 model 字段。
	// InternalLLMRequest.Model 没有 omitempty，直接序列化会得到 "model":""，导致 Workers AI
	// 的 anyOf 输入格式动态检测失败（messages/prompt/input 均不匹配，oneOf 0 matches），
	// 因此需要从 body 中剔除 model 键。
	var bodyMap map[string]json.RawMessage
	if err := json.Unmarshal(body, &bodyMap); err != nil {
		return nil, fmt.Errorf("failed to decode request body: %w", err)
	}
	delete(bodyMap, "model")
	if body, err = json.Marshal(bodyMap); err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	modelName := strings.TrimPrefix(strings.TrimSpace(request.Model), "@cf/")
	if modelName == "" {
		return nil, fmt.Errorf("model is required for cloudflare channel")
	}
	upstreamURL := strings.TrimRight(baseURL, "/") + "/ai/run/@cf/" + modelName

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	return req, nil
}

// cloudflareResponse 描述 Cloudflare Workers AI 非流式响应的局部结构。
// chat 文本生成的 result.response 为生成文本（string）；其他任务类型可能是数组/对象，这里只关心 chat。
type cloudflareResponse struct {
	Result struct {
		Response string `json:"response"`
	} `json:"result"`
	Success bool `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// TransformResponse 将 Cloudflare 非流式响应转为内部通用响应。
func (o *ChatOutbound) TransformResponse(ctx context.Context, response *http.Response) (*transformermodel.InternalLLMResponse, error) {
	if response == nil {
		return nil, fmt.Errorf("response is nil")
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("response body is empty")
	}

	if response.StatusCode >= 400 {
		var errResp cloudflareResponse
		if err := json.Unmarshal(body, &errResp); err == nil && len(errResp.Errors) > 0 && errResp.Errors[0].Message != "" {
			return nil, &transformermodel.ResponseError{
				StatusCode: response.StatusCode,
				Detail:     transformermodel.ErrorDetail{Message: errResp.Errors[0].Message},
			}
		}
		return nil, fmt.Errorf("HTTP error %d: %s", response.StatusCode, string(body))
	}

	var cfResp cloudflareResponse
	if err := json.Unmarshal(body, &cfResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	content := cfResp.Result.Response
	finishReason := "stop"
	return &transformermodel.InternalLLMResponse{
		Object: "chat.completion",
		Choices: []transformermodel.Choice{{
			Index: 0,
			Message: &transformermodel.Message{
				Role:    "assistant",
				Content: transformermodel.MessageContent{Content: &content},
			},
			FinishReason: &finishReason,
		}},
	}, nil
}

// TransformStream 将 Cloudflare 流式 SSE 事件转为内部通用流式响应。
// 每个 SSE data 为 {"response":"增量文本"}；以 [DONE] 标记结束。
func (o *ChatOutbound) TransformStream(ctx context.Context, eventData []byte) (*transformermodel.InternalLLMResponse, error) {
	if bytes.HasPrefix(eventData, []byte("[DONE]")) {
		return &transformermodel.InternalLLMResponse{Object: "[DONE]"}, nil
	}

	// 错误可能内嵌在 chunk 中。
	var errCheck struct {
		Error *transformermodel.ErrorDetail `json:"error"`
	}
	if err := json.Unmarshal(eventData, &errCheck); err == nil && errCheck.Error != nil {
		return nil, &transformermodel.ResponseError{Detail: *errCheck.Error}
	}

	var chunk struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(eventData, &chunk); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stream chunk: %w", err)
	}

	return &transformermodel.InternalLLMResponse{
		Object: "chat.completion.chunk",
		Choices: []transformermodel.Choice{{
			Index: 0,
			Delta: &transformermodel.Message{
				Role:    "assistant",
				Content: transformermodel.MessageContent{Content: &chunk.Response},
			},
		}},
	}, nil
}
