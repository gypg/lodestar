package airoute

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/channel"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/utils/xstrings"
)

const (
	defaultAIRouteParallelism = 3
	maxAcquireCooldownWait    = 10 * time.Second
)

// aiRouteCooldownError is returned by Acquire when all eligible services are
// in cooldown and the wait exceeds maxAcquireCooldownWait. Callers should
// release any held resources, wait for the cooldown, then retry.
type aiRouteCooldownError struct {
	RetryAfter time.Duration
}

func (e *aiRouteCooldownError) Error() string {
	return fmt.Sprintf("all AI route services are in cooldown, retry after %v", e.RetryAfter.Round(time.Second))
}

type aiRouteService struct {
	Index   int
	Name    string
	BaseURL string
	APIKey  string
	Model   string
}

type aiRouteServiceHint struct {
	PromptEndpointType string
	BucketIndex        int
}

type aiRouteServiceLease struct {
	Index   int
	Service aiRouteService
}

type aiRouteServiceOutcome struct {
	Success   bool
	Retryable bool
	Cooldown  time.Duration
	Err       error
}

type aiRouteServicePool interface {
	Acquire(ctx context.Context, hint aiRouteServiceHint, exclude map[int]struct{}) (*aiRouteServiceLease, error)
	Next(ctx context.Context, prev *aiRouteServiceLease, hint aiRouteServiceHint, exclude map[int]struct{}, cause error) (*aiRouteServiceLease, error)
	Release(lease *aiRouteServiceLease, outcome aiRouteServiceOutcome)
}

type aiRouteServiceState struct {
	service       aiRouteService
	inflight      int
	cooldownUntil time.Time
}

type roundRobinAIRouteServicePool struct {
	mu       sync.Mutex
	cursor   int
	services []aiRouteServiceState
}

func newAIRouteServicePool(services []aiRouteService) aiRouteServicePool {
	states := make([]aiRouteServiceState, len(services))
	for i := range services {
		states[i] = aiRouteServiceState{service: services[i]}
	}
	return &roundRobinAIRouteServicePool{services: states}
}

