package relay

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/helper"
	dbmodel "github.com/gypg/lodestar/internal/model"
	opMain "github.com/gypg/lodestar/internal/op"
	ak "github.com/gypg/lodestar/internal/op/apikey"
	billing "github.com/gypg/lodestar/internal/op/billing"
	ch "github.com/gypg/lodestar/internal/op/channel"
	grp "github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/op/relaylog"
	st "github.com/gypg/lodestar/internal/op/stats"
	"github.com/gypg/lodestar/internal/pkg/billingexpr"
	"github.com/gypg/lodestar/internal/relay/balancer"
	"github.com/gypg/lodestar/internal/relay/condition"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/utils/log"
	"github.com/gypg/lodestar/internal/utils/telemetry"
)

func mediaEndpointTypeToGroupEndpointType(endpointType MediaEndpointType) string {
	switch endpointType {
	case MediaEndpointImageGeneration:
		return dbmodel.EndpointTypeImageGeneration
	case MediaEndpointImageEdit:
		return dbmodel.EndpointTypeImageGeneration
	case MediaEndpointImageVariation:
		return dbmodel.EndpointTypeImageGeneration
	case MediaEndpointAudioSpeech:
		return dbmodel.EndpointTypeAudioSpeech
	case MediaEndpointAudioTranscription:
		return dbmodel.EndpointTypeAudioTranscription
	case MediaEndpointVideoGeneration:
		return dbmodel.EndpointTypeVideoGeneration
	case MediaEndpointMusicGeneration:
		return dbmodel.EndpointTypeMusicGeneration
	case MediaEndpointSearch:
		return dbmodel.EndpointTypeSearch
	case MediaEndpointRerank:
		return dbmodel.EndpointTypeRerank
	case MediaEndpointModeration:
		return dbmodel.EndpointTypeModerations
	default:
		return dbmodel.EndpointTypeAll
	}
}

