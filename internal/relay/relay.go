package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/helper"
	dbmodel "github.com/gypg/lodestar/internal/model"
	ch "github.com/gypg/lodestar/internal/op/channel"
	grp "github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/op/modelmapping"
	rl "github.com/gypg/lodestar/internal/op/ratelimitstore"
	st "github.com/gypg/lodestar/internal/op/stats"
	"github.com/gypg/lodestar/internal/relay/balancer"
	"github.com/gypg/lodestar/internal/relay/condition"
	"github.com/gypg/lodestar/internal/relay/guardrail"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/transformer/inbound"
	"github.com/gypg/lodestar/internal/transformer/model"
	"github.com/gypg/lodestar/internal/transformer/outbound"
	"github.com/gypg/lodestar/internal/transformer/rewrite"
	"github.com/gypg/lodestar/internal/utils/log"
	"github.com/gypg/lodestar/internal/utils/semantic_cache"
	"github.com/tmaxmax/go-sse"
)

var errClientDisconnected = errors.New("client disconnected")
var errResponseFilterBlocked = errors.New("response filter blocked by keyword")

func resolveRequestedUpstreamModel(requestModel string) (string, bool) {
	trimmed := strings.TrimSpace(requestModel)
	if trimmed == "" {
		return "", false
	}
	prefix, upstream, ok := strings.Cut(trimmed, "/")
	if !ok {
		return "", false
	}
	if !strings.EqualFold(strings.TrimSpace(prefix), "zen") {
		return "", false
	}
	upstream = strings.TrimSpace(upstream)
	if upstream == "" {
		return "", false
	}
	return upstream, true
}

func detectZenPreferredChannelTypes(requestModel string, isEmbeddingRequest bool) map[outbound.OutboundType]struct{} {
	upstreamModel, ok := resolveRequestedUpstreamModel(requestModel)
	if !ok {
		return nil
	}
	if isEmbeddingRequest {
		return map[outbound.OutboundType]struct{}{
			outbound.OutboundTypeOpenAIEmbedding: {},
		}
	}

	lowerModel := strings.ToLower(strings.TrimSpace(upstreamModel))
	switch {
	case strings.HasPrefix(lowerModel, "claude"):
		return map[outbound.OutboundType]struct{}{
			outbound.OutboundTypeAnthropic: {},
		}
	case strings.HasPrefix(lowerModel, "gemini"), strings.HasPrefix(lowerModel, "models/gemini"), strings.HasPrefix(lowerModel, "gemma"):
		return map[outbound.OutboundType]struct{}{
			outbound.OutboundTypeGemini: {},
		}
	case strings.HasPrefix(lowerModel, "gpt-"), strings.HasPrefix(lowerModel, "o1"), strings.HasPrefix(lowerModel, "o3"), strings.HasPrefix(lowerModel, "o4"), strings.HasPrefix(lowerModel, "text-embedding"), strings.HasPrefix(lowerModel, "text-moderation"):
		return map[outbound.OutboundType]struct{}{
			outbound.OutboundTypeOpenAIChat:     {},
			outbound.OutboundTypeOpenAIResponse: {},
			outbound.OutboundTypeVolcengine:     {},
			outbound.OutboundTypeMimo:           {},
		}
	default:
		return nil
	}
}

func outboundAttemptTypes(channelType outbound.OutboundType, request *model.InternalLLMRequest, outboundFormat string) []outbound.OutboundType {
	// For LLM requests (both ChatCompletion and Responses API formats), provide
	// adapter fallback with configurable priority order.
	// When outboundFormat is "chat", prefer Chat Completions first.
	// When outboundFormat is "responses", prefer Responses API first.
	// Default (auto): prefer Chat first, then fall back to Responses.
	// The internal request/response format abstracts over both API formats, so
	// the inAdapter handles the final output conversion regardless of which
	// outbound adapter is used.
	if request != nil && isLLMRequestFormat(request) && (channelType == outbound.OutboundTypeOpenAIChat || channelType == outbound.OutboundTypeOpenAIResponse) {
		switch strings.ToLower(strings.TrimSpace(outboundFormat)) {
		case "responses":
			return []outbound.OutboundType{outbound.OutboundTypeOpenAIResponse, outbound.OutboundTypeOpenAIChat}
		default: // auto / chat
			return []outbound.OutboundType{outbound.OutboundTypeOpenAIChat, outbound.OutboundTypeOpenAIResponse}
		}
	}
	return []outbound.OutboundType{channelType}
}

func isLLMRequestFormat(request *model.InternalLLMRequest) bool {
	switch request.RawAPIFormat {
	case model.APIFormatOpenAIChatCompletion, model.APIFormatOpenAIResponse:
		return true
	default:
		return false
	}
}

func shouldTryAdapterFallback(result attemptResult, adapterIndex, attemptCount int) bool {
	if result.Success || result.Written || result.Decision.Scope == ScopeAbortAll || adapterIndex >= attemptCount-1 {
		return false
	}
	// Key-scoped failures use the same credential across adapter formats, so
	// trying another adapter only adds latency before the normal key retry path.
	if result.Decision.Scope == ScopeSameChannel {
		return false
	}
	return true
}
func isZenCandidateChannelAllowed(requestModel string, channelType outbound.OutboundType, isEmbeddingRequest bool) bool {
	preferred := detectZenPreferredChannelTypes(requestModel, isEmbeddingRequest)
	if len(preferred) == 0 {
		return true
	}
	_, ok := preferred[channelType]
	return ok
}

type perModelQuota struct {
	RPM int `json:"rpm"`
	TPM int `json:"tpm"`
}

