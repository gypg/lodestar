package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/apikey"
	billing "github.com/gypg/lodestar/internal/op/billing"
	"github.com/gypg/lodestar/internal/op/cacheusage"
	"github.com/gypg/lodestar/internal/op/ratelimitstore"
	"github.com/gypg/lodestar/internal/op/relaylog"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/stats"
	"github.com/gypg/lodestar/internal/price"
	"github.com/gypg/lodestar/internal/relay/redact"
	transformerModel "github.com/gypg/lodestar/internal/transformer/model"
	"github.com/gypg/lodestar/internal/utils/log"
	"github.com/gypg/lodestar/internal/utils/telemetry"
)

const relayLogTextFieldMaxBytes = 4096

const relayLogJSONFieldMaxBytes = 16384

// RelayMetrics 负责最终的日志收集与持久化
type RelayMetrics struct {
	APIKeyID     int
	RequestModel string
	EndpointType string
	ClientIP     string
	StartTime    time.Time

	// 首 Token 时间
	FirstTokenTime time.Time

	// 请求和响应内容
	InternalRequest  *transformerModel.InternalLLMRequest
	InternalResponse *transformerModel.InternalLLMResponse

	// 统计指标
	ActualModel string
	Stats       model.StatsMetrics

	// TPM 配额（per-minute token quota for this request's model, 0 = unconfigured）。
	// 预检时只按 tokenCount=1 做准入；请求完成后按实际用量扣减，见 Save。
	TPM int

	// saved 标记本次请求是否已收尾。客户端断连路径会先由 handleClientDisconnect
	// 调用 Save，随后 retryWithChannels 的 OnExhausted 再调一次（relay.go:1139 →
	// retry_shared.go:108/122/162），同一请求因此会走两遍收尾。写日志/统计重复尚可
	// 容忍，但 TPM 扣减与计费重复会真实多扣，故此处统一挡住第二次。
	// 一次请求只在一个 goroutine 内收尾，无需加锁。
	saved bool
}

func NewRelayMetrics(apiKeyID int, requestModel string, requestedEndpointType string, matchedGroupEndpointType string, clientIP string, req *transformerModel.InternalLLMRequest) *RelayMetrics {
	return &RelayMetrics{
		APIKeyID:        apiKeyID,
		RequestModel:    requestModel,
		EndpointType:    resolveRelayLogEndpointType(requestedEndpointType, matchedGroupEndpointType),
		ClientIP:        clientIP,
		StartTime:       time.Now(),
		InternalRequest: req,
	}
}

func (m *RelayMetrics) SetFirstTokenTime(t time.Time) {
	m.FirstTokenTime = t
}

func (m *RelayMetrics) SetInternalResponse(resp *transformerModel.InternalLLMResponse, actualModel string) {
	m.InternalResponse = resp
	m.ActualModel = actualModel

	if resp == nil || resp.Usage == nil {
		return
	}

	usage := resp.Usage
	m.Stats.InputToken = usage.PromptTokens
	m.Stats.OutputToken = usage.CompletionTokens

	modelPrice := price.GetLLMPrice(actualModel)
	if modelPrice == nil {
		return
	}
	if usage.PromptTokensDetails == nil {
		usage.PromptTokensDetails = &transformerModel.PromptTokensDetails{
			CachedTokens: 0,
		}
	}
	if usage.AnthropicUsage {
		m.Stats.InputCost = (float64(usage.PromptTokensDetails.CachedTokens)*modelPrice.CacheRead +
			float64(usage.PromptTokens)*modelPrice.Input +
			float64(usage.CacheCreationInputTokens)*modelPrice.CacheWrite) * 1e-6
	} else {
		m.Stats.InputCost = (float64(usage.PromptTokensDetails.CachedTokens)*modelPrice.CacheRead + float64(usage.PromptTokens-usage.PromptTokensDetails.CachedTokens)*modelPrice.Input) * 1e-6
	}
	m.Stats.OutputCost = float64(usage.CompletionTokens) * modelPrice.Output * 1e-6
}

// SetTPM records the effective per-minute token quota for this request.
// It is set immediately after the metrics are constructed, from the rate-limit
// resolution that already ran for the pre-check (relay.go). A value <= 0 means
// TPM is unconfigured for this model and the bucket must be left untouched.
func (m *RelayMetrics) SetTPM(tpm int) {
	m.TPM = tpm
}