// MediaHandler handles non-LLLM media/utility endpoints by forwarding requests
// directly to upstream channels, reusing the existing channel/group/balancer/circuit-breaker
// infrastructure without going through the Inbound/Outbound transformer pipeline.
func MediaHandler(endpointType MediaEndpointType, c *gin.Context) {
	InflightInc()
	defer InflightDec()
	cfg := getMediaEndpointConfig(endpointType)

	// 1. Extract model name from the request
	requestModel, bodyBytes, streamRequested, err := extractModelFromRequest(c, cfg)
	if err != nil {
		resp.Error(c, relayRequestBodyErrorStatus(err), err.Error())
		return
	}
	// Multipart endpoints carry no JSON body (bodyBytes==nil), so billing
	// expressions cannot read param('size'/'n'/'quality') from it (BUG-004b).
	// Serialize the parsed form fields into a JSON object once, here, so the
	// cost path and the relay log both see the fields.
	bodyBytes = extractBodyForBilling(c, cfg, bodyBytes)
	if cfg.MultipartInput && c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	if requestModel == "" {
		resp.Error(c, http.StatusBadRequest, "model is required")
		return
	}

	apiKeyID := c.GetInt("api_key_id")
	clientIP := c.ClientIP()
	startTime := time.Now()

	// 2. Resolve channel group
	groupEndpointType := mediaEndpointTypeToGroupEndpointType(endpointType)
	group, err := grp.GroupGetEnabledMapByEndpoint(groupEndpointType, requestModel, c.Request.Context())
	if err != nil {
		log.Infof("model not found in media relay: model=%s endpoint_type=%s reason=%v", requestModel, groupEndpointType, err)
		resp.Error(c, http.StatusNotFound, "model not found")
		return
	}
	logEndpointType := resolveRelayLogEndpointType(groupEndpointType, group.EndpointType)

	// Narrow * group items: a * group may contain items that only support
	// specific endpoint types (e.g., chat-only items don't support image_generation).
	// Filter items to only those likely compatible with the requested endpoint.
	if group.EndpointType == dbmodel.EndpointTypeAll && groupEndpointType != dbmodel.EndpointTypeAll {
		narrowed := narrowGroupItemsForEndpoint(group, groupEndpointType)
		if len(narrowed.Items) == 0 {
			log.Infof("no endpoint-matching items in '*' group: model=%s endpoint_type=%s", requestModel, groupEndpointType)
			resp.Error(c, http.StatusNotFound, "model not found")
			return
		}
		group = narrowed
	}

	// 检查条件路由：条件不匹配则跳过（与 LLM relay 保持一致）
	if group.Condition != "" {
		condCtx := buildConditionContext(c, requestModel, apiKeyID)
		if match, condErr := condition.Evaluate(group.Condition, condCtx); condErr != nil || !match {
			log.Infof("media relay: condition not met for group %s", group.Name)
			resp.Error(c, http.StatusNotFound, "model not found")
			return
		}
	}

	// 3. Create load balancer iterator
	iter := balancer.NewIterator(group, apiKeyID, requestModel, parseExcludedChannels(c.GetString("excluded_channels")))
	if iter.Len() == 0 {
		resp.Error(c, http.StatusServiceUnavailable, "no available channel")
		return
	}

	operationCtx, cancel := newRelayOperationContext()
	defer cancel()

	maxKeyRetriesPerRoute := getMaxAttemptsPerCandidate()
	maxRouteRetries := getMaxRouteRetries()
	ratelimitCooldown := getRatelimitCooldown()
	maxTotalAttempts := getMaxTotalAttempts()

	// Track the last channel used for exhausted logging
	var lastChannelID int
	var lastChannelName string
	var lastResolvedModel string
	var mediaDone bool
	// Upstream-reported token usage of the attempt that produced the response the
	// client received. Reset per attempt so a failed hop's usage can never be
	// billed against a later hop's success (P1 #11 / scan doc §10.1).
	var billedUsage mediaUsage

	retryWithChannels(group, requestModel, apiKeyID, c.GetString("excluded_channels"),
		maxKeyRetriesPerRoute, maxRouteRetries, ratelimitCooldown, maxTotalAttempts,
		retryCallbacks{
			Ctx: c.Request.Context(),
			CheckContext: func() error {
				if err := operationCtx.Err(); err != nil {
					log.Infof("relay operation ended before media request completed: %v", err)
					return err
				}
				select {
				case <-c.Request.Context().Done():
					err := c.Request.Context().Err()
					log.Infof("request context canceled, stopping media retry")
					return err
				default:
				}
				return nil
			},
			LogAttempt: func(channel *dbmodel.Channel, resolvedModel string, round retryRoundInfo) {
				log.Infof("media relay: endpoint=%d, model=%s, channel: %s model: %s key_id: %d (route R%d, key %d/%d)",
					endpointType, requestModel, channel.Name, resolvedModel, round.UsedKey.ID,
					round.RouteRound, round.KeyRound, round.MaxKeyRetries)
			},
			ForwardRequest: func(channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, resolvedModel string, round retryRoundInfo) retryForwardResult {
				span := round.Iter.StartAttempt(channel.ID, usedKey.ID, channel.Name, resolvedModel)
				// One scanner per attempt: a retry must not inherit the previous
				// hop's usage.
				usageScan := &usageScanner{}
				statusCode, fwdErr := forwardMediaRequest(c, cfg, group, channel, usedKey.ChannelKey, bodyBytes, requestModel, resolvedModel, streamRequested, operationCtx, usageScan)
				if scanned, ok := usageScan.Usage(); ok {
					billedUsage = scanned
				} else {
					billedUsage = mediaUsage{}
				}

				lastChannelID = channel.ID
				lastChannelName = channel.Name
				lastResolvedModel = resolvedModel

				written := c.Writer.Written()
				decision := ClassifyRelayError(statusCode, fwdErr, written)

				usedKey.StatusCode = statusCode
				usedKey.LastUseTimeStamp = time.Now().Unix()

				if decision.Scope == ScopeNone && !decision.IsError {
					ch.KeyUpdate(usedKey)
					span.End(dbmodel.AttemptSuccess, statusCode, "")
					st.ChannelUpdate(channel.ID, dbmodel.StatsMetrics{
						WaitTime:       span.Duration().Milliseconds(),
						RequestSuccess: 1,
					})
					balancer.RecordSuccess(channel.ID, usedKey.ID, resolvedModel)
					balancer.RecordAutoSuccess(channel.ID, resolvedModel)
					balancer.RecordAutoLatency(channel.ID, resolvedModel, span.Duration().Milliseconds())
					balancer.SetSticky(apiKeyID, requestModel, channel.ID, usedKey.ID)
					return retryForwardResult{Decision: decision, Err: fwdErr}
				}

				ch.KeyUpdate(usedKey)
				span.End(dbmodel.AttemptFailed, statusCode, decision.String())
				st.ChannelUpdate(channel.ID, dbmodel.StatsMetrics{
					WaitTime:      span.Duration().Milliseconds(),
					RequestFailed: 1,
				})

				if decision.IsError {
					log.Warnf("media relay: channel %s failed on key %d: %v (decision: %s)",
						channel.Name, round.KeyRound, fwdErr, decision.Scope.String())
				}

				return retryForwardResult{Decision: decision, Err: fwdErr}
			},
			OnSuccess: func(channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, resolvedModel string, round retryRoundInfo) {
				recordMediaRelayLog(apiKeyID, requestModel, logEndpointType, bodyBytes, channel.ID, channel.Name, resolvedModel, time.Since(startTime), snapshotAttempts(round.Iter), nil, clientIP, billedUsage)
				mediaDone = true
			},
			OnFailure: func(channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, resolvedModel string) {
				balancer.RecordFailure(channel.ID, usedKey.ID, resolvedModel)
				balancer.RecordAutoFailure(channel.ID, resolvedModel)
			},
			OnFinalFailure: func(channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, resolvedModel string, round retryRoundInfo, fwdResult retryForwardResult) bool {
				recordMediaRelayLog(apiKeyID, requestModel, logEndpointType, bodyBytes, channel.ID, channel.Name, resolvedModel, time.Since(startTime), snapshotAttempts(round.Iter), fwdResult.Err, clientIP, billedUsage)
				// 与 LLM relay 一致：客户端错误原样回给下游，不吞成 502。
				if fwdResult.Decision.Scope == ScopeNone {
					writeClientTerminalError(c, fwdResult.Decision.Code, fwdResult.Err)
				}
				mediaDone = true
				return true
			},
			OnExhausted: func(allAttempts []dbmodel.ChannelAttempt, lastErr error) {
				if mediaDone {
					return
				}
				// No attempt produced a response the client kept, so there is no
				// usage to bill — pass the zero value, not the last hop's.
				recordMediaRelayLog(apiKeyID, requestModel, logEndpointType, bodyBytes, lastChannelID, lastChannelName, lastResolvedModel, time.Since(startTime), allAttempts, lastErr, clientIP, mediaUsage{})
				if lastErr != nil {
					resp.Error(c, http.StatusBadGateway, fmt.Sprintf("all channels failed: %v", lastErr))
				} else {
					resp.Error(c, http.StatusBadGateway, "all channels failed")
				}
			},
			UseFailureHints:             false,
			UsePrepareCandidateForRetry: false,
		},
	)
}