func resolveAPIRateLimit(modelName string, c *gin.Context) (rpm int, tpm int) {
	rpm = c.GetInt("rate_limit_rpm")
	tpm = c.GetInt("rate_limit_tpm")

	perModelJSON := c.GetString("per_model_quota_json")
	if perModelJSON == "" {
		return
	}

	var quotas map[string]perModelQuota
	if err := json.Unmarshal([]byte(perModelJSON), &quotas); err != nil {
		return
	}

	if q, ok := quotas[modelName]; ok {
		if q.RPM > 0 {
			rpm = q.RPM
		}
		if q.TPM > 0 {
			tpm = q.TPM
		}
	}
	return
}

func resolveCandidateModelName(requestModel string, item dbmodel.GroupItem) string {
	if upstreamModel, ok := resolveRequestedUpstreamModel(requestModel); ok {
		if strings.TrimSpace(item.ModelName) == "" || strings.EqualFold(strings.TrimSpace(item.ModelName), "zen") {
			return upstreamModel
		}
	}
	return item.ModelName
}

// Handler 处理入站请求并转发到上游服务
func Handler(endpointType string, inboundType inbound.InboundType, c *gin.Context) {
	InflightInc()
	defer InflightDec()
	// 解析请求
	internalRequest, inAdapter, err := parseRequest(inboundType, c)
	if err != nil {
		return
	}

	// ── Guardrail input check ──────────────────────────────────────────
	// Scan the user's input against guardrail rules (banned words, PII,
	// length limits) configured via Settings → Guardrail. No-op unless the
	// guardrail toggle is on. Output-side scanning is intentionally not wired
	// here: streaming (SSE) responses can't be buffered without breaking the
	// stream, and non-stream output filtering is already covered by the
	// separate response-keyword filter (see errResponseFilterBlocked).
	if cfg := guardrail.LoadConfig(); cfg.Enabled {
		if v := guardrail.CheckInput(extractRequestText(internalRequest), cfg); v != nil {
			resp.Error(c, http.StatusBadRequest, v.Message)
			return
		}
	}
	// ── End guardrail input check ──────────────────────────────────────

	supportedModels := c.GetString("supported_models")
	if supportedModels != "" {
		supportedModelsArray := strings.Split(supportedModels, ",")
		for i := range supportedModelsArray {
			supportedModelsArray[i] = strings.TrimSpace(supportedModelsArray[i])
		}
		if !slices.Contains(supportedModelsArray, internalRequest.Model) {
			resp.Error(c, http.StatusBadRequest, "model not supported")
			return
		}
	}

	requestModel := internalRequest.Model
	apiKeyID := c.GetInt("api_key_id")

	// Rate limiting: check RPM/TPM before forwarding
	if rpm := c.GetInt("rate_limit_rpm"); rpm > 0 || c.GetInt("rate_limit_tpm") > 0 {
		effectiveRPM, effectiveTPM := resolveAPIRateLimit(requestModel, c)
		if effectiveRPM > 0 || effectiveTPM > 0 {
			allowed, remaining, retryAfter := rl.CheckRateLimit(apiKeyID, requestModel, effectiveRPM, effectiveTPM, 0)
			if !allowed {
				c.Header("X-RateLimit-Remaining", "0")
				c.Header("Retry-After", strconv.Itoa(retryAfter))
				resp.Error(c, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			if effectiveRPM > 0 {
				c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
			}
		}
	}
	var streamSession *relayStreamSession
	var streamSessionOwned bool
	var lastErr error

	if conversationID, sessionHash, ok := resolveRelayStreamSessionIdentity(endpointType, int(inboundType), apiKeyID, internalRequest); ok {
		session, created, err := acquireRelayStreamSession(conversationID, apiKeyID, sessionHash)
		if err != nil {
			statusCode := http.StatusConflict
			if !errors.Is(err, errRelayConversationBusy) {
				statusCode = http.StatusInternalServerError
			}
			resp.Error(c, statusCode, err.Error())
			return
		}
		streamSession = session
		streamSessionOwned = created
		if !created {
			req := &relayRequest{
				c:               c,
				clientCtx:       c.Request.Context(),
				internalRequest: internalRequest,
				apiKeyID:        apiKeyID,
				requestModel:    requestModel,
				streamSession:   streamSession,
			}
			serveRelayStreamSession(c, req)
			return
		}
		defer func() {
			if streamSession == nil || !streamSessionOwned || streamSession.IsDone() {
				return
			}
			if lastErr == nil {
				lastErr = errors.New("relay stream ended without a terminal result")
			}
			streamSession.Finish(lastErr)
		}()
	}

	operationCtx, cancel := newRelayOperationContext()
	defer cancel()

	// 获取通道分组
	group, err := grp.GroupGetEnabledMapByEndpoint(endpointType, requestModel, operationCtx)
	if err != nil {
		lastErr = err
		log.Infof("model not found: model=%s endpoint_type=%s reason=%v", requestModel, endpointType, err)
		resp.Error(c, http.StatusNotFound, "model not found")
		return
	}

	// 检查条件路由：条件不匹配则跳过
	if group.Condition != "" {
		condCtx := condition.RequestContext{
			Model:    requestModel,
			APIKeyID: apiKeyID,
			Hour:     time.Now().UTC().Hour(),
		}
		if match, condErr := condition.Evaluate(group.Condition, condCtx); condErr != nil || !match {
			lastErr = fmt.Errorf("condition not met for group %s", group.Name)
			resp.Error(c, http.StatusNotFound, "model not found")
			return
		}
	}

	// 创建迭代器（策略排序 + 粘性优先）
	iter := balancer.NewIterator(group, apiKeyID, requestModel, parseExcludedChannels(c.GetString("excluded_channels")))
	if iter.Len() == 0 {
		lastErr = errors.New("no available channel")
		resp.Error(c, http.StatusServiceUnavailable, "no available channel")
		return
	}

	// 根据分组端点提供方做请求兼容改写
	internalRequest = rewriteConversationRequestByProvider(group, internalRequest)

	// 初始化 Metrics
	clientIP := c.ClientIP()
	metrics := NewRelayMetrics(apiKeyID, requestModel, endpointType, group.EndpointType, clientIP, internalRequest)

	// 请求级上下文
	req := &relayRequest{
		c:                 c,
		clientCtx:         c.Request.Context(),
		operationCtx:      operationCtx,
		inAdapter:         inAdapter,
		internalRequest:   internalRequest,
		metrics:           metrics,
		apiKeyID:          apiKeyID,
		requestModel:      requestModel,
		groupEndpointType: group.EndpointType,
		iter:              iter,
		streamSession:     streamSession,
		retryCache:        newRetryRequestCache(),
	}

	var inflightKey string
	var inflightEnabled bool
	if endpointFamily := semanticCacheEndpointFamily(endpointType, inboundType); endpointFamily != "" {
		served, payload, cacheErr := maybeServeSemanticCacheHit(c, req, endpointFamily)
		if cacheErr != nil {
			log.Warnf("semantic cache lookup failed: %v", cacheErr)
		}
		if served {
			log.Infof("semantic cache hit: model=%s endpoint=%s", requestModel, endpointFamily)
			if normalizedPayload := semanticCacheHitPayload(payload, internalRequest); len(normalizedPayload) > 0 {
				if internalResponse, parseErr := buildSemanticCacheHitInternalResponse(internalRequest, normalizedPayload); parseErr == nil {
					metrics.SetInternalResponse(internalResponse, internalRequest.Model)
				}
			}
			metrics.Save(true, nil, nil)
			return
		}
		if _, text, ok, _ := getSemanticCacheLookupInput(req, endpointFamily); ok {
			inflightKey, inflightEnabled = requestSingleflightKey(apiKeyID, endpointFamily, internalRequest.Model, text, internalRequest)
		}
	}

	maxKeyRetriesPerRoute := getMaxAttemptsPerCandidate()
	maxRouteRetries := getMaxRouteRetries()
	ratelimitCooldown := getRatelimitCooldown()
	maxTotalAttempts := getMaxTotalAttempts()

	if inflightEnabled {
		result, sfErr, shared := relayInflightGroup.Do(inflightKey, func() (any, error) {
			return executeRelay(req, group, requestModel, maxKeyRetriesPerRoute, maxRouteRetries, ratelimitCooldown, maxTotalAttempts)
		})
		if sfErr == nil {
			if outcome, ok := result.(*inflightRelayResult); ok && outcome != nil {
				if shared {
					if outcome.namespace != "" && outcome.requestText != "" {
						cfg, ok := semanticCacheRuntimeConfig()
						if ok {
							embedding, _, embErr := lookupSemanticEmbeddingWithCache(req.operationCtx, req, cfg, outcome.namespace, outcome.requestText)
							if embErr == nil {
								if payload, found := semantic_cache.Lookup(outcome.namespace, embedding); found {
									normalizedPayload := semanticCacheHitPayload(payload, internalRequest)
									c.Data(http.StatusOK, "application/json", normalizedPayload)
									if internalResponse, parseErr := buildSemanticCacheHitInternalResponse(internalRequest, normalizedPayload); parseErr == nil {
										metrics.SetInternalResponse(internalResponse, outcome.actualModel)
									}
									metrics.Save(true, nil, nil)
									return
								}
							}
						}
					}
					if resp := cloneInternalResponse(outcome.internalResp); resp != nil {
						metrics.SetInternalResponse(resp, outcome.actualModel)
					}
					metrics.Save(true, nil, outcome.attempts)
					return
				}
				return
			}
		}
	}

	if _, err := executeRelay(req, group, requestModel, maxKeyRetriesPerRoute, maxRouteRetries, ratelimitCooldown, maxTotalAttempts); err != nil {
		return
	}
	return
}

// attempt 统一管理一次通道尝试的完整生命周期
func (ra *relayAttempt) attempt() attemptResult {
	span := ra.iter.StartAttempt(ra.channel.ID, ra.usedKey.ID, ra.channel.Name, ra.internalRequest.Model)
	span.SetAdapterType(ra.adapterType.String())

	// 转发请求
	statusCode, fwdErr := ra.forward()

	// Client disconnected — do not record failure stats, circuit-breaker
	// counts, or retry hints. The client chose to stop, not the channel.
	if errors.Is(fwdErr, errClientDisconnected) {
		span.End(dbmodel.AttemptFailed, statusCode, "client disconnected")
		return attemptResult{
			Success:  false,
			Written:  ra.c.Writer.Written(),
			Err:      fwdErr,
			Decision: RetryDecision{Scope: ScopeAbortAll, Reason: "client disconnected", Code: statusCode},
		}
	}

	// 输出结果关键词拦截 — 不重试，不记录渠道失败统计
	if errors.Is(fwdErr, errResponseFilterBlocked) {
		span.End(dbmodel.AttemptFailed, statusCode, "response filter blocked")
		return attemptResult{
			Success:  false,
			Written:  ra.c.Writer.Written(),
			Err:      fwdErr,
			Decision: RetryDecision{Scope: ScopeAbortAll, Reason: "response filter blocked by keyword", Code: statusCode},
		}
	}

	// 检查是否已写入流式响应
	written := ra.c.Writer.Written()

	// 使用错误分类驱动决策
	decision := ClassifyRelayError(statusCode, fwdErr, written)

	// 更新 channel key 状态
	ra.usedKey.StatusCode = statusCode
	ra.usedKey.LastUseTimeStamp = time.Now().Unix()

	// Per-model cooldown: record 429 for this specific (key, model) pair
	if statusCode == 429 {
		modelName := ra.internalRequest.Model
		if modelName != "" {
			dbmodel.RecordKeyModelCooldown(ra.usedKey.ID, modelName)
		}
	}

	if decision.Scope == ScopeNone && !decision.IsError {
		// ====== 成功 ======
		ra.collectResponse()
		ra.collectAndStoreStreamResponse()
		ra.usedKey.TotalCost += ra.metrics.Stats.InputCost + ra.metrics.Stats.OutputCost
		ch.KeyUpdate(ra.usedKey)

		span.End(dbmodel.AttemptSuccess, statusCode, "")

		// Channel 维度统计
		updateChannelSuccessStats(ra.channel.ID, span.Duration().Milliseconds(), ra.metrics.Stats)
		st.ModelRecord(ra.channel.ID, ra.internalRequest.Model, dbmodel.StatsMetrics{
			WaitTime:       span.Duration().Milliseconds(),
			InputToken:     ra.metrics.Stats.InputToken,
			OutputToken:    ra.metrics.Stats.OutputToken,
			InputCost:      ra.metrics.Stats.InputCost,
			OutputCost:     ra.metrics.Stats.OutputCost,
			RequestSuccess: 1,
		})

		// 熔断器：记录成功
		balancer.RecordSuccess(ra.channel.ID, ra.usedKey.ID, ra.internalRequest.Model)
		// Auto策略：记录成功
		balancer.RecordAutoSuccess(ra.channel.ID, ra.internalRequest.Model)
		// Auto策略：记录延迟（毫秒）
		balancer.RecordAutoLatency(ra.channel.ID, ra.internalRequest.Model, span.Duration().Milliseconds())
		// 会话保持：更新粘性记录
		balancer.SetSticky(ra.apiKeyID, ra.requestModel, ra.channel.ID, ra.usedKey.ID)

		return attemptResult{Success: true, Decision: decision}
	}

	// ====== 失败 ======
	ch.KeyUpdate(ra.usedKey)

	// 构造日志消息
	msg := decision.String()
	if ra.tryTotal > 1 {
		msg = fmt.Sprintf("attempt %d/%d: %s", ra.tryIndex, ra.tryTotal, msg)
	}
	span.End(dbmodel.AttemptFailed, statusCode, msg)

	// Channel 维度统计
	st.ChannelUpdate(ra.channel.ID, dbmodel.StatsMetrics{
		WaitTime:      span.Duration().Milliseconds(),
		RequestFailed: 1,
	})
	st.ModelRecord(ra.channel.ID, ra.internalRequest.Model, dbmodel.StatsMetrics{
		WaitTime:      span.Duration().Milliseconds(),
		RequestFailed: 1,
	})

	// 熔断器和 Auto 策略的记录由调用方（adapter fallback loop）控制，
	// 避免在 adapter 降级场景（如 Responses→Chat）中误触发熔断。

	if written {
		ra.collectResponse()
	}

	// 记录决策日志
	if decision.IsError {
		logRelayErrorfByContext(fwdErr, "[%s] channel %s adapter=%s attempt %d/%d failed: %s (decision: %s)",
			ra.internalRequest.RawAPIFormat, ra.channel.Name, ra.adapterType, ra.tryIndex, ra.tryTotal, fwdErr, decision.Scope.String())
	}

	return attemptResult{
		Success:  false,
		Written:  written,
		Err:      fmt.Errorf("channel %s adapter=%s attempt %d/%d: %v", ra.channel.Name, ra.adapterType, ra.tryIndex, ra.tryTotal, fwdErr),
		Decision: decision,
	}
}

// parseRequest 解析并验证入站请求
func parseRequest(inboundType inbound.InboundType, c *gin.Context) (*model.InternalLLMRequest, model.Inbound, error) {
	body, err := readLimitedRequestBody(c, getMaxRelayJSONBodyBytes())
	if err != nil {
		resp.Error(c, relayRequestBodyErrorStatus(err), err.Error())
		return nil, nil, err
	}

	inAdapter := inbound.Get(inboundType)
	internalRequest, err := inAdapter.TransformRequest(c.Request.Context(), body)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return nil, nil, err
	}

	// Pass through the original query parameters
	internalRequest.Query = c.Request.URL.Query()

	if err := internalRequest.Validate(); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return nil, nil, err
	}

	populateRelayRequestSessionFields(c, internalRequest, body)

	return internalRequest, inAdapter, nil
}