// consumeRateLimitTokens deducts this request's actual token usage from the
// TPM bucket after the request has finished. It is the post-check counterpart
// to CheckRateLimit's pre-check admission (which only deducts 1 token when the
// real usage is still unknown). ConsumeTokens no-ops when TPM <= 0 or the
// actual usage is 0, so a request that recorded no tokens costs nothing.
func (m *RelayMetrics) consumeRateLimitTokens() {
	if m.TPM <= 0 {
		return
	}
	ratelimitstore.ConsumeTokens(m.APIKeyID, m.RequestModel, m.TPM, int(m.Stats.InputToken)+int(m.Stats.OutputToken))
}

// isUnbillableFake200Response 是计费层专用的假 200 判定：零载荷（Choices 与
// EmbeddingData 全空，见 isFake200Response）**且**没有记录任何真实 token 用量。
// 带 Usage 的零载荷响应不是假 200——那是流式聚合后只剩 usage 的合法形态，上游
// 确实消耗了 token，必须照常计费（TestSaveNonZeroCostOnFailureIsStillCharged
// 钉死了这条）。relay 层（handleResponse）用的是纯载荷谓词 isFake200Response，
// 两者职责不同：relay 层决定"要不要交付/重试"，计费层决定"有没有可扣费的东西"。
func isUnbillableFake200Response(resp *transformerModel.InternalLLMResponse) bool {
	if !isFake200Response(resp) {
		return false
	}
	return resp.Usage == nil || (resp.Usage.PromptTokens <= 0 && resp.Usage.CompletionTokens <= 0)
}

func (m *RelayMetrics) Save(success bool, err error, attempts []model.ChannelAttempt) {
	// 同一请求只收尾一次，重复调用直接返回（见 saved 字段说明）。
	if m.saved {
		return
	}
	m.saved = true

	// ── 假 200 计费层守卫（纵深防御第二层，独立于 relay 层）──
	// relay 层已把假 200 拦为失败（handleResponse → errFake200Response），但计费
	// 不变式不能依赖单一判定点：若一个假 200 响应以"成功"身份到达计费层，这里
	// 独立识别并按失败入账——不扣费、记 RequestFailed 而非 RequestSuccess（否则
	// 会压住错误率告警）。该判定与 retry_empty_output 设置无关。
	chargeable := true
	if isUnbillableFake200Response(m.InternalResponse) {
		chargeable = false
		if success {
			log.Warnf("fake 200 response reached billing layer as success: model=%s, api_key_id=%d — demoting to failure, skipping charge",
				m.RequestModel, m.APIKeyID)
			success = false
			if err == nil {
				err = errFake200Response
			}
		}
	} else if !success && m.InternalResponse == nil {
		// 整条链没记录到任何内部响应的失败（典型：所有渠道都回假 200 后耗尽）：
		// 没有可交付的内容，不收表达式计费的固定费，与 media 路径 dd8f26d 的
		// relayErr 守卫对齐。记录了真实用量的失败仍照常扣费（上游确实消耗了
		// token，见 TestSaveNonZeroCostOnFailureIsStillCharged）。
		chargeable = false
	}

	ctx, cancel := newRelayPersistenceContext()
	defer cancel()

	duration := time.Since(m.StartTime)
	totalAttempts := len(attempts)
	forwardedAttempts := countForwardedAttempts(attempts)

	useTimeMs := duration.Milliseconds()

	globalStats := model.StatsMetrics{
		WaitTime:    useTimeMs,
		InputToken:  m.Stats.InputToken,
		OutputToken: m.Stats.OutputToken,
		InputCost:   m.Stats.InputCost,
		OutputCost:  m.Stats.OutputCost,
	}
	if success {
		globalStats.RequestSuccess = 1
	} else {
		globalStats.RequestFailed = 1
	}

	// Latency histogram bucket assignment
	switch {
	case useTimeMs < 100:
		globalStats.HistogramLt100 = 1
	case useTimeMs < 500:
		globalStats.Histogram100to500 = 1
	case useTimeMs < 1000:
		globalStats.Histogram500to1k = 1
	case useTimeMs < 5000:
		globalStats.Histogram1kto5k = 1
	default:
		globalStats.HistogramGt5k = 1
	}

	// FTUT: first token time
	if !m.FirstTokenTime.IsZero() {
		ftutMs := m.FirstTokenTime.Sub(m.StartTime).Milliseconds()
		globalStats.FtutAvg = ftutMs
		globalStats.FtutP50 = ftutMs
		globalStats.FtutP95 = ftutMs
		globalStats.FtutP99 = ftutMs
	}

	// Latency percentiles from telemetry ring buffer (approximate)
	snap := telemetry.Global().Snapshot()
	globalStats.LatencyP50 = int64(snap.AvgLatencyMs)
	globalStats.LatencyP95 = int64(snap.P95LatencyMs)
	globalStats.LatencyP99 = int64(snap.P99LatencyMs)

	channelID, channelName := finalChannel(attempts)
	stats.TotalUpdate(globalStats)
	stats.HourlyUpdate(globalStats)
	if statsErr := stats.DailyUpdate(ctx, globalStats); statsErr != nil {
		log.Warnf("failed to update daily stats: %v", statsErr)
	}
	stats.APIKeyUpdate(m.APIKeyID, globalStats)
	// Lodestar commercial: deduct this request's USD cost from the key owner's balance (no-op unless commercial_mode on).
	// chargeable=false 的两类场景见函数开头的假 200 计费层守卫。
	if chargeable {
		billing.ChargeKeyWithExpr(m.APIKeyID, m.RequestModel, int(m.Stats.InputToken), int(m.Stats.OutputToken), globalStats.InputCost+globalStats.OutputCost, ctx)
	}

	// Post-check TPM deduction: deduct the request's actual token usage from the
	// TPM bucket. The pre-check in relay.go only admitted 1 token (usage unknown);
	// this is where the real usage is charged. No-op when TPM unconfigured or no
	// tokens were recorded.
	m.consumeRateLimitTokens()

	log.Infof("relay complete: model=%s, channel=%d(%s), success=%t, duration=%dms, input_token=%d, output_token=%d, input_cost=%f, output_cost=%f, total_cost=%f, attempts=%d, forwarded_attempts=%d",
		m.RequestModel, channelID, channelName, success, duration.Milliseconds(),
		m.Stats.InputToken, m.Stats.OutputToken,
		m.Stats.InputCost, m.Stats.OutputCost, m.Stats.InputCost+m.Stats.OutputCost,
		totalAttempts, forwardedAttempts)

	actualModel := m.ActualModel
	if actualModel == "" {
		actualModel = m.RequestModel
	}
	m.saveLog(ctx, err, duration, attempts, channelID, channelName)
	op.StatsSiteModelHourlyRecordAttempts(attempts, actualModel)
	telemetry.Global().RecordRequest(duration.Milliseconds(), success)
}