func (p *roundRobinAIRouteServicePool) Acquire(
	ctx context.Context,
	hint aiRouteServiceHint,
	exclude map[int]struct{},
) (*aiRouteServiceLease, error) {
	for {
		now := time.Now()

		p.mu.Lock()
		idx, waitUntil, ok := p.pickLocked(now, exclude)
		if ok && idx >= 0 {
			p.services[idx].inflight++
			p.cursor = (idx + 1) % len(p.services)
			lease := &aiRouteServiceLease{
				Index:   idx,
				Service: p.services[idx].service,
			}
			p.mu.Unlock()
			return lease, nil
		}
		p.mu.Unlock()

		if !ok {
			return nil, fmt.Errorf("没有可用的 AI 路由分析服务（batch %d）", hint.BucketIndex)
		}

		wait := time.Until(waitUntil)
		if wait <= 0 {
			continue
		}
		if wait > maxAcquireCooldownWait {
			return nil, &aiRouteCooldownError{RetryAfter: wait}
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (p *roundRobinAIRouteServicePool) Next(
	ctx context.Context,
	_ *aiRouteServiceLease,
	hint aiRouteServiceHint,
	exclude map[int]struct{},
	_ error,
) (*aiRouteServiceLease, error) {
	return p.Acquire(ctx, hint, exclude)
}

func (p *roundRobinAIRouteServicePool) Release(lease *aiRouteServiceLease, outcome aiRouteServiceOutcome) {
	if p == nil || lease == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if lease.Index < 0 || lease.Index >= len(p.services) {
		return
	}

	state := &p.services[lease.Index]
	if state.inflight > 0 {
		state.inflight--
	}

	if !outcome.Success && outcome.Retryable && outcome.Cooldown > 0 {
		nextCooldown := time.Now().Add(outcome.Cooldown)
		if nextCooldown.After(state.cooldownUntil) {
			state.cooldownUntil = nextCooldown
		}
	}
}

func (p *roundRobinAIRouteServicePool) pickLocked(
	now time.Time,
	exclude map[int]struct{},
) (idx int, waitUntil time.Time, ok bool) {
	if len(p.services) == 0 {
		return -1, time.Time{}, false
	}

	bestIdx := -1
	bestInflight := 0
	hasEligible := false
	earliestCooldown := time.Time{}

	for offset := 0; offset < len(p.services); offset++ {
		idx = (p.cursor + offset) % len(p.services)
		if exclude != nil {
			if _, skipped := exclude[idx]; skipped {
				continue
			}
		}

		hasEligible = true
		state := p.services[idx]
		if state.cooldownUntil.After(now) {
			if earliestCooldown.IsZero() || state.cooldownUntil.Before(earliestCooldown) {
				earliestCooldown = state.cooldownUntil
			}
			continue
		}

		if bestIdx < 0 || state.inflight < bestInflight {
			bestIdx = idx
			bestInflight = state.inflight
		}
	}

	if bestIdx >= 0 {
		return bestIdx, time.Time{}, true
	}
	if hasEligible {
		return -1, earliestCooldown, true
	}
	return -1, time.Time{}, false
}

func loadAIRouteParallelism() int {
	value, err := setting.GetInt(model.SettingKeyAIRouteParallelism)
	if err != nil || value < 1 {
		return defaultAIRouteParallelism
	}
	return value
}

func clampAIRouteParallelism(value int, bucketCount int) int {
	if bucketCount <= 0 {
		return 1
	}
	if value < 1 {
		value = defaultAIRouteParallelism
	}
	if value > bucketCount {
		return bucketCount
	}
	return value
}

func loadAIRouteServicesSnapshot() ([]aiRouteService, error) {
	configs, err := loadAIRouteServiceConfigs()
	if err != nil {
		return nil, err
	}

	services := make([]aiRouteService, 0, len(configs))
	for i, cfg := range configs {
		if !cfg.IsEnabled() {
			continue
		}

		services = append(services, aiRouteService{
			Index:   i,
			Name:    NormalizeAIRouteServiceName(cfg, i+1),
			BaseURL: strings.TrimSpace(cfg.BaseURL),
			APIKey:  strings.TrimSpace(cfg.APIKey),
			Model:   strings.TrimSpace(cfg.Model),
		})
	}

	if len(services) == 0 {
		return nil, fmt.Errorf("没有可用的 AI 路由分析服务")
	}
	return services, nil
}

func loadAIRouteServiceConfigs() ([]model.AIRouteServiceConfig, error) {
	rawServices, _ := setting.GetString(model.SettingKeyAIRouteServices)
	rawServices = strings.TrimSpace(rawServices)
	if rawServices != "" && rawServices != "[]" {
		var services []model.AIRouteServiceConfig
		if err := json.Unmarshal([]byte(rawServices), &services); err != nil {
			return nil, fmt.Errorf("AI路由服务池配置不完整")
		}
		return services, nil
	}

	// Local mode derives the analysis service straight from this site's own
	// channels, so the operator never hand-fills base_url/api_key for it and a
	// bare model choice is enough to run. Checked after the explicit service
	// pool (which is an explicit operator override) and before the legacy
	// three-piece credentials, which only external mode should need.
	if mode, _ := setting.GetString(model.SettingKeyAIRouteSourceMode); strings.TrimSpace(mode) == model.AIRouteSourceModeLocal {
		return loadLocalAIRouteServices()
	}

	baseURL, err := setting.GetString(model.SettingKeyAIRouteBaseURL)
	if err != nil || strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("AI路由模型配置不完整")
	}

	apiKey, err := setting.GetString(model.SettingKeyAIRouteAPIKey)
	if err != nil || strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("AI路由模型配置不完整")
	}

	modelName, err := setting.GetString(model.SettingKeyAIRouteModel)
	if err != nil || strings.TrimSpace(modelName) == "" {
		return nil, fmt.Errorf("AI路由模型配置不完整")
	}

	enabled := true
	return []model.AIRouteServiceConfig{{
		Name:    "legacy",
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   modelName,
		Enabled: &enabled,
	}}, nil
}

func NormalizeAIRouteServiceName(cfg model.AIRouteServiceConfig, ordinal int) string {
	if name := strings.TrimSpace(cfg.Name); name != "" {
		return name
	}

	if parsed, err := url.Parse(strings.TrimSpace(cfg.BaseURL)); err == nil && parsed.Host != "" {
		if modelName := strings.TrimSpace(cfg.Model); modelName != "" {
			return fmt.Sprintf("%s (%s)", parsed.Host, modelName)
		}
		return parsed.Host
	}

	return fmt.Sprintf("service-%d", ordinal)
}

// loadLocalAIRouteServices builds the analysis service from this site's own
// channels. The channel cache is loaded with Preload("Keys"), so List already
// carries base URLs and keys — no extra queries.
func loadLocalAIRouteServices() ([]model.AIRouteServiceConfig, error) {
	channels, err := channel.List(context.Background())
	if err != nil {
		return nil, fmt.Errorf("读取本站渠道失败: %w", err)
	}

	preferred, _ := setting.GetString(model.SettingKeyAIRouteModel)
	return buildLocalAIRouteServices(channels, strings.TrimSpace(preferred))
}

// buildLocalAIRouteServices picks the analysis service from the given channels.
//
// With a preferred model it must be served by an enabled channel; silently
// swapping in a different model would run the analysis with credentials the
// operator never saw, so a missing mapping is an error naming the model.
// With no preference (setting never chosen) the first usable model in channel
// order wins — deterministic, and only a fallback: the panel auto-picks the
// lowest-latency model into the setting well before the task runs.
func buildLocalAIRouteServices(channels []model.Channel, preferredModel string) ([]model.AIRouteServiceConfig, error) {
	if preferredModel != "" {
		for i := range channels {
			if svc, ok := localServiceFromChannel(&channels[i], preferredModel); ok {
				return []model.AIRouteServiceConfig{svc}, nil
			}
		}
		return nil, fmt.Errorf("本站渠道中没有支持模型 %q 的启用渠道（需启用且带地址与密钥）", preferredModel)
	}

	for i := range channels {
		ch := &channels[i]
		if !ch.Enabled {
			continue
		}
		for _, modelName := range xstrings.SplitTrimCompact(",", ch.Model, ch.CustomModel) {
			if svc, ok := localServiceFromChannel(ch, modelName); ok {
				return []model.AIRouteServiceConfig{svc}, nil
			}
		}
	}
	return nil, fmt.Errorf("本站渠道中没有可用的分析模型（需启用且带地址与密钥）")
}

// localServiceFromChannel turns a channel into an analysis service for the
// given model. Matching is case-insensitive against the channel's comma list,
// but the service carries the channel's own spelling — that is the name the
// upstream actually knows. Requires a non-empty base URL and one enabled key.
func localServiceFromChannel(ch *model.Channel, modelName string) (model.AIRouteServiceConfig, bool) {
	if ch == nil || !ch.Enabled {
		return model.AIRouteServiceConfig{}, false
	}
	target := strings.ToLower(strings.TrimSpace(modelName))
	if target == "" {
		return model.AIRouteServiceConfig{}, false
	}

	servedModel := ""
	for _, served := range xstrings.SplitTrimCompact(",", ch.Model, ch.CustomModel) {
		if strings.ToLower(served) == target {
			servedModel = served
			break
		}
	}
	if servedModel == "" {
		return model.AIRouteServiceConfig{}, false
	}

	baseURL := ""
	for _, base := range ch.BaseUrls {
		if trimmed := strings.TrimSpace(base.URL); trimmed != "" {
			baseURL = trimmed
			break
		}
	}

	apiKey := ""
	for _, key := range ch.Keys {
		if key.Enabled && strings.TrimSpace(key.ChannelKey) != "" {
			apiKey = strings.TrimSpace(key.ChannelKey)
			break
		}
	}

	if baseURL == "" || apiKey == "" {
		return model.AIRouteServiceConfig{}, false
	}

	name := strings.TrimSpace(ch.Name)
	if name == "" {
		name = fmt.Sprintf("local-channel-%d", ch.ID)
	}

	return model.AIRouteServiceConfig{
		Name:    fmt.Sprintf("%s (%s)", name, servedModel),
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   servedModel,
	}, true
}
