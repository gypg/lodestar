package airoute

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/channel"
	"github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/utils/log"
	"github.com/gypg/lodestar/internal/utils/xstrings"
	"golang.org/x/sync/semaphore"
)

const (
	defaultAIRouteHTTPTimeout  = 180 * time.Second
	aiRouteMaxModelsPerRequest = 120
	aiRouteResponseMaxSize     = 2 << 20
	defaultAIRouteRetryBackoff = 10 * time.Second
	aiRouteMaxTokens           = 4096
)

type aiRoutePromptModelInput struct {
	ChannelID int    `json:"channel_id"`
	Model     string `json:"model"`
}

type aiRoutePromptBucket struct {
	PromptEndpointType string
	GroupEndpointType  string
	ModelInputs        []aiRoutePromptModelInput
}

type aiRouteChatCompletionRequest struct {
	Model       string                      `json:"model"`
	Messages    []aiRouteChatCompletionItem `json:"messages"`
	Temperature float64                     `json:"temperature"`
	MaxTokens   int                         `json:"max_tokens,omitempty"`
}

type aiRouteChatCompletionItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type aiRouteChatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content any `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type aiRouteCallError struct {
	StatusCode int
	Retryable  bool
	Cooldown   time.Duration
	Message    string
	Cause      error
}

type aiRouteTableRouteCorrection struct {
	OriginalName  string
	EndpointType  string
	CorrectedName string
}

type aiRouteBucketFailure struct {
	Index int
	Total int
	Err   error
}

type aiRouteBucketResult struct {
	Index  int
	Total  int
	Routes []model.AIRouteEntry
	Err    error
}

// AIRoutePartialFailureError is exported for callers to check partial failure via errors.As.
type AIRoutePartialFailureError struct {
	Message     string
	MessageKey  string
	MessageArgs map[string]any
	Cause       error
}

func (e *aiRouteCallError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *aiRouteCallError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *AIRoutePartialFailureError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *AIRoutePartialFailureError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// GenerateAIRoute is the public entry point for AI route generation.
func GenerateAIRoute(
	ctx context.Context,
	req model.GenerateAIRouteRequest,
	report aiRouteProgressReporter,
) (*model.GenerateAIRouteResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	tracker := newAIRouteProgressTracker(req, report)
	modelInputs, inputModelSet, err := collectAIRouteModelInputs(ctx)
	if err != nil {
		return nil, err
	}
	if len(modelInputs) == 0 {
		return nil, fmt.Errorf("没有可分析的模型")
	}
	tracker.SetModelInputs(modelInputs)

	if req.Scope == model.AIRouteScopeTable {
		return generateAIRouteTable(ctx, modelInputs, inputModelSet, tracker)
	}

	return generateAIRouteForGroup(ctx, req.GroupID, modelInputs, inputModelSet, tracker)
}

func generateAIRouteForGroup(
	ctx context.Context,
	groupID int,
	modelInputs []model.AIRouteModelInput,
	inputModelSet map[int]map[string]struct{},
	tracker *aiRouteProgressTracker,
) (*model.GenerateAIRouteResult, error) {
	if groupID <= 0 {
		value, err := setting.GetInt(model.SettingKeyAIRouteGroupID)
		if err != nil {
			return nil, fmt.Errorf("请先在设置中选择AI路由目标分组")
		}
		groupID = value
	}
	if groupID <= 0 {
		return nil, fmt.Errorf("请先在设置中选择AI路由目标分组")
	}

	g, err := group.GroupGet(groupID, ctx)
	if err != nil {
		return nil, fmt.Errorf("目标分组不存在")
	}
	if tracker != nil {
		tracker.SetTargetGroup(g.ID)
	}

	targetPromptEndpointType := detectAIRoutePromptEndpointTypeForGroup(*g)
	routes, err := generateAIRoutesFromModelList(ctx, modelInputs, g.Name, targetPromptEndpointType, tracker)
	if err != nil {
		return nil, err
	}
	if tracker != nil {
		tracker.SetValidatingRoutes(len(routes))
	}

	selectedRoute, err := selectAIRouteForGroup(*g, routes)
	if err != nil {
		return nil, err
	}

	validatedItems, err := validateAIRouteItems(selectedRoute, inputModelSet)
	if err != nil {
		return nil, err
	}
	if len(validatedItems) == 0 {
		return nil, fmt.Errorf("AI 返回结果为空，未写入任何路由")
	}
	if tracker != nil {
		tracker.SetWritingGroup(g.Name)
	}

	addedCount, err := syncGroupItemsWithAIRoute(ctx, g.ID, selectedRoute.EndpointType, validatedItems)
	if err != nil {
		return nil, err
	}
	if tracker != nil {
		tracker.SetFinalizing("已写入当前分组，正在完成任务")
	}

	log.Infof("ai route generated successfully: group_id=%d routes=%d validated_items=%d added_items=%d",
		g.ID, len(routes), len(validatedItems), addedCount)

	return &model.GenerateAIRouteResult{
		Scope:      model.AIRouteScopeGroup,
		GroupID:    g.ID,
		GroupCount: 1,
		RouteCount: len(routes),
		ItemCount:  addedCount,
	}, nil
}

func generateAIRouteTable(
	ctx context.Context,
	modelInputs []model.AIRouteModelInput,
	inputModelSet map[int]map[string]struct{},
	tracker *aiRouteProgressTracker,
) (*model.GenerateAIRouteResult, error) {
	routes, routesErr := generateAIRoutesFromModelList(ctx, modelInputs, "", "", tracker)
	if routesErr != nil && len(routes) == 0 {
		return nil, routesErr
	}
	if tracker != nil {
		tracker.SetValidatingRoutes(len(routes))
	}

	existingGroups, err := group.GroupList(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取现有分组失败: %w", err)
	}

	routes, corrections := autoCorrectAIRouteTableRoutes(routes, existingGroups)
	for _, correction := range corrections {
		log.Warnf("ai route auto-corrected conflicting route name: requested_model=%q endpoint=%s corrected=%q",
			correction.OriginalName,
			correction.EndpointType,
			correction.CorrectedName,
		)
	}
	if err := validateAIRouteTableRoutes(routes); err != nil {
		return nil, err
	}

	groupByName := make(map[string]model.Group, len(existingGroups))
	for _, group := range existingGroups {
		name := strings.ToLower(strings.TrimSpace(group.Name))
		if name == "" {
			continue
		}
		groupByName[name] = group
	}

	affectedGroups := 0
	addedItems := 0
	for i, route := range routes {
		if tracker != nil {
			tracker.SetWritingRoute(i+1, len(routes), route.RequestedModel)
		}
		validatedItems, err := validateAIRouteItems(route, inputModelSet)
		if err != nil {
			log.Warnf("ai route skipped invalid route: requested_model=%q err=%v", route.RequestedModel, err)
			continue
		}

		groupName := strings.TrimSpace(route.RequestedModel)
		if groupName == "" {
			continue
		}

		groupKey := strings.ToLower(groupName)
		if existing, ok := groupByName[groupKey]; ok {
			addedCount, err := syncGroupItemsWithAIRoute(ctx, existing.ID, route.EndpointType, validatedItems)
			if err != nil {
				log.Warnf("ai route failed to sync existing group: group=%q err=%v", existing.Name, err)
				continue
			}
			if updatedGroup, getErr := group.GroupGet(existing.ID, ctx); getErr == nil && updatedGroup != nil {
				groupByName[groupKey] = *updatedGroup
			}
			addedItems += addedCount
			affectedGroups++
			continue
		}

		createdGroup, addedCount, err := createAIRouteGroup(ctx, groupName, route.EndpointType, route.MatchRegex, validatedItems)
		if err != nil {
			log.Warnf("ai route failed to create group: group=%q err=%v", groupName, err)
			continue
		}

		groupByName[groupKey] = *createdGroup
		addedItems += addedCount
		affectedGroups++
	}

	if affectedGroups == 0 {
		if routesErr != nil {
			return nil, fmt.Errorf("%s；且未成功写入任何分组", routesErr.Error())
		}
		return nil, fmt.Errorf("AI 返回结果为空，未写入任何路由")
	}
	if tracker != nil {
		tracker.SetFinalizing("路由表写入完成，正在完成任务")
	}

	result := &model.GenerateAIRouteResult{
		Scope:      model.AIRouteScopeTable,
		GroupCount: affectedGroups,
		RouteCount: len(routes),
		ItemCount:  addedItems,
	}
	if routesErr != nil {
		log.Warnf("ai route table partially generated: routes=%d groups=%d added_items=%d err=%v",
			len(routes), affectedGroups, addedItems, routesErr)
		return result, newAIRouteTablePartialFailureError(affectedGroups, routesErr)
	}

	log.Infof("ai route table generated successfully: routes=%d groups=%d added_items=%d",
		len(routes), affectedGroups, addedItems)

	return result, nil
}

func collectAIRouteModelInputs(ctx context.Context) ([]model.AIRouteModelInput, map[int]map[string]struct{}, error) {
	channels, err := channel.List(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("收集模型列表失败: %w", err)
	}

	seen := make(map[string]struct{})
	result := make([]model.AIRouteModelInput, 0)
	modelSet := make(map[int]map[string]struct{})

	for _, channel := range channels {
		if !channel.Enabled {
			continue
		}

		for _, modelName := range xstrings.SplitTrimCompact(",", channel.Model, channel.CustomModel) {
			modelName = strings.TrimSpace(modelName)
			if modelName == "" {
				continue
			}

			key := fmt.Sprintf("%d\x00%s", channel.ID, strings.ToLower(modelName))
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}

			if _, ok := modelSet[channel.ID]; !ok {
				modelSet[channel.ID] = make(map[string]struct{})
			}
			modelSet[channel.ID][strings.ToLower(modelName)] = struct{}{}

			result = append(result, model.AIRouteModelInput{
				ChannelID:   channel.ID,
				ChannelName: channel.Name,
				Provider:    aiRouteProviderName(channel.Type),
				Model:       modelName,
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].ChannelID != result[j].ChannelID {
			return result[i].ChannelID < result[j].ChannelID
		}
		return result[i].Model < result[j].Model
	})

	return result, modelSet, nil
}

func generateAIRoutesFromModelList(
	ctx context.Context,
	modelInputs []model.AIRouteModelInput,
	targetGroupName string,
	targetPromptEndpointType string,
	tracker *aiRouteProgressTracker,
) ([]model.AIRouteEntry, error) {
	services, err := loadAIRouteServicesSnapshot()
	if err != nil {
		return nil, err
	}

	buckets := buildAIRoutePromptBuckets(modelInputs, targetPromptEndpointType)
	if len(buckets) == 0 {
		if targetPromptEndpointType != "" {
			return nil, fmt.Errorf("没有可分析的 %s 模型", airoutePromptEndpointLabel(targetPromptEndpointType))
		}
		return nil, fmt.Errorf("没有可分析的模型")
	}
	if tracker != nil {
		tracker.SetBuckets(buckets)
	}

	parallelism := clampAIRouteParallelism(loadAIRouteParallelism(), len(buckets))
	servicePool := newAIRouteServicePool(services)
	bucketResults := make([][]model.AIRouteEntry, len(buckets))
	sem := semaphore.NewWeighted(int64(parallelism))
	results := make(chan aiRouteBucketResult, len(buckets))
	var wg sync.WaitGroup

	for i := range buckets {
		bucketIndex := i
		bucket := buckets[i]
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				if err := sem.Acquire(ctx, 1); err != nil {
					results <- aiRouteBucketResult{Index: bucketIndex, Total: len(buckets), Err: err}
					return
				}

				routes, bucketErr := generateAIRoutesForBucket(
					ctx,
					servicePool,
					len(services),
					bucket,
					targetGroupName,
					bucketIndex+1,
					tracker,
				)

				var cooldownErr *aiRouteCooldownError
				if bucketErr != nil && errors.As(bucketErr, &cooldownErr) && cooldownErr.RetryAfter > 0 {
					sem.Release(1)
					log.Warnf("ai route bucket %d: all services in cooldown, sleeping %v before retry",
						bucketIndex+1, cooldownErr.RetryAfter.Round(time.Second))
					select {
					case <-ctx.Done():
						results <- aiRouteBucketResult{Index: bucketIndex, Total: len(buckets), Err: ctx.Err()}
						return
					case <-time.After(cooldownErr.RetryAfter):
					}
					continue
				}

				sem.Release(1)
				results <- aiRouteBucketResult{
					Index:  bucketIndex,
					Total:  len(buckets),
					Routes: routes,
					Err:    bucketErr,
				}
				return
			}
		}()
	}

	wg.Wait()
	close(results)

	failures := make([]aiRouteBucketFailure, 0)
	for result := range results {
		if result.Err != nil {
			failures = append(failures, aiRouteBucketFailure{
				Index: result.Index + 1,
				Total: result.Total,
				Err:   result.Err,
			})
			continue
		}
		bucketResults[result.Index] = result.Routes
	}

	allRoutes := make([]model.AIRouteEntry, 0)
	for i := range bucketResults {
		if len(bucketResults[i]) == 0 {
			continue
		}
		allRoutes = append(allRoutes, bucketResults[i]...)
	}

	normalizedRoutes := normalizeAIRouteEntries(allRoutes)
	if len(normalizedRoutes) == 0 {
		if len(failures) > 0 {
			return nil, summarizeAIRouteBucketFailures(failures)
		}
		return nil, fmt.Errorf("AI返回结果为空")
	}

	if len(failures) > 0 {
		return normalizedRoutes, summarizeAIRouteBucketFailures(failures)
	}

	return normalizedRoutes, nil
}

func summarizeAIRouteBucketFailures(failures []aiRouteBucketFailure) error {
	if len(failures) == 0 {
		return nil
	}

	sort.SliceStable(failures, func(i, j int) bool {
		return failures[i].Index < failures[j].Index
	})

	first := failures[0]
	if len(failures) == 1 {
		return fmt.Errorf("第 %d/%d 批 AI 分析失败：%w", first.Index, first.Total, first.Err)
	}

	return fmt.Errorf("%d 个 AI 分析批次失败，首个错误为第 %d/%d 批：%w",
		len(failures),
		first.Index,
		first.Total,
		first.Err,
	)
}

func newAIRouteTablePartialFailureError(successGroups int, cause error) error {
	if cause == nil {
		return nil
	}
	if successGroups <= 0 {
		return cause
	}
	return &AIRoutePartialFailureError{
		Message:    fmt.Sprintf("AI 路由部分失败，但已保留成功写入的 %d 个分组：%s", successGroups, cause.Error()),
		MessageKey: "group.aiRoute.progress.runtime.partialFailure",
		MessageArgs: map[string]any{
			"success_groups": successGroups,
			"cause":          cause.Error(),
		},
		Cause: cause,
	}
}

func generateAIRoutesForBucket(
	ctx context.Context,
	servicePool aiRouteServicePool,
	serviceCount int,
	bucket aiRoutePromptBucket,
	targetGroupName string,
	batchIndex int,
	tracker *aiRouteProgressTracker,
) ([]model.AIRouteEntry, error) {
	if servicePool == nil {
		return nil, fmt.Errorf("没有可用的 AI 路由分析服务")
	}
	if serviceCount <= 0 {
		return nil, fmt.Errorf("没有可用的 AI 路由分析服务")
	}

	hint := aiRouteServiceHint{
		PromptEndpointType: bucket.PromptEndpointType,
		BucketIndex:        batchIndex,
	}
	exclude := make(map[int]struct{}, serviceCount)

	lease, err := servicePool.Acquire(ctx, hint, nil)
	if err != nil {
		return nil, err
	}

	for attempt := 1; ; attempt++ {
		if tracker != nil {
			tracker.StartBatch(batchIndex, bucket, lease.Service.Name, attempt)
		}

		routes, callErr := generateAIRoutesForBucketWithService(
			ctx,
			lease.Service,
			bucket,
			targetGroupName,
			batchIndex,
			attempt,
			tracker,
		)
		if callErr == nil {
			servicePool.Release(lease, aiRouteServiceOutcome{Success: true})
			if tracker != nil {
				tracker.CompleteBatch(batchIndex, bucket, lease.Service.Name, attempt)
			}
			return routes, nil
		}

		outcome := aiRouteServiceOutcome{Err: callErr}
		var routeErr *aiRouteCallError
		if errors.As(callErr, &routeErr) && routeErr.Retryable && ctx.Err() == nil {
			outcome.Retryable = true
			outcome.Cooldown = routeErr.Cooldown
		}
		servicePool.Release(lease, outcome)

		if !outcome.Retryable {
			if tracker != nil {
				tracker.FailBatch(batchIndex, bucket, lease.Service.Name, attempt, callErr.Error())
			}
			return nil, callErr
		}

		if tracker != nil {
			tracker.MarkBatchRetry(batchIndex, bucket, lease.Service.Name, attempt, callErr.Error())
		}
		log.Warnf("ai route bucket retrying with next service: batch=%d service=%s attempt=%d err=%v",
			batchIndex, lease.Service.Name, attempt, callErr)

		exclude[lease.Index] = struct{}{}
		if len(exclude) >= serviceCount {
			if tracker != nil {
				tracker.FailBatch(batchIndex, bucket, lease.Service.Name, attempt, callErr.Error())
			}
			return nil, callErr
		}

		nextLease, nextErr := servicePool.Next(ctx, lease, hint, exclude, callErr)
		if nextErr != nil {
			// Propagate cooldown errors so the caller can release resources and retry
			var cooldownErr *aiRouteCooldownError
			if errors.As(nextErr, &cooldownErr) {
				return nil, cooldownErr
			}
			if tracker != nil {
				tracker.FailBatch(batchIndex, bucket, lease.Service.Name, attempt, callErr.Error())
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, callErr
		}
		lease = nextLease
	}
}
