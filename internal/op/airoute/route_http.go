package airoute

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/utils/log"
	"golang.org/x/net/proxy"
)

// ---------- HTTP client & transport ----------

func getAIRouteHTTPTimeout() time.Duration {
	timeoutSeconds, err := setting.GetInt(model.SettingKeyAIRouteTimeoutSeconds)
	if err != nil || timeoutSeconds < 1 {
		return defaultAIRouteHTTPTimeout
	}
	return time.Duration(timeoutSeconds) * time.Second
}

func isAIRouteRetryableStatusCode(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests,
		http.StatusRequestTimeout,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func getAIRouteRetryCooldown(statusCode int) time.Duration {
	if statusCode == http.StatusTooManyRequests {
		cooldownSeconds, err := setting.GetInt(model.SettingKeyRatelimitCooldown)
		if err == nil && cooldownSeconds >= 0 {
			return time.Duration(cooldownSeconds) * time.Second
		}
		return 5 * time.Minute
	}
	return defaultAIRouteRetryBackoff
}

func formatAIRouteTimeout(timeout time.Duration) string {
	seconds := int(timeout / time.Second)
	if seconds < 1 {
		seconds = int(defaultAIRouteHTTPTimeout / time.Second)
	}
	return fmt.Sprintf("%ds", seconds)
}

func isAIRouteTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func getAIRouteHTTPClient(timeout time.Duration) (*http.Client, error) {
	proxyURL, _ := setting.GetString(model.SettingKeyProxyURL)

	baseClient, err := newAIRouteHTTPClient(strings.TrimSpace(proxyURL))
	if err != nil {
		return nil, err
	}

	cloned := *baseClient
	if timeout <= 0 {
		timeout = defaultAIRouteHTTPTimeout
	}
	cloned.Timeout = timeout
	return &cloned, nil
}

func newAIRouteHTTPClient(proxyURLStr string) (*http.Client, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default transport is not *http.Transport")
	}

	cloned := transport.Clone()
	if proxyURLStr == "" {
		cloned.Proxy = nil
		return &http.Client{Transport: cloned}, nil
	}

	proxyURL, err := url.Parse(proxyURLStr)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url: %w", err)
	}

	switch proxyURL.Scheme {
	case "http", "https":
		cloned.Proxy = http.ProxyURL(proxyURL)
	case "socks", "socks5":
		socksDialer, err := proxy.FromURL(proxyURL, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("invalid socks proxy: %w", err)
		}
		cloned.Proxy = nil
		cloned.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return socksDialer.Dial(network, addr)
		}
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
	}

	return &http.Client{Transport: cloned}, nil
}

// ---------- Upstream status error ----------

func buildAIRouteUpstreamStatusError(statusCode int, rawBody []byte) error {
	body := strings.TrimSpace(string(rawBody))
	body = summarizeAIRouteErrorBody(body)

	message := ""
	switch statusCode {
	case http.StatusTooManyRequests:
		if body == "" {
			message = "AI 分析服务触发限流，正在尝试切换其他服务"
		} else {
			message = fmt.Sprintf("AI 分析服务触发限流，正在尝试切换其他服务: %s", body)
		}
	case http.StatusGatewayTimeout:
		if body == "" {
			message = "AI 分析服务响应超时，请更换更快的 AI 模型，或减少待分析模型数量后重试"
		} else {
			message = fmt.Sprintf("AI 分析服务响应超时，请更换更快的 AI 模型，或减少待分析模型数量后重试: %s", body)
		}
	case http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable:
		if body == "" {
			message = "AI 分析服务暂时不可用，请稍后重试"
		} else {
			message = fmt.Sprintf("AI 分析服务暂时不可用，请稍后重试: %s", body)
		}
	default:
		if body == "" {
			message = fmt.Sprintf("AI 分析失败: upstream status %d", statusCode)
		} else {
			message = fmt.Sprintf("AI 分析失败: upstream status %d: %s", statusCode, body)
		}
	}

	retryable := isAIRouteRetryableStatusCode(statusCode)
	if !retryable {
		return errors.New(message)
	}

	return &aiRouteCallError{
		StatusCode: statusCode,
		Retryable:  true,
		Cooldown:   getAIRouteRetryCooldown(statusCode),
		Message:    message,
	}
}

func summarizeAIRouteErrorBody(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}

	if strings.HasPrefix(strings.ToLower(body), "<html") {
		return "upstream returned an HTML error page"
	}

	if len(body) > 200 {
		return body[:200] + "..."
	}
	return body
}

// isLikelyHTMLResponse 检测响应是否为 HTML（常见于 BaseURL 缺少 /v1 导致命中 SPA fallback）。
func isLikelyHTMLResponse(body []byte, header http.Header) bool {
	ct := header.Get("Content-Type")
	if strings.Contains(strings.ToLower(ct), "text/html") {
		return true
	}
	// Content-Type 不可靠时，检查 body 开头是否像 HTML
	trimmed := strings.TrimSpace(string(body))
	return strings.HasPrefix(trimmed, "<!DOCTYPE") || strings.HasPrefix(trimmed, "<html")
}