// forward 转发请求到上游服务
func (ra *relayAttempt) forward() (int, error) {
	ctx := ra.operationCtx

	requestForOutbound, effectiveRewrite, err := prepareInternalRequestForOutbound(ra.channel, ra.internalRequest, ra.groupEndpointType)
	if err != nil {
		log.Warnf("failed to prepare outbound request data: %v", err)
		return 0, fmt.Errorf("failed to prepare outbound request data: %w", err)
	}

	// 构建出站请求
	outboundRequest, err := ra.outAdapter.TransformRequest(
		ctx,
		requestForOutbound,
		ra.channel.GetNormalizedBaseUrl(),
		ra.usedKey.ChannelKey,
	)
	if err != nil {
		log.Warnf("failed to create request: %v", err)
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	// 复制请求头
	ra.copyHeaders(outboundRequest, effectiveRewrite)

	// 发送请求
	response, err := ra.sendRequest(outboundRequest)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer response.Body.Close()

	// 检查响应状态
	statusCode, err := ra.handleForwardResponse(response)
	if err != nil {
		return statusCode, err
	}

	// 处理响应
	if ra.internalRequest.Stream != nil && *ra.internalRequest.Stream {
		if err := ra.handleStreamResponse(ctx, response); err != nil {
			return 0, err
		}
		return response.StatusCode, nil
	}
	if err := ra.handleResponse(ctx, response); err != nil {
		return 0, err
	}
	return response.StatusCode, nil
}

func (ra *relayAttempt) handleForwardResponse(response *http.Response) (int, error) {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response.StatusCode, nil
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes+1))
	if err != nil {
		return response.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}
	if len(body) > maxErrorBodyBytes {
		return response.StatusCode, fmt.Errorf("upstream error: %d: response body too large", response.StatusCode)
	}
	// Log the raw upstream error body verbatim for debugging.
	channelName := ""
	modelName := ""
	if ra.channel != nil {
		channelName = ra.channel.Name
	}
	if ra.relayRequest != nil {
		modelName = ra.internalRequest.Model
	}
	log.Infof("upstream error body [%s] %s %d: %s",
		channelName, modelName, response.StatusCode, string(body))
	return response.StatusCode, fmt.Errorf("upstream error: %d: %s", response.StatusCode, string(body))
}