// snapshotAttempts copies the iterator's attempt records for the terminal
// callbacks (R-4). OnSuccess/OnFinalFailure used to pass nil, so a media relay
// that failed over A→B→C logged TotalAttempts=0, wrote no relay_log_attempts
// rows, and never credited the per-(channel,model) site stats — the failed
// hops were invisible and the winning hop uncounted.
//
// Both call sites are terminal (the retry loop returns right after), so the
// backing array is not appended to afterwards today; the copy is defensive and
// matches how the LLM relay hands attempts to its own callbacks (relay.go:1239).
// It matters because the RelayLog that holds this slice outlives the call — it
// is buffered in the relay-log cache and flushed asynchronously.
func snapshotAttempts(iter *balancer.Iterator) []dbmodel.ChannelAttempt {
	if iter == nil {
		return nil
	}
	return append([]dbmodel.ChannelAttempt(nil), iter.Attempts()...)
}

// recordMediaRelayLog creates a RelayLog entry and updates global stats for media endpoints.
//
// usage carries the upstream-reported token counts for the attempt whose response
// reached the client, or the zero value when upstream reported none (binary TTS,
// providers that omit usage, every failed request). A zero usage keeps the
// pre-existing request-param pricing behavior untouched.
func recordMediaRelayLog(apiKeyID int, requestModel string, endpointType string, bodyBytes []byte, channelID int, channelName string, resolvedModel string, duration time.Duration, attempts []dbmodel.ChannelAttempt, relayErr error, clientIP string, usage mediaUsage) {
	ctx, cancel := newRelayPersistenceContext()
	defer cancel()

	// resolvedModel 来自 ForwardRequest，仅在请求真正转发后才有值。OnExhausted 等
	// 从未进入 ForwardRequest 的路径会传空串，回退到用户请求的模型名（BUG-003 附带缺陷 ②）。
	if resolvedModel == "" {
		resolvedModel = requestModel
	}

	relayLog := dbmodel.RelayLog{
		Time:             time.Now().Add(-duration).Unix(),
		RequestModelName: requestModel,
		RequestAPIKeyID:  apiKeyID,
		ClientIP:         clientIP,
		EndpointType:     endpointType,
		ChannelId:        channelID,
		ChannelName:      channelName,
		ActualModelName:  resolvedModel,
		UseTime:          int(duration.Milliseconds()),
		Attempts:         attempts,
		TotalAttempts:    len(attempts),
		// P1 #11: persist the upstream token counts. These were hard zero for all
		// 10 /v1 media routes, which left the token dimension of the dashboard,
		// and the per-key MaxTokens ceiling, permanently blind to media traffic.
		InputTokens:     int(usage.InputTokens),
		OutputTokens:    int(usage.OutputTokens),
		CacheReadTokens: int(usage.CachedTokens),
	}

	if apiKey, getErr := ak.Get(apiKeyID, ctx); getErr == nil {
		relayLog.RequestAPIKeyName = apiKey.Name
	}

	if len(bodyBytes) > 0 {
		relayLog.RequestContent = string(bodyBytes)
	}

	if relayErr != nil {
		relayLog.Error = relayErr.Error()
	}

	// Lodestar media billing (BUG-003): price the request by its body params when a
	// billing expression is configured for the user-facing model. Cost must be
	// computed before RelayLogAdd so relayLog.Cost persists with the log.
	//
	// P1 #11 priority rule (scan doc §10.1, written down deliberately): when
	// upstream reported real usage, feed those tokens into the expression;
	// otherwise pass all-zero TokenParams so param()-only expressions price the
	// request exactly as they did before. The request body is passed either way,
	// so an expression may mix param() with token dimensions. Tokens are never
	// fabricated when upstream reported none.
	tokenParams := billingexpr.TokenParams{}
	if usage.Valid() {
		tokenParams = usage.TokenParams()
	}
	var mediaCost float64
	if exprCost, _, ok := billing.ComputeExprCostFullWithRequest(requestModel, tokenParams, billingexpr.RequestInput{Body: bodyBytes}); ok {
		mediaCost = exprCost
	}
	relayLog.Cost = mediaCost

	// relayLog.Attempts 已在上面设好；尝试明细由 relayLogFlushToDB 在父日志
	// 落库后同批写入，此处不再单独写（R-9：RelayLogAdd 只进内存缓存，
	// 在这里写明细会让 relay_log_attempts 先于 relay_logs 落库）。
	if logErr := relaylog.RelayLogAdd(ctx, &relayLog); logErr != nil {
		log.Warnf("failed to save media relay log: %v", logErr)
	}

	// Record global and API-key stats. InputToken/OutputToken were hard zero
	// before P1 #11; APIKeyUpdate feeds the accumulator that the per-key
	// MaxTokens ceiling reads (middleware/auth.go), so media traffic now counts
	// against it instead of being unlimited.
	stats := dbmodel.StatsMetrics{
		WaitTime:    int64(duration.Milliseconds()),
		InputToken:  usage.InputTokens,
		OutputToken: usage.OutputTokens,
		InputCost:   mediaCost,
		OutputCost:  0,
	}
	if relayErr == nil {
		stats.RequestSuccess = 1
		log.Infof("media relay complete: model=%s, channel=%d(%s), duration=%dms, attempts=%d, input_token=%d, output_token=%d, cost=%f",
			requestModel, channelID, channelName, duration.Milliseconds(), len(attempts),
			usage.InputTokens, usage.OutputTokens, mediaCost)
	} else {
		stats.RequestFailed = 1
		log.Infof("media relay failed: model=%s, duration=%dms, attempts=%d, error=%v",
			requestModel, duration.Milliseconds(), len(attempts), relayErr)
	}

	st.TotalUpdate(stats)
	st.HourlyUpdate(stats)
	if statsErr := st.DailyUpdate(ctx, stats); statsErr != nil {
		log.Warnf("failed to update daily stats for media relay: %v", statsErr)
	}
	st.APIKeyUpdate(apiKeyID, stats)
	// Lodestar commercial: deduct the already-computed media cost from the key
	// owner's balance (no-op unless commercial_mode on). BUG-004: ChargeKeyWithExpr
	// would re-run ComputeExprCost WITHOUT the request body and overwrite mediaCost
	// with a body-less value; media cost is already final here, so call ChargeKey
	// directly — exactly one charge, for exactly mediaCost.
	// P2 guard: do NOT charge if relay failed — prevents charging for 502 responses
	// when OnExhausted returns error after all retries exhausted.
	if relayErr == nil {
		billing.ChargeKey(apiKeyID, mediaCost, ctx)
	}
	opMain.StatsSiteModelHourlyRecordAttempts(attempts, resolvedModel)
	telemetry.Global().RecordRequest(duration.Milliseconds(), relayErr == nil)
}