func joinAIRouteChatCompletionsURL(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid base url")
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(parsed.Path, "/chat/completions") {
		return parsed.String(), nil
	}
	// 与 channel.go:appendBaseURLPathByChannel 保持一致：
	// 若路径不含已知版本前缀（/v1, /v1beta, /api/v3），自动补 /v1。
	lowerPath := strings.ToLower(parsed.Path)
	hasVersionPrefix := strings.HasPrefix(lowerPath, "/v1") ||
		strings.HasPrefix(lowerPath, "/v1beta") ||
		strings.HasPrefix(lowerPath, "/api/v3")
	if !hasVersionPrefix {
		parsed.Path += "/v1"
	}
	parsed.Path += "/chat/completions"
	return parsed.String(), nil
}

// generateAIRoutesForBucketWithService 发送单次 AI 请求并解析路由结果。
func generateAIRoutesForBucketWithService(
	ctx context.Context,
	service aiRouteService,
	bucket aiRoutePromptBucket,
	targetGroupName string,
	batchIndex int,
	attempt int,
	tracker *aiRouteProgressTracker,
) ([]model.AIRouteEntry, error) {
	payload, err := json.Marshal(bucket.ModelInputs)
	if err != nil {
		return nil, fmt.Errorf("构造模型列表失败: %w", err)
	}

	requestBody := aiRouteChatCompletionRequest{
		Model: strings.TrimSpace(service.Model),
		Messages: []aiRouteChatCompletionItem{
			{Role: "system", Content: buildAIRouteSystemPrompt(bucket.PromptEndpointType)},
			{Role: "user", Content: buildAIRouteUserPrompt(bucket.PromptEndpointType, targetGroupName, payload)},
		},
		Temperature: 0.1,
		MaxTokens:   aiRouteMaxTokens,
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("构造AI请求失败: %w", err)
	}

	timeout := getAIRouteHTTPTimeout()

	httpClient, err := getAIRouteHTTPClient(timeout)
	if err != nil {
		return nil, fmt.Errorf("初始化AI请求客户端失败: %w", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpoint, err := joinAIRouteChatCompletionsURL(service.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("AI路由模型配置不完整")
	}

	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建AI请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(service.APIKey))

	resp, err := httpClient.Do(req)
	if err != nil {
		if isAIRouteTimeoutError(err) {
			return nil, &aiRouteCallError{
				Retryable: true,
				Cooldown:  getAIRouteRetryCooldown(http.StatusRequestTimeout),
				Message:   fmt.Sprintf("AI 分析超时（%s）", formatAIRouteTimeout(timeout)),
				Cause:     err,
			}
		}
		if requestCtx.Err() != nil && errors.Is(requestCtx.Err(), context.Canceled) && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("AI 分析失败: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, aiRouteResponseMaxSize))
	if err != nil {
		return nil, fmt.Errorf("读取AI响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, buildAIRouteUpstreamStatusError(resp.StatusCode, rawBody)
	}
	if tracker != nil {
		tracker.MarkBatchAIResponseReceived(batchIndex, aiRoutePromptBucket{
			PromptEndpointType: bucket.PromptEndpointType,
			GroupEndpointType:  bucket.GroupEndpointType,
			ModelInputs:        append([]aiRoutePromptModelInput(nil), bucket.ModelInputs...),
		}, service.Name, attempt)
	}

	var completionResp aiRouteChatCompletionResponse
	if err := json.Unmarshal(rawBody, &completionResp); err != nil {
		log.Warnf("ai route completion response decode failed: status=%d body=%q", resp.StatusCode, summarizeAIRouteErrorBody(string(rawBody)))
		// 检测常见配置错误：BaseURL 缺少 /v1 导致返回 HTML 首页
		if isLikelyHTMLResponse(rawBody, resp.Header) {
			return nil, fmt.Errorf("AI返回HTML而非JSON，疑似BaseURL配置错误（缺少/v1后缀）")
		}
		return nil, fmt.Errorf("AI返回结果不是合法JSON")
	}
	if len(completionResp.Choices) == 0 {
		return nil, nil
	}

	content, err := normalizeAIMessageContent(completionResp.Choices[0].Message.Content)
	if err != nil {
		return nil, err
	}

	routeResp, err := parseAIRouteResponseContent(content)
	if err != nil {
		log.Warnf("ai route content decode failed: service=%s batch=%d attempt=%d content=%q",
			service.Name, batchIndex, attempt, summarizeAIRouteErrorBody(content))
		return nil, err
	}

	normalizedRoutes := normalizeAIRouteEntries(routeResp.Routes)
	if len(normalizedRoutes) == 0 {
		return nil, nil
	}
	for i := range normalizedRoutes {
		normalizedRoutes[i].EndpointType = bucket.GroupEndpointType
	}

	return normalizedRoutes, nil
}