// copyHeaders 复制请求头，过滤 hop-by-hop 头
func (ra *relayAttempt) copyHeaders(outboundRequest *http.Request, effectiveRewrite *rewrite.EffectiveConfig) {
	for key, values := range ra.c.Request.Header {
		if hopByHopHeaders[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			outboundRequest.Header.Set(key, value)
		}
	}
	if len(ra.channel.CustomHeader) > 0 {
		for _, header := range ra.channel.CustomHeader {
			outboundRequest.Header.Set(header.HeaderKey, header.HeaderValue)
		}
	}

	if effectiveRewrite != nil && len(effectiveRewrite.ExtraHeaders) > 0 {
		for key, value := range effectiveRewrite.ExtraHeaders {
			outboundRequest.Header.Set(key, value)
		}
	}
}

// sendRequest 发送 HTTP 请求
func (ra *relayAttempt) sendRequest(req *http.Request) (*http.Response, error) {
	httpClient, err := helper.ChannelHttpClient(ra.channel)
	if err != nil {
		log.Warnf("failed to get http client: %v", err)
		return nil, err
	}

	response, err := httpClient.Do(req)
	if err != nil {
		logRelayErrorfByContext(err, "failed to send request: %v", err)
		return nil, err
	}

	return response, nil
}

// handleStreamResponse 处理流式响应
func (ra *relayAttempt) handleStreamResponse(ctx context.Context, response *http.Response) (retErr error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 安全网：确保 stream session 在所有退出路径上都被关闭，
	// 避免外层 defer 产生 "relay stream ended without a terminal result"。
	defer func() {
		if ra.streamSession != nil && !ra.streamSession.IsDone() {
			ra.streamSession.Finish(retErr)
		}
	}()

	if ct := response.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "text/event-stream") {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return fmt.Errorf("upstream returned non-SSE content-type %q for stream request: %s", ct, string(body))
	}

	// 设置 SSE 响应头
	ra.c.Header("Content-Type", "text/event-stream")
	ra.c.Header("Cache-Control", "no-cache")
	ra.c.Header("Connection", "keep-alive")
	ra.c.Header("X-Accel-Buffering", "no")
	if ra.internalRequest.ConversationID != "" {
		ra.c.Header("X-Conversation-ID", ra.internalRequest.ConversationID)
	}

	firstToken := true
	clientDone := ra.clientCtx.Done()
	clientDisconnected := false
	clientDisconnectLogged := false
	markClientDisconnected := func() {
		if clientDisconnected {
			return
		}
		clientDisconnected = true
		clientDone = nil
	}
	logClientDisconnected := func() {
		if !clientDisconnected || clientDisconnectLogged {
			return
		}
		clientDisconnectLogged = true
		log.Warnf(clientDisconnectedLogMessage)
	}

	type sseReadResult struct {
		data string
		err  error
	}
	results := make(chan sseReadResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("SSE reader panic recovered: %v", r)
			}
		}()
		defer close(results)
		readCfg := &sse.ReadConfig{MaxEventSize: maxSSEEventSize}
		for ev, err := range sse.Read(response.Body, readCfg) {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err != nil {
				select {
				case results <- sseReadResult{err: err}:
				case <-ctx.Done():
				}
				return
			}
			select {
			case results <- sseReadResult{data: ev.Data}:
			case <-ctx.Done():
				return
			}
		}
	}()

	var firstTokenTimer *time.Timer
	var firstTokenC <-chan time.Time
	if firstToken && ra.firstTokenTimeOutSec > 0 {
		firstTokenTimer = time.NewTimer(time.Duration(ra.firstTokenTimeOutSec) * time.Second)
		firstTokenC = firstTokenTimer.C
		defer func() {
			if firstTokenTimer != nil {
				firstTokenTimer.Stop()
			}
		}()
	}

	for {
		select {
		case <-clientDone:
			if ra.streamSession == nil {
				log.Infof("client disconnected, stopping stream")
				return errClientDisconnected
			}
			markClientDisconnected()
		case <-firstTokenC:
			logClientDisconnected()
			log.Warnf("first token timeout (%ds), switching channel", ra.firstTokenTimeOutSec)
			if err := response.Body.Close(); err != nil {
				log.Warnf("failed to close response body on first token timeout: %v", err)
			}
			return fmt.Errorf("first token timeout (%ds)", ra.firstTokenTimeOutSec)
		case r, ok := <-results:
			if !ok {
				// results channel 被 SSE reader goroutine 关闭。
				// 需要区分正常结束（上游 EOF）和异常中断（ctx 取消/超时）。
				if ctxErr := ctx.Err(); ctxErr != nil {
					if ra.streamSession != nil {
						ra.streamSession.Finish(ctxErr)
					}
					return fmt.Errorf("stream interrupted: %w", ctxErr)
				}
				logClientDisconnected()
				if ra.streamSession != nil {
					ra.streamSession.Finish(nil)
				}
				log.Infof("stream end")
				return nil
			}
			if r.err != nil {
				logClientDisconnected()
				logRelayErrorfByContext(r.err, "failed to read event: %v", r.err)
				return fmt.Errorf("failed to read stream event: %w", r.err)
			}

			data, err := ra.transformStreamData(ctx, r.data)
			if err != nil {
				if errors.Is(err, errResponseFilterBlocked) {
					// 关键词拦截：发送错误 SSE 事件并终止流
					filterCfg := ra.getResponseFilterConfig()
					if ra.streamSession != nil {
						errPayload, _ := json.Marshal(map[string]any{
							"error": map[string]any{
								"message": filterCfg.ErrorMessage,
								"type":    "content_filter",
								"code":    "content_blocked",
							},
						})
						ra.streamSession.AddPayload(errPayload)
						ra.streamSession.Finish(nil)
					} else if !clientDisconnected {
						writeSSEErrorEvent(ra.c.Writer, filterCfg.ErrorMessage)
						ra.c.Writer.Flush()
					}
					if closeErr := response.Body.Close(); closeErr != nil {
						log.Warnf("failed to close response body on response filter block: %v", closeErr)
					}
					return fmt.Errorf("response filter blocked streaming output")
				}
				continue
			}
			if len(data) == 0 {
				continue
			}
			if firstToken {
				ra.metrics.SetFirstTokenTime(time.Now())
				firstToken = false
				if firstTokenTimer != nil {
					if !firstTokenTimer.Stop() {
						select {
						case <-firstTokenTimer.C:
						default:
						}
					}
					firstTokenTimer = nil
					firstTokenC = nil
				}
			}

			if ra.streamSession != nil {
				sessionEvents := ra.streamSession.AddPayload(data)
				if clientDisconnected {
					logClientDisconnected()
					continue
				}
				for _, event := range sessionEvents {
					if _, err := ra.c.Writer.Write(formatRelaySSEEvent(event.Sequence, event.Payload)); err != nil {
						markClientDisconnected()
						logClientDisconnected()
						break
					}
					ra.c.Writer.Flush()
				}
				continue
			}

			if clientDisconnected {
				logClientDisconnected()
				continue
			}
			if _, err := ra.c.Writer.Write(data); err != nil {
				markClientDisconnected()
				logClientDisconnected()
				continue
			}
			ra.c.Writer.Flush()
		}
	}
}