func recordPreparedCandidateSkip(iter *balancer.Iterator, item dbmodel.GroupItem, prepare PrepareCandidateResult) {
	if prepare.SkipReason == "" {
		return
	}
	// PrepareCandidate already records circuit-break rejections with cooldown details.
	if prepare.SkipStatus == dbmodel.AttemptCircuitBreak {
		return
	}

	channelID := item.ChannelID
	channelName := fmt.Sprintf("channel_%d", item.ChannelID)
	keyID := 0
	if prepare.Channel != nil {
		channelID = prepare.Channel.ID
		channelName = prepare.Channel.Name
	}
	if prepare.UsedKey.ID != 0 {
		keyID = prepare.UsedKey.ID
	}
	iter.Skip(channelID, keyID, channelName, prepare.SkipReason)
}

// extractModelFromRequest extracts the model name from the request body.
// For JSON endpoints, it parses the body into a generic map.
// For multipart endpoints, it reads the form field.
func extractModelFromRequest(c *gin.Context, cfg mediaEndpointConfig) (string, []byte, bool, error) {
	if cfg.MultipartInput {
		return extractModelFromMultipart(c)
	}
	return extractModelFromJSON(c)
}

// extractModelFromJSON reads the JSON body and extracts the "model" field.
func extractModelFromJSON(c *gin.Context) (string, []byte, bool, error) {
	body, err := readLimitedRequestBody(c, getMaxRelayJSONBodyBytes())
	if err != nil {
		return "", nil, false, err
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", nil, false, fmt.Errorf("invalid JSON body: %w", err)
	}

	model, _ := raw["model"].(string)
	streamRequested := parseMediaStreamFlag(raw["stream"])
	return model, body, streamRequested, nil
}

// extractBodyForBilling returns the request body as seen by the billing path.
// JSON endpoints pass bodyBytes through unchanged; multipart endpoints have no
// JSON body, so their parsed form fields are serialized to a JSON object
// (BUG-004b). Returns bodyBytes unchanged when the request is not multipart or
// the form is absent.
func extractBodyForBilling(c *gin.Context, cfg mediaEndpointConfig, bodyBytes []byte) []byte {
	if !cfg.MultipartInput || bodyBytes != nil || c.Request.MultipartForm == nil {
		return bodyBytes
	}
	return multipartFormToJSONBody(c.Request.MultipartForm)
}

// multipartFormToJSONBody serializes multipart form value fields into a JSON
// object ({"field":"firstValue", ...}) so billing expressions can read them via
// param(). Only the first value of each field is kept — multipart form fields
// are string lists, and billing params like size/n/quality are single-valued.
// Returns nil (no panic) when form is nil or has no value fields.
func multipartFormToJSONBody(form *multipart.Form) []byte {
	if form == nil {
		return nil
	}
	fields := make(map[string]string, len(form.Value))
	for key, values := range form.Value {
		if len(values) > 0 {
			fields[key] = values[0]
		}
	}
	if len(fields) == 0 {
		return nil
	}
	body, err := json.Marshal(fields)
	if err != nil {
		log.Warnf("multipart form serialization failed: %v", err)
		return nil
	}
	return body
}