func finalChannel(attempts []model.ChannelAttempt) (int, string) {
	var fallbackID int
	var fallbackName string
	var lastID int
	var lastName string
	for i := len(attempts) - 1; i >= 0; i-- {
		a := attempts[i]
		if fallbackID == 0 && a.ChannelID != 0 {
			fallbackID = a.ChannelID
			fallbackName = a.ChannelName
		}
		if a.Status == model.AttemptSuccess {
			return a.ChannelID, a.ChannelName
		}
		if a.Status == model.AttemptFailed && lastID == 0 {
			lastID = a.ChannelID
			lastName = a.ChannelName
		}
	}
	if lastID != 0 {
		return lastID, lastName
	}
	return fallbackID, fallbackName
}

func countForwardedAttempts(attempts []model.ChannelAttempt) int {
	count := 0
	for _, attempt := range attempts {
		if attempt.Status == model.AttemptSkipped || attempt.Status == model.AttemptCircuitBreak {
			continue
		}
		count++
	}
	return count
}

func (m *RelayMetrics) saveLog(ctx context.Context, err error, duration time.Duration, attempts []model.ChannelAttempt, channelID int, channelName string) {
	actualModel := m.ActualModel
	if actualModel == "" {
		actualModel = m.RequestModel
	}

	relayLog := model.RelayLog{
		Time:             m.StartTime.Unix(),
		RequestModelName: m.RequestModel,
		RequestAPIKeyID:  m.APIKeyID,
		ClientIP:         m.ClientIP,
		EndpointType:     m.EndpointType,
		ChannelName:      channelName,
		ChannelId:        channelID,
		ActualModelName:  actualModel,
		UseTime:          int(duration.Milliseconds()),
		Attempts:         attempts,
		TotalAttempts:    len(attempts),
	}

	if apiKey, getErr := apikey.Get(m.APIKeyID, ctx); getErr == nil {
		relayLog.RequestAPIKeyName = apiKey.Name
	}

	// 首字时间
	if !m.FirstTokenTime.IsZero() {
		relayLog.Ftut = int(m.FirstTokenTime.Sub(m.StartTime).Milliseconds())
	}

	// Usage
	if m.InternalResponse != nil && m.InternalResponse.Usage != nil {
		relayLog.InputTokens = int(m.InternalResponse.Usage.PromptTokens)
		relayLog.OutputTokens = int(m.InternalResponse.Usage.CompletionTokens)
		relayLog.Cost = m.Stats.InputCost + m.Stats.OutputCost
	}

	// 请求内容
	if m.InternalRequest != nil {
		if reqJSON, jsonErr := json.Marshal(m.filterRequestForLog(m.InternalRequest)); jsonErr == nil {
			relayLog.RequestContent = string(reqJSON)
		}
	}

	// 响应内容
	if m.InternalResponse != nil {
		respForLog := m.filterResponseForLog(m.InternalResponse)
		if respJSON, jsonErr := json.Marshal(respForLog); jsonErr == nil {
			if m.InternalResponse.Usage != nil && m.InternalResponse.Usage.AnthropicUsage {
				respStr := string(respJSON)
				old := `"usage":{`
				insert := fmt.Sprintf(`"usage":{"cache_creation_input_tokens":%d,`, m.InternalResponse.Usage.CacheCreationInputTokens)
				respJSON = []byte(strings.Replace(respStr, old, insert, 1))
			}
			if isSemanticCacheHitRequest(m.InternalRequest) {
				relayLog.SemanticCacheHit = true
				if relayLog.ChannelName == "" {
					relayLog.ChannelName = "Semantic Cache"
				}
				respJSON = semanticCacheHitPayload(respJSON, m.InternalRequest)
			}
			relayLog.ResponseContent = string(respJSON)
		}
	}

	// PII 脱敏：在日志写入前对 request/response content 进行脱敏
	if piiEnabled, _ := setting.GetBool(model.SettingKeyPIIRedactionEnabled); piiEnabled {
		if relayLog.RequestContent != "" {
			relayLog.RequestContent = redact.RedactPII(relayLog.RequestContent)
		}
		if relayLog.ResponseContent != "" {
			relayLog.ResponseContent = redact.RedactPII(relayLog.ResponseContent)
		}
	}

	if !relayLog.SemanticCacheHit {
		relayLog.CacheReadTokens = opRelayLogCacheReadTokens(relayLog.ResponseContent)
	}

	// 错误信息
	if err != nil {
		relayLog.Error = err.Error()
	}

	// relayLog.Attempts 已在上面设好；尝试明细（issue #67 的 relay_log_attempts）
	// 由 relayLogFlushToDB 在父日志落库后同批写入，此处不再单独写 ——
	// RelayLogAdd 只进内存缓存，在这里写明细会让它先于父日志落库（R-9）。
	if logErr := relaylog.RelayLogAdd(ctx, &relayLog); logErr != nil {
		log.Warnf("failed to save relay log: %v", logErr)
	}
}