// transformStreamData 转换流式数据
func (ra *relayAttempt) transformStreamData(ctx context.Context, data string) ([]byte, error) {
	internalStream, err := ra.outAdapter.TransformStream(ctx, []byte(data))
	if err != nil {
		logRelayErrorfByContext(err, "failed to transform stream: %v", err)
		return nil, err
	}
	if internalStream == nil {
		return nil, nil
	}

	// 输出结果关键词拦截（流式）
	filterCfg := ra.getResponseFilterConfig()
	if blocked, keyword := applyResponseFilter(internalStream, filterCfg); blocked {
		log.Infof("response filter blocked streaming chunk with keyword %q", keyword)
		return nil, errResponseFilterBlocked
	}

	inStream, err := ra.inAdapter.TransformStream(ctx, internalStream)
	if err != nil {
		logRelayErrorfByContext(err, "failed to transform stream: %v", err)
		return nil, err
	}

	return inStream, nil
}

// handleResponse 处理非流式响应
func (ra *relayAttempt) handleResponse(ctx context.Context, response *http.Response) error {
	internalResponse, err := ra.outAdapter.TransformResponse(ctx, response)
	if err != nil {
		logRelayErrorfByContext(err, "failed to transform response: %v", err)
		return fmt.Errorf("failed to transform outbound response: %w", err)
	}

	// 输出结果关键词拦截
	filterCfg := ra.getResponseFilterConfig()
	if blocked, keyword := applyResponseFilter(internalResponse, filterCfg); blocked {
		log.Infof("response filter blocked keyword %q", keyword)
		errMsg := filterCfg.ErrorMessage
		errorResp := map[string]any{
			"error": map[string]any{
				"message": errMsg,
				"type":    "content_filter",
				"code":    "content_blocked",
			},
		}
		data, _ := json.Marshal(errorResp)
		ra.c.Data(http.StatusOK, "application/json", data)
		return nil
	}

	applyReasoningExhaustedHeader(ra.c, internalResponse)

	inResponse, err := ra.inAdapter.TransformResponse(ctx, internalResponse)
	if err != nil {
		logRelayErrorfByContext(err, "failed to transform response: %v", err)
		return fmt.Errorf("failed to transform inbound response: %w", err)
	}

	storeSemanticCacheResponse(ctx, ra.internalRequest, inResponse)

	ra.c.Data(http.StatusOK, "application/json", inResponse)
	return nil
}