// extractModelFromMultipart extracts the model from a multipart/form-data request.
func extractModelFromMultipart(c *gin.Context) (string, []byte, bool, error) {
	limitRequestBody(c, getMaxRelayMultipartBodyBytes())

	// Parse the multipart form
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		return "", nil, false, normalizeRelayRequestBodyError(err)
	}

	model := c.Request.FormValue("model")
	streamRequested := strings.EqualFold(strings.TrimSpace(c.Request.FormValue("stream")), "true")
	// We'll re-read the full multipart body in forwardMediaRequestMultipart
	return model, nil, streamRequested, nil
}

// forwardMediaRequest builds and sends the upstream request, then streams the response back.
func forwardMediaRequest(
	c *gin.Context,
	cfg mediaEndpointConfig,
	group dbmodel.Group,
	channel *dbmodel.Channel,
	key string,
	bodyBytes []byte,
	requestModel string,
	resolvedModel string,
	streamRequested bool,
	operationCtx context.Context,
	usage *usageScanner,
) (int, error) {
	if cfg.MultipartInput {
		return forwardMediaRequestMultipart(c, cfg, channel, key, requestModel, resolvedModel, streamRequested, operationCtx, usage)
	}
	return forwardMediaRequestJSON(c, cfg, group, channel, key, bodyBytes, requestModel, resolvedModel, streamRequested, operationCtx, usage)
}

// forwardMediaRequestJSON handles JSON-based media endpoint forwarding.
func forwardMediaRequestJSON(
	c *gin.Context,
	cfg mediaEndpointConfig,
	group dbmodel.Group,
	channel *dbmodel.Channel,
	key string,
	bodyBytes []byte,
	requestModel string,
	resolvedModel string,
	streamRequested bool,
	operationCtx context.Context,
	usage *usageScanner,
) (int, error) {
	ctx := operationCtx

	// Replace model name in the JSON body
	modifiedBody, err := replaceModelInJSON(bodyBytes, requestModel, resolvedModel)
	if err != nil {
		return 0, fmt.Errorf("failed to replace model in request: %w", err)
	}

	// Apply provider-specific path rewrite for video generation
	cfg = rewriteVideoRequestByProvider(group, cfg)

	// Apply provider-specific body + path rewrite for audio speech
	modifiedBody, cfg = rewriteAudioSpeechRequestByProvider(group, cfg, modifiedBody)

	// Apply provider-specific body + path rewrite for music generation
	modifiedBody, cfg.UpstreamPath, err = rewriteMusicRequestByProvider(group, cfg, modifiedBody, resolvedModel)
	if err != nil {
		return 0, fmt.Errorf("failed to rewrite music request: %w", err)
	}

	// Build upstream URL
	upstreamURL, err := buildMediaUpstreamURL(channel.GetBaseUrl(), cfg.UpstreamPath)
	if err != nil {
		return 0, fmt.Errorf("failed to build upstream URL: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(modifiedBody))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	copyMediaForwardHeaders(req, c, channel, key, "application/json", streamRequested)

	// MiMo Chat Completions API only accepts application/json, but the
	// upstream TTS client (e.g. OpenAI SDK) sends Accept: audio/mpeg.
	// Override after copyMediaForwardHeaders to prevent 406 responses.
	if strings.EqualFold(strings.TrimSpace(group.EndpointProvider), "mimo") && cfg.UpstreamPath == "/v1/chat/completions" {
		req.Header.Set("Accept", "application/json")
	}

	// Send request
	httpClient, err := helper.ChannelHttpClient(channel)
	if err != nil {
		return 0, fmt.Errorf("failed to get http client: %w", err)
	}

	response, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(response.Body, 4*1024))
		return response.StatusCode, fmt.Errorf("upstream error: %d: %s", response.StatusCode, string(respBody))
	}

	// Image bed: for image generation endpoints, try to upload base64 images
	// to an external image bed and replace with hosted URLs.
	if cfg.UpstreamPath == "/v1/images/generations" {
		if modified, ok := tryImageBedRewrite(c, response, usage); ok {
			return response.StatusCode, modified
		}
	}

	// Stream response back to client
	if cfg.BinaryResponse {
		provider := strings.ToLower(strings.TrimSpace(group.EndpointProvider))
		if provider == "mimo" && cfg.UpstreamPath == "/v1/chat/completions" {
			return handleMimoTTSResponse(c, response, cfg.AudioFormat, usage)
		}
		return handleBinaryResponse(c, response)
	}
	if isMediaSSEResponse(response) {
		return handleSSEResponse(c, response, usage)
	}
	return handleJSONResponse(c, response, usage)
}