func opRelayLogCacheReadTokens(responseContent string) int {
	signals, ok := cacheusage.ParseProviderPromptCacheUsageSignals(responseContent)
	if !ok || signals.SemanticCacheHit || signals.CachedTokens <= 0 {
		return 0
	}
	return int(signals.CachedTokens)
}

func filterMessageForLog(msg *transformerModel.Message) *transformerModel.Message {
	if msg == nil {
		return nil
	}
	c := *msg
	if c.Content.Content != nil {
		content := truncateRelayLogString(*c.Content.Content, relayLogTextFieldMaxBytes)
		c.Content.Content = &content
	}
	if c.ReasoningContent != nil {
		reasoningContent := truncateRelayLogString(*c.ReasoningContent, relayLogTextFieldMaxBytes)
		c.ReasoningContent = &reasoningContent
	}
	if c.Reasoning != nil {
		reasoning := truncateRelayLogString(*c.Reasoning, relayLogTextFieldMaxBytes)
		c.Reasoning = &reasoning
	}
	if len(c.ToolCalls) > 0 {
		c.ToolCalls = make([]transformerModel.ToolCall, len(msg.ToolCalls))
		for i, toolCall := range msg.ToolCalls {
			c.ToolCalls[i] = toolCall
			c.ToolCalls[i].Function.Arguments = truncateRelayLogString(toolCall.Function.Arguments, relayLogTextFieldMaxBytes)
		}
	}
	c.Images = nil
	if len(c.Content.MultipleContent) > 0 {
		parts := make([]transformerModel.MessageContentPart, 0, len(c.Content.MultipleContent))
		for _, p := range c.Content.MultipleContent {
			switch {
			case p.Type == "text" && p.Text != nil:
				text := truncateRelayLogString(*p.Text, relayLogTextFieldMaxBytes)
				parts = append(parts, transformerModel.MessageContentPart{
					Type: p.Type,
					Text: &text,
				})
			case p.Type == "image_url" && p.ImageURL != nil:
				parts = append(parts, transformerModel.MessageContentPart{
					Type:     "image_url",
					ImageURL: &transformerModel.ImageURL{URL: "[image data omitted for storage]", Detail: p.ImageURL.Detail},
				})
			case p.Type == "input_audio" && p.Audio != nil:
				audio := *p.Audio
				audio.Data = "[audio data omitted for storage]"
				parts = append(parts, transformerModel.MessageContentPart{
					Type:  p.Type,
					Audio: &audio,
				})
			case p.Type == "file" && p.File != nil && p.File.FileData != "":
				file := *p.File
				file.FileData = "[file data omitted for storage]"
				parts = append(parts, transformerModel.MessageContentPart{
					Type: p.Type,
					File: &file,
				})
			default:
				parts = append(parts, p)
			}
		}
		c.Content = transformerModel.MessageContent{Content: c.Content.Content, MultipleContent: parts}
	}
	if c.Audio != nil && c.Audio.Data != "" {
		a := *c.Audio
		a.Data = "[audio data omitted for storage]"
		c.Audio = &a
	}
	return &c
}