func applyReasoningExhaustedHeader(c *gin.Context, resp *model.InternalLLMResponse) {
	if c == nil || !isReasoningExhaustedResponse(resp) {
		return
	}
	c.Header("X-Reasoning-Exhausted", "true")
}

func isReasoningExhaustedResponse(resp *model.InternalLLMResponse) bool {
	if resp == nil || resp.Usage == nil || len(resp.Choices) == 0 {
		return false
	}
	if resp.Usage.CompletionTokensDetails == nil || resp.Usage.CompletionTokensDetails.ReasoningTokens <= 0 {
		return false
	}
	for _, choice := range resp.Choices {
		if choice.Message == nil {
			continue
		}
		if choice.Message.Content.Content != nil && strings.TrimSpace(*choice.Message.Content.Content) != "" {
			return false
		}
		if len(choice.Message.Content.MultipleContent) > 0 || len(choice.Message.ToolCalls) > 0 {
			return false
		}
	}
	return true
}

// collectResponse 收集响应信息
func (ra *relayAttempt) collectResponse() {
	internalResponse, err := ra.inAdapter.GetInternalResponse(ra.operationCtx)
	if err != nil || internalResponse == nil {
		return
	}

	ra.metrics.SetInternalResponse(internalResponse, ra.internalRequest.Model)
}