// forwardMediaRequestMultipart handles multipart/form-data media endpoint forwarding.
func forwardMediaRequestMultipart(
	c *gin.Context,
	cfg mediaEndpointConfig,
	channel *dbmodel.Channel,
	key string,
	requestModel string,
	resolvedModel string,
	streamRequested bool,
	operationCtx context.Context,
	usage *usageScanner,
) (int, error) {
	ctx := operationCtx

	// Build upstream URL
	upstreamURL, err := buildMediaUpstreamURL(channel.GetBaseUrl(), cfg.UpstreamPath)
	if err != nil {
		return 0, fmt.Errorf("failed to build upstream URL: %w", err)
	}

	bodyReader, contentType := buildMultipartForwardBody(c.Request.MultipartForm, resolvedModel)

	// Create upstream request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bodyReader)
	if err != nil {
		bodyReader.Close() // 关闭 pipe reader 以释放 writer goroutine
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	copyMediaForwardHeaders(req, c, channel, key, contentType, streamRequested)

	// Send request
	httpClient, err := helper.ChannelHttpClient(channel)
	if err != nil {
		return 0, fmt.Errorf("failed to get http client: %w", err)
	}

	response, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(response.Body, 4*1024))
		return response.StatusCode, fmt.Errorf("upstream error: %d: %s", response.StatusCode, string(respBody))
	}

	if isMediaSSEResponse(response) {
		return handleSSEResponse(c, response, usage)
	}
	return handleJSONResponse(c, response, usage)
}

func parseMediaStreamFlag(raw any) bool {
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	default:
		return false
	}
}

func buildMultipartForwardBody(form *multipart.Form, resolvedModel string) (io.ReadCloser, string) {
	reader, writer := io.Pipe()
	mpWriter := multipart.NewWriter(writer)
	contentType := mpWriter.FormDataContentType()

	go func() {
		defer writer.Close()
		defer mpWriter.Close()
		defer func() {
			if r := recover(); r != nil {
				_ = writer.CloseWithError(fmt.Errorf("panic in multipart builder: %v", r))
			}
		}()

		if form == nil {
			return
		}

		for fieldName, values := range form.Value {
			for _, value := range values {
				fieldValue := value
				if fieldName == "model" && resolvedModel != "" {
					fieldValue = resolvedModel
				}
				if err := mpWriter.WriteField(fieldName, fieldValue); err != nil {
					_ = writer.CloseWithError(fmt.Errorf("failed to write field %s: %w", fieldName, err))
					return
				}
			}
		}

		for fieldName, fileHeaders := range form.File {
			for _, fileHeader := range fileHeaders {
				file, err := fileHeader.Open()
				if err != nil {
					_ = writer.CloseWithError(fmt.Errorf("failed to open uploaded file: %w", err))
					return
				}

				part, err := mpWriter.CreateFormFile(fieldName, fileHeader.Filename)
				if err != nil {
					file.Close()
					_ = writer.CloseWithError(fmt.Errorf("failed to create form file: %w", err))
					return
				}
				if _, err := io.Copy(part, file); err != nil {
					file.Close()
					_ = writer.CloseWithError(fmt.Errorf("failed to copy file content: %w", err))
					return
				}
				file.Close()
			}
		}
	}()

	return reader, contentType
}

// replaceModelInJSON replaces the model field value in a JSON body.
func replaceModelInJSON(body []byte, originalModel, resolvedModel string) ([]byte, error) {
	if resolvedModel == "" || resolvedModel == originalModel {
		return body, nil
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		log.Debugf("replaceModelInJSON: failed to parse JSON body, returning original: %v", err)
		return body, nil
	}

	raw["model"] = resolvedModel
	return json.Marshal(raw)
}

// buildMediaUpstreamURL constructs the full upstream URL from base URL and path.
func buildMediaUpstreamURL(baseURL, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil {
		return "", fmt.Errorf("failed to parse base url: %w", err)
	}

	basePath := strings.TrimSuffix(parsed.Path, "/")
	normalizedPath := path
	if strings.HasSuffix(basePath, "/v1") && strings.HasPrefix(normalizedPath, "/v1/") {
		normalizedPath = strings.TrimPrefix(normalizedPath, "/v1")
	}

	parsed.Path = basePath + normalizedPath
	return parsed.String(), nil
}

// applyChannelHeaders applies channel custom headers to the request.
func applyChannelHeaders(req *http.Request, channel *dbmodel.Channel) {
	if len(channel.CustomHeader) > 0 {
		for _, header := range channel.CustomHeader {
			req.Header.Set(header.HeaderKey, header.HeaderValue)
		}
	}
}