func truncateRelayLogString(value string, maxBytes int) string {
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}

	truncated := value[:maxBytes]
	for !utf8.ValidString(truncated) && len(truncated) > 0 {
		truncated = truncated[:len(truncated)-1]
	}
	return fmt.Sprintf("%s...[truncated %d bytes for storage]", truncated, len(value)-len(truncated))
}

func filterEmbeddingInputForLog(input *transformerModel.EmbeddingInput) *transformerModel.EmbeddingInput {
	if input == nil {
		return nil
	}
	cloned := *input
	if len(input.Multiple) > 0 {
		cloned.Multiple = make([]string, len(input.Multiple))
		copy(cloned.Multiple, input.Multiple)
	}
	for i, value := range cloned.Multiple {
		cloned.Multiple[i] = truncateRelayLogString(value, relayLogTextFieldMaxBytes)
	}
	if cloned.Single != nil {
		truncated := truncateRelayLogString(*cloned.Single, relayLogTextFieldMaxBytes)
		cloned.Single = &truncated
	}
	return &cloned
}

func filterToolsForLog(tools []transformerModel.Tool) []transformerModel.Tool {
	if len(tools) == 0 {
		return nil
	}
	filtered := make([]transformerModel.Tool, len(tools))
	for i, tool := range tools {
		filtered[i] = tool
		filtered[i].Function.Description = truncateRelayLogString(tool.Function.Description, relayLogTextFieldMaxBytes)
		if len(tool.Function.Parameters) > relayLogJSONFieldMaxBytes {
			filtered[i].Function.Parameters = json.RawMessage(strconv.Quote(truncateRelayLogString(string(tool.Function.Parameters), relayLogJSONFieldMaxBytes)))
		}
	}
	return filtered
}

func (m *RelayMetrics) filterRequestForLog(req *transformerModel.InternalLLMRequest) *transformerModel.InternalLLMRequest {
	if req == nil {
		return nil
	}

	filtered := *req
	if len(req.Messages) > 0 {
		filtered.Messages = make([]transformerModel.Message, len(req.Messages))
		for i, msg := range req.Messages {
			filteredMsg := filterMessageForLog(&msg)
			if filteredMsg != nil {
				filtered.Messages[i] = *filteredMsg
			}
		}
	}
	filtered.EmbeddingInput = filterEmbeddingInputForLog(req.EmbeddingInput)
	filtered.Tools = filterToolsForLog(req.Tools)
	filtered.ExtraBody = nil
	filtered.RawRequest = nil
	return &filtered
}

// filterResponseForLog 创建响应的浅拷贝，过滤掉 images、MultipleContent 中的图片数据和 Audio.Data 以减少存储压力
func (m *RelayMetrics) filterResponseForLog(resp *transformerModel.InternalLLMResponse) *transformerModel.InternalLLMResponse {
	if resp == nil {
		return nil
	}

	filtered := *resp
	filtered.Choices = make([]transformerModel.Choice, len(resp.Choices))
	for i, choice := range resp.Choices {
		filtered.Choices[i] = choice
		filtered.Choices[i].Message = filterMessageForLog(choice.Message)
		filtered.Choices[i].Delta = filterMessageForLog(choice.Delta)
	}
	return &filtered
}