// collectAndStoreStreamResponse stores the already-aggregated stream response
// in the semantic cache (success path only). It reuses the InternalResponse
// previously collected by collectResponse() to avoid a second call to
// GetInternalResponse(), which would return nil after stream chunks are consumed.
func (ra *relayAttempt) collectAndStoreStreamResponse() {
	if ra.internalRequest.Stream == nil || !*ra.internalRequest.Stream {
		return
	}
	internalResponse := ra.metrics.InternalResponse
	if internalResponse == nil {
		return
	}
	if responseJSON, err := json.Marshal(internalResponse); err == nil {
		storeSemanticCacheResponse(ra.operationCtx, ra.internalRequest, responseJSON)
	}
}

func rewriteConversationRequestByProvider(group dbmodel.Group, req *model.InternalLLMRequest) *model.InternalLLMRequest {
	if req == nil {
		return req
	}
	endpointType := dbmodel.NormalizeEndpointType(group.EndpointType)
	provider := strings.ToLower(strings.TrimSpace(group.EndpointProvider))
	if provider == "" || provider == "auto" {
		return req
	}
	if endpointType == dbmodel.EndpointTypeAll {
		return req
	}
	if endpointType != dbmodel.EndpointTypeChat {
		return req
	}

	// Provider rewrite config: which non-standard message fields to strip.
	// Some providers (e.g. standard OpenAI) reject reasoning_content / reasoning_signature fields.
	type providerRewrite struct {
		stripReasoning          bool
		stripReasoningSignature bool
	}
	providers := map[string]providerRewrite{
		"openai":   {stripReasoning: true, stripReasoningSignature: true},
		"deepseek": {stripReasoning: true, stripReasoningSignature: false},
		"mimo":     {stripReasoning: false, stripReasoningSignature: true},
	}
	cfg, ok := providers[provider]
	if !ok {
		return req
	}

	cloned := *req
	if len(req.Messages) > 0 {
		cloned.Messages = make([]model.Message, len(req.Messages))
		for i, msg := range req.Messages {
			cloned.Messages[i] = msg
			if cfg.stripReasoning {
				cloned.Messages[i].Reasoning = nil
			}
			if cfg.stripReasoningSignature {
				cloned.Messages[i].ReasoningSignature = nil
			}
		}
	}
	return &cloned
}

// isClientDisconnected reports whether the client has disconnected.
func isClientDisconnected(clientCtx context.Context) bool {
	select {
	case <-clientCtx.Done():
		return true
	default:
		return false
	}
}

// handleClientDisconnect is a shared handler for client-disconnect checks
// inside the executeRelay retry loops. It saves metrics and returns the error.
func handleClientDisconnect(req *relayRequest, allAttempts []dbmodel.ChannelAttempt) error {
	log.Infof("client disconnected, stopping relay retry loop")
	req.metrics.Save(false, errClientDisconnected, allAttempts)
	return errClientDisconnected
}