func copyMediaForwardHeaders(req *http.Request, c *gin.Context, channel *dbmodel.Channel, key string, contentType string, streamRequested bool) {
	for headerKey, values := range c.Request.Header {
		if hopByHopHeaders[strings.ToLower(headerKey)] {
			continue
		}
		if strings.EqualFold(headerKey, "Authorization") || strings.EqualFold(headerKey, "Content-Type") || strings.EqualFold(headerKey, "Content-Length") {
			continue
		}
		for _, value := range values {
			req.Header.Add(headerKey, value)
		}
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if streamRequested {
		req.Header.Set("Accept", "text/event-stream")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	applyChannelHeaders(req, channel)
}

// handleBinaryResponse streams a binary response (e.g. audio) back to the client.
// Binary bodies carry no JSON usage object, so nothing is scanned here — TTS
// keeps request-param pricing.
func handleBinaryResponse(c *gin.Context, response *http.Response) (int, error) {
	// Copy relevant headers
	if ct := response.Header.Get("Content-Type"); ct != "" {
		c.Header("Content-Type", ct)
	}
	c.Header("Content-Disposition", response.Header.Get("Content-Disposition"))

	_, err := io.Copy(c.Writer, response.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to stream binary response: %w", err)
	}

	return response.StatusCode, nil
}

func isMediaSSEResponse(response *http.Response) bool {
	if response == nil {
		return false
	}
	return strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream")
}

// handleSSEResponse streams an SSE response back to the client, teeing each line
// through usage so the final frame's usage object is captured. Streaming image
// generation repeats `usage` per frame; the scanner keeps the newest complete one.
func handleSSEResponse(c *gin.Context, response *http.Response, usage *usageScanner) (int, error) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if _, writeErr := c.Writer.Write(line); writeErr != nil {
				return 0, fmt.Errorf("failed to stream sse response: %w", writeErr)
			}
			usage.Scan(line)
			c.Writer.Flush()
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return response.StatusCode, nil
			}
			return 0, fmt.Errorf("failed to read sse response: %w", err)
		}
	}
}

// handleJSONResponse streams a JSON response back to the client, teeing it
// through usage to capture the upstream token counts.
//
// The body is *not* buffered: image responses carry multi-megabyte base64 data,
// so it is copied straight to the client while the scanner keeps only the usage
// object. io.MultiWriter keeps io.Copy's read/write loop (and its buffer reuse)
// intact.
func handleJSONResponse(c *gin.Context, response *http.Response, usage *usageScanner) (int, error) {
	// For large responses (e.g. image generation with base64), stream directly
	if ct := response.Header.Get("Content-Type"); ct != "" {
		c.Header("Content-Type", ct)
	}

	_, err := io.Copy(io.MultiWriter(c.Writer, usage), response.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to stream response: %w", err)
	}

	return response.StatusCode, nil
}

type musicGenerationChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func rewriteMusicRequestByProvider(group dbmodel.Group, cfg mediaEndpointConfig, body []byte, resolvedModel string) ([]byte, string, error) {
	if cfg.UpstreamPath != "/v1/music/generations" {
		return body, cfg.UpstreamPath, nil
	}
	provider := strings.ToLower(strings.TrimSpace(group.EndpointProvider))
	if provider == "" || provider == "auto" {
		return body, cfg.UpstreamPath, nil
	}

	if provider != "newapi" && provider != "minimax" {
		return body, cfg.UpstreamPath, nil
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, "", err
	}

	raw["model"] = resolvedModel
	if _, ok := raw["messages"]; !ok {
		prompt := strings.TrimSpace(fmt.Sprintf("%v", raw["prompt"]))
		if prompt != "" && prompt != "<nil>" {
			raw["messages"] = []musicGenerationChatMessage{{Role: "user", Content: prompt}}
		}
	}
	delete(raw, "prompt")

	converted, err := json.Marshal(raw)
	if err != nil {
		return nil, "", err
	}
	return converted, "/v1/music_generation", nil
}

// rewriteVideoRequestByProvider adjusts the upstream path for video generation
// based on the group's EndpointProvider setting.
// Agnes Video V2.0 uses POST /v1/videos instead of the standard /v1/videos/generations.
func rewriteVideoRequestByProvider(group dbmodel.Group, cfg mediaEndpointConfig) mediaEndpointConfig {
	if cfg.UpstreamPath != "/v1/videos/generations" {
		return cfg
	}
	provider := strings.ToLower(strings.TrimSpace(group.EndpointProvider))
	switch provider {
	case "agnes":
		cfg.UpstreamPath = "/v1/videos"
	}
	return cfg
}

// rewriteAudioSpeechRequestByProvider converts the request body and path for
// provider-specific TTS implementations. MiMo TTS uses the Chat Completions API
// format (POST /v1/chat/completions) instead of the standard /v1/audio/speech.
func rewriteAudioSpeechRequestByProvider(group dbmodel.Group, cfg mediaEndpointConfig, body []byte) ([]byte, mediaEndpointConfig) {
	if cfg.UpstreamPath != "/v1/audio/speech" {
		return body, cfg
	}
	provider := strings.ToLower(strings.TrimSpace(group.EndpointProvider))
	if provider != "mimo" {
		return body, cfg
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, cfg
	}

	input, _ := raw["input"].(string)
	voice, _ := raw["voice"].(string)
	format, _ := raw["response_format"].(string)
	model, _ := raw["model"].(string)

	if format == "" {
		format = "wav"
	}
	// MiMo TTS only supports wav, mp3, pcm, pcm16.
	// Map unsupported formats (opus, flac, aac, etc.) to mp3.
	if format != "wav" && format != "mp3" && format != "pcm" && format != "pcm16" {
		format = "mp3"
	}
	cfg.AudioFormat = format
	if voice == "" {
		voice = "mimo_default"
	}

	mimoReq := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "assistant", "content": input},
		},
		"audio": map[string]string{
			"format": format,
			"voice":  voice,
		},
	}

	converted, err := json.Marshal(mimoReq)
	if err != nil {
		return body, cfg
	}
	cfg.UpstreamPath = "/v1/chat/completions"
	return converted, cfg
}

// mimoTTSChatResponse represents the relevant fields of a MiMo TTS chat completion response.
type mimoTTSChatResponse struct {
	Choices []struct {
		Message struct {
			Audio *struct {
				Data string `json:"data"`
			} `json:"audio"`
		} `json:"message"`
	} `json:"choices"`
}

// handleMimoTTSResponse extracts the base64-encoded audio from a MiMo chat
// completion JSON response and sends it as binary audio to the client.
func handleMimoTTSResponse(c *gin.Context, response *http.Response, audioFormat string, usage *usageScanner) (int, error) {
	respBody, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read MiMo TTS response: %w", err)
	}
	// The upstream here is chat/completions, so it reports usage even though what
	// we hand the client is binary audio.
	usage.Scan(respBody)

	var mimoResp mimoTTSChatResponse
	if err := json.Unmarshal(respBody, &mimoResp); err != nil {
		return response.StatusCode, fmt.Errorf("failed to parse MiMo TTS response: %w", err)
	}

	if len(mimoResp.Choices) == 0 || mimoResp.Choices[0].Message.Audio == nil {
		return response.StatusCode, fmt.Errorf("MiMo TTS response contains no audio data")
	}

	audioData, err := base64.StdEncoding.DecodeString(mimoResp.Choices[0].Message.Audio.Data)
	if err != nil {
		return response.StatusCode, fmt.Errorf("failed to decode MiMo TTS audio: %w", err)
	}

	// Set Content-Type based on the resolved audio format.
	contentType := "audio/wav"
	switch audioFormat {
	case "mp3":
		contentType = "audio/mpeg"
	case "pcm", "pcm16":
		contentType = "audio/pcm"
	}
	c.Header("Content-Type", contentType)
	_, err = c.Writer.Write(audioData)
	if err != nil {
		return 0, fmt.Errorf("failed to write MiMo TTS audio: %w", err)
	}

	return response.StatusCode, nil
}

// tryImageBedRewrite reads the image generation response, attempts to upload
// each b64_json item to the image bed, and rewrites the response with hosted
// URLs. Returns (error, true) on successful rewrite, (nil, false) if image
// bed is disabled or the rewrite should be skipped.
func tryImageBedRewrite(c *gin.Context, response *http.Response, usage *usageScanner) (error, bool) {
	cfg := readImageBedConfig()
	if !cfg.Enabled {
		return nil, false
	}

	respBody, err := io.ReadAll(response.Body)
	if err != nil {
		log.Warnf("image bed: failed to read response body: %v", err)
		return nil, false
	}
	// Scan before any rewrite: the rewritten body is re-marshalled from
	// imageGenResponse, and Usage must survive that round-trip either way.
	usage.Scan(respBody)

	var parsed imageGenResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		log.Warnf("image bed: failed to parse image response JSON: %v", err)
		return writeOriginalResponse(c, respBody, response)
	}

	modified := rewriteImageGenResponse(parsed, cfg)
	if !modified {
		return writeOriginalResponse(c, respBody, response)
	}

	newBody, err := json.Marshal(parsed)
	if err != nil {
		log.Warnf("image bed: failed to marshal modified response: %v", err)
		return writeOriginalResponse(c, respBody, response)
	}

	if ct := response.Header.Get("Content-Type"); ct != "" {
		c.Header("Content-Type", ct)
	} else {
		c.Header("Content-Type", "application/json")
	}
	if _, err := c.Writer.Write(newBody); err != nil {
		return fmt.Errorf("failed to write modified image response: %w", err), true
	}
	return nil, true
}

// imageGenResponse represents the minimal structure of an image generation
// response needed for image bed rewriting.
//
// Usage is carried as raw JSON purely so the image-bed rewrite does not drop it:
// the rewritten body is re-marshalled from this struct, and a client that asked
// for token usage must still get it. Billing reads usage from the scanner, not
// from here. omitempty keeps responses without usage byte-identical to before.
type imageGenResponse struct {
	Created int64           `json:"created"`
	Data    []imageGenDatum `json:"data"`
	Usage   json.RawMessage `json:"usage,omitempty"`
}

// imageGenDatum represents a single image in the response.
type imageGenDatum struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// rewriteImageGenResponse replaces b64_json entries with image bed URLs.
// Returns true if at least one image was rewritten.
func rewriteImageGenResponse(resp imageGenResponse, cfg imageBedConfig) bool {
	anyRewritten := false
	for i, datum := range resp.Data {
		if datum.B64JSON == "" {
			continue
		}
		url, err := uploadToImageBed(datum.B64JSON, cfg)
		if err != nil {
			log.Warnf("image bed: upload failed for image %d: %v", i, err)
			continue
		}
		// Create a new datum instead of mutating.
		resp.Data[i] = imageGenDatum{
			URL:           url,
			RevisedPrompt: datum.RevisedPrompt,
		}
		anyRewritten = true
	}
	return anyRewritten
}

// writeOriginalResponse writes the original response body to the client.
func writeOriginalResponse(c *gin.Context, body []byte, response *http.Response) (error, bool) {
	if ct := response.Header.Get("Content-Type"); ct != "" {
		c.Header("Content-Type", ct)
	}
	if _, err := c.Writer.Write(body); err != nil {
		return fmt.Errorf("failed to write response: %w", err), true
	}
	return nil, true
}