func executeRelay(req *relayRequest, group dbmodel.Group, requestModel string, maxKeyRetriesPerRoute int, maxRouteRetries int, ratelimitCooldown int, maxTotalAttempts int) (*inflightRelayResult, error) {
	// 应用全局模型映射表（Phase 7）
	requestModel = modelmapping.Resolve(req.operationCtx, requestModel, group.ID)

	// N-08: save the original Model so we can restore it after each channel
	// attempt. Without this, setting req.internalRequest.Model = resolvedModelName
	// mutates the shared request object, causing subsequent channels to see a
	// stale model.
	originalModel := req.internalRequest.Model

	var relayResult *inflightRelayResult
	var relayErr error
	var resultSaved bool

	retryWithChannels(group, requestModel, req.apiKeyID, req.c.GetString("excluded_channels"),
		maxKeyRetriesPerRoute, maxRouteRetries, ratelimitCooldown, maxTotalAttempts,
		retryCallbacks{
			Ctx: req.operationCtx,
			CheckContext: func() error {
				if isClientDisconnected(req.clientCtx) {
					return handleClientDisconnect(req, nil)
				}
				if err := req.operationCtx.Err(); err != nil {
					logRelayErrorfByContext(err, "relay operation ended: %v", err)
					return err
				}
				return nil
			},
			FilterChannel: func(item dbmodel.GroupItem, channel *dbmodel.Channel, iter *balancer.Iterator) bool {
				attemptTypes := outboundAttemptTypes(channel.Type, req.internalRequest, group.OutboundFormat)
				if len(attemptTypes) == 0 || outbound.Get(attemptTypes[0]) == nil {
					iter.Skip(channel.ID, 0, channel.Name, fmt.Sprintf("unsupported channel type: %d", channel.Type))
					return true
				}
				if req.internalRequest.IsEmbeddingRequest() && !outbound.IsEmbeddingChannelType(channel.Type) {
					iter.Skip(channel.ID, 0, channel.Name, "channel type not compatible with embedding request")
					return true
				}
				if req.internalRequest.IsChatRequest() && !outbound.IsChatChannelType(channel.Type) {
					iter.Skip(channel.ID, 0, channel.Name, "channel type not compatible with chat request")
					return true
				}
				if !isZenCandidateChannelAllowed(requestModel, channel.Type, req.internalRequest.IsEmbeddingRequest()) {
					iter.Skip(channel.ID, 0, channel.Name, "channel type not preferred for zen model prefix")
					return true
				}
				return false
			},
			ResolveModel: func(item dbmodel.GroupItem) string {
				mappedModel := modelmapping.Resolve(req.operationCtx, requestModel, group.ID)
				return resolveCandidateModelName(mappedModel, item)
			},
			LogAttempt: func(channel *dbmodel.Channel, resolvedModel string, round retryRoundInfo) {
				log.Infof("request model %s, mode: %d, channel: %s (%s) model: %s key_id: %d (route R%d, key %d/%d, sticky=%t)",
					requestModel, group.Mode, channel.Name, channel.Type, resolvedModel, round.UsedKey.ID,
					round.RouteRound, round.KeyRound, round.MaxKeyRetries, round.Iter.IsSticky())
			},
			ForwardRequest: func(channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, resolvedModelName string, round retryRoundInfo) retryForwardResult {
				req.internalRequest.Model = resolvedModelName
				defer func() { req.internalRequest.Model = originalModel }()

				req.iter = round.Iter
				attemptTypes := outboundAttemptTypes(channel.Type, req.internalRequest, group.OutboundFormat)

				var result attemptResult
				for adapterIndex, attemptType := range attemptTypes {
					outAdapter := outbound.Get(attemptType)
					if outAdapter == nil {
						continue
					}
					ra := &relayAttempt{
						relayRequest:         req,
						outAdapter:           outAdapter,
						adapterType:          attemptType,
						channel:              channel,
						usedKey:              usedKey,
						firstTokenTimeOutSec: group.FirstTokenTimeOut,
						tryIndex:             round.KeyRound,
						tryTotal:             round.MaxKeyRetries,
					}

					result = ra.attempt()
					if result.Success {
						if adapterIndex > 0 {
							log.Infof("[%s] adapter fallback succeeded on channel %s: %s -> %s",
								req.internalRequest.RawAPIFormat, channel.Name, attemptTypes[0], attemptType)
						}
						break
					}
					if !shouldTryAdapterFallback(result, adapterIndex, len(attemptTypes)) {
						break
					}
					log.Infof("[%s] %s adapter failed on channel %s, falling back to %s: %v",
						req.internalRequest.RawAPIFormat, attemptType, channel.Name, attemptTypes[adapterIndex+1], result.Err)
				}

				return retryForwardResult{Decision: result.Decision, Err: result.Err}
			},
			OnSuccess: func(channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, resolvedModelName string, round retryRoundInfo) {
				currentAttempts := append([]dbmodel.ChannelAttempt(nil), round.Iter.Attempts()...)
				namespace, requestText, _ := semanticCacheStoreMetadata(req.internalRequest)
				req.metrics.Save(true, nil, currentAttempts)
				relayResult = newInflightRelayResult(cloneInternalResponse(req.metrics.InternalResponse), req.internalRequest.Model, currentAttempts, namespace, requestText)
				resultSaved = true
			},
			OnFailure: func(channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, resolvedModel string) {
				balancer.RecordFailure(channel.ID, usedKey.ID, resolvedModel)
				balancer.RecordAutoFailure(channel.ID, resolvedModel)
			},
			OnFinalFailure: func(channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, resolvedModel string, round retryRoundInfo, fwdResult retryForwardResult) bool {
				// Client disconnected — stop all retries immediately.
				if errors.Is(fwdResult.Err, errClientDisconnected) {
					currentAttempts := append([]dbmodel.ChannelAttempt(nil), round.Iter.Attempts()...)
					req.metrics.Save(false, fwdResult.Err, currentAttempts)
					relayErr = fwdResult.Err
					resultSaved = true
					return true
				}
				// ScopeNone: direct failure, send 502
				if fwdResult.Decision.Scope == ScopeNone {
					currentAttempts := append([]dbmodel.ChannelAttempt(nil), round.Iter.Attempts()...)
					req.metrics.Save(false, fwdResult.Err, currentAttempts)
					resp.BadGateway(req.c)
					relayErr = fwdResult.Err
					resultSaved = true
					return true
				}
				return false
			},
			OnExhausted: func(allAttempts []dbmodel.ChannelAttempt, lastErr error) {
				if resultSaved {
					return
				}
				req.metrics.Save(false, lastErr, allAttempts)
				log.Warnf("[%s] all channels exhausted: model=%s, attempts=%d, last_error=%v",
					req.internalRequest.RawAPIFormat, requestModel, len(allAttempts), lastErr)
				if lastErr != nil {
					resp.Error(req.c, http.StatusBadGateway, fmt.Sprintf("all channels failed: %v", lastErr))
				} else {
					resp.Error(req.c, http.StatusBadGateway, "all channels failed")
				}
				relayErr = lastErr
				if relayErr == nil {
					relayErr = errors.New("all channels failed")
				}
			},
			UseFailureHints:          true,
			UsePrepareCandidateForRetry: true,
		},
	)

	if resultSaved && relayResult != nil {
		return relayResult, nil
	}
	return nil, relayErr
}
