// Package sub2api implements the SiteAdapter for Sub2API-type remote sites.
// Sub2API uses a JWT access+refresh token auth flow with {code, message, data} envelope.
package sub2api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gypg/lodestar/internal/hub"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/utils/crypto"
)

func init() {
	hub.Register(model.SiteTypeSub2API, &Adapter{})
}

// Adapter implements hub.SiteAdapter for Sub2API-type remote sites.
type Adapter struct{}

const (
	// quotaPerUSD converts Sub2API's USD amounts into Lodestar quota units.
	quotaPerUSD = 500000
	// sub2apiPageSize is the page size used for paginated Sub2API endpoints.
	// The server caps page_size at 1000; 100 keeps responses small.
	sub2apiPageSize = 100
	// sub2apiMaxPages bounds pagination so a misbehaving remote cannot spin
	// this loop forever.
	sub2apiMaxPages = 100
)

// ── JWT auth with refresh token flow ────────────────────────────────────────

type cachedToken struct {
	accessToken  string
	refreshToken string
	expiresAt    time.Time
}

var (
	tokenMu    sync.Mutex
	tokenCache = make(map[string]*cachedToken) // key: "baseURL:username"
)

// cleanupTokenCache removes expired entries to prevent unbounded growth.
// Must be called with tokenMu held.
func cleanupTokenCache() {
	now := time.Now()
	for k, v := range tokenCache {
		if now.After(v.expiresAt) {
			delete(tokenCache, k)
		}
	}
}

// sub2apiEnvelope is the response envelope used by Sub2API: {code: 0, message, data}.
type sub2apiEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func cacheKey(site *model.RemoteSite) string {
	return strings.TrimRight(site.BaseURL, "/") + ":" + site.Username
}

// getValidToken returns a valid access token, refreshing if needed.
// Sub2API stores access_token in site.AccessToken (encrypted) and refresh_token in site.Password (encrypted).
func getValidToken(ctx context.Context, site *model.RemoteSite) (string, error) {
	key := cacheKey(site)

	tokenMu.Lock()
	defer tokenMu.Unlock()

	if cached, ok := tokenCache[key]; ok {
		// Proactive refresh: if within 120 seconds of expiry
		if time.Now().Add(120 * time.Second).Before(cached.expiresAt) {
			return cached.accessToken, nil
		}
		// Try refresh token flow
		newToken, err := refreshToken(ctx, site, cached.refreshToken)
		if err == nil {
			tokenCache[key] = newToken
			cleanupTokenCache()
			return newToken.accessToken, nil
		}
		// Refresh failed, fall through to try stored token
	}

	// Use stored access token
	accessToken, err := crypto.Decrypt(site.AccessToken)
	if err != nil {
		return "", fmt.Errorf("decrypt access token: %w", err)
	}

	// Try stored refresh token to get a fresh access token
	refreshTokenStr, _ := crypto.Decrypt(site.Password) // Password field stores refresh token
	if refreshTokenStr != "" {
		newToken, err := refreshToken(ctx, site, refreshTokenStr)
		if err == nil {
			tokenCache[key] = newToken
			cleanupTokenCache()
			return newToken.accessToken, nil
		}
	}

	// Fallback to stored access token
	tokenCache[key] = &cachedToken{
		accessToken:  accessToken,
		refreshToken: refreshTokenStr,
		expiresAt:    time.Now().Add(5 * time.Minute), // assume 5 min validity
	}
	cleanupTokenCache()
	return accessToken, nil
}

type refreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // seconds
}

func refreshToken(ctx context.Context, site *model.RemoteSite, refreshTokenStr string) (*cachedToken, error) {
	body, _ := json.Marshal(map[string]string{
		"refresh_token": refreshTokenStr,
	})

	url := strings.TrimRight(site.BaseURL, "/") + "/api/v1/auth/refresh"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := hub.AdapterHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var envelope sub2apiEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("parse refresh response: %w", err)
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("refresh failed: %s", envelope.Message)
	}

	var data refreshResponse
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return nil, fmt.Errorf("parse refresh data: %w", err)
	}

	expiresIn := data.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 300 // default 5 min
	}

	return &cachedToken{
		accessToken:  data.AccessToken,
		refreshToken: data.RefreshToken,
		expiresAt:    time.Now().Add(time.Duration(expiresIn) * time.Second),
	}, nil
}

// sub2apiFetch performs a JSON API call with JWT auth and {code, message, data} envelope.
func sub2apiFetch[T any](ctx context.Context, site *model.RemoteSite, method, endpoint string, reqBody interface{}) (T, error) {
	var zero T

	token, err := getValidToken(ctx, site)
	if err != nil {
		return zero, fmt.Errorf("auth: %w", err)
	}

	var body io.Reader
	if reqBody != nil {
		b, _ := json.Marshal(reqBody)
		body = strings.NewReader(string(b))
	}

	url := strings.TrimRight(site.BaseURL, "/") + endpoint
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return zero, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := hub.AdapterHTTPClient.Do(req)
	if err != nil {
		return zero, fmt.Errorf("request %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return zero, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, endpoint, truncate(string(respBody), 200))
	}

	var envelope sub2apiEnvelope
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return zero, fmt.Errorf("parse response from %s: %w", endpoint, err)
	}
	if envelope.Code != 0 {
		return zero, fmt.Errorf("API error from %s (code %d): %s", endpoint, envelope.Code, envelope.Message)
	}

	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return zero, nil
	}

	var result T
	if err := json.Unmarshal(envelope.Data, &result); err != nil {
		return zero, fmt.Errorf("unmarshal data: %w", err)
	}
	return result, nil
}

// ── SiteAdapter implementation ──────────────────────────────────────────────

type authMeResponse struct {
	ID       int     `json:"id"`
	Username string  `json:"username"`
	Email    string  `json:"email"`
	Balance  float64 `json:"balance"` // USD
}

func (a *Adapter) FetchUserInfo(ctx context.Context, site *model.RemoteSite) (*hub.UserInfoResult, error) {
	me, err := sub2apiFetch[authMeResponse](ctx, site, http.MethodGet, "/api/v1/auth/me", nil)
	if err != nil {
		return nil, err
	}
	// Sub2API balance is in USD, convert to Lodestar quota units.
	return &hub.UserInfoResult{
		ID:       me.ID,
		Username: me.Username,
		Email:    me.Email,
		Quota:    me.Balance * quotaPerUSD,
	}, nil
}

func (a *Adapter) FetchCheckInStatus(_ context.Context, _ *model.RemoteSite) (*bool, error) {
	return nil, nil // Sub2API does not support check-in
}

func (a *Adapter) PerformCheckIn(_ context.Context, _ *model.RemoteSite) (*hub.CheckInResult, error) {
	return &hub.CheckInResult{Success: false, Message: "check-in not supported"}, nil
}

func (a *Adapter) FetchModels(_ context.Context, _ *model.RemoteSite) ([]string, error) {
	return nil, nil // Sub2API does not expose a model list endpoint
}

func (a *Adapter) FetchModelPricing(_ context.Context, _ *model.RemoteSite) ([]hub.ModelPricingEntry, error) {
	return nil, nil
}

// ── Tokens ──────────────────────────────────────────────────────────────────

// sub2apiKey mirrors Sub2API's dto.APIKey. Note that `status` is a string
// ("active"/"disabled"/"expired"/"inactive") and `expires_at` is an ISO-8601
// timestamp, not the numeric forms used by New-API style sites.
type sub2apiKey struct {
	ID        int     `json:"id"`
	UserID    int     `json:"user_id"`
	Key       string  `json:"key"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	Quota     float64 `json:"quota"`      // USD, 0 = unlimited
	QuotaUsed float64 `json:"quota_used"` // USD
	ExpiresAt *string `json:"expires_at"` // ISO-8601, nil = never expires
	CreatedAt string  `json:"created_at"`
}

// sub2apiTokenStatus maps Sub2API's string status onto the numeric convention
// used across Lodestar adapters (1 = enabled, 2 = disabled/unusable).
func sub2apiTokenStatus(s string) int {
	if strings.EqualFold(s, "active") {
		return 1
	}
	return 2
}

type sub2apiKeyList struct {
	Items []sub2apiKey `json:"items"`
	Total int          `json:"total"`
}

func (a *Adapter) FetchTokens(ctx context.Context, site *model.RemoteSite) ([]hub.RemoteToken, error) {
	var allTokens []hub.RemoteToken
	// Sub2API pagination is 1-based: `page=0` is rejected by the server and
	// silently falls back to page 1, so starting at 0 would re-read page 1.
	for page := 1; page <= sub2apiMaxPages; page++ {
		endpoint := fmt.Sprintf("/api/v1/keys?page=%d&page_size=%d", page, sub2apiPageSize)
		list, err := sub2apiFetch[sub2apiKeyList](ctx, site, http.MethodGet, endpoint, nil)
		if err != nil {
			// Try flat array response
			tokens, err2 := sub2apiFetch[[]sub2apiKey](ctx, site, http.MethodGet, endpoint, nil)
			if err2 != nil {
				return nil, fmt.Errorf("fetch tokens: %w (fallback: %w)", err, err2)
			}
			list.Items = tokens
		}
		for _, t := range list.Items {
			unlimited := t.Quota == 0
			// Sub2API reports the quota cap and the amount already spent;
			// the remaining balance is the difference, not the cap itself.
			remainUSD := t.Quota - t.QuotaUsed
			if unlimited || remainUSD < 0 {
				remainUSD = 0
			}
			allTokens = append(allTokens, hub.RemoteToken{
				ID:             t.ID,
				Name:           t.Name,
				Key:            t.Key,
				Status:         sub2apiTokenStatus(t.Status),
				RemainQuota:    remainUSD * quotaPerUSD,
				UsedQuota:      t.QuotaUsed * quotaPerUSD,
				UnlimitedQuota: unlimited,
				ExpiredTime:    parseSub2APITime(derefString(t.ExpiresAt)),
				CreatedTime:    parseSub2APITime(t.CreatedAt),
			})
		}
		if len(list.Items) < sub2apiPageSize {
			break
		}
	}
	return allTokens, nil
}

func (a *Adapter) CreateToken(ctx context.Context, site *model.RemoteSite, req hub.CreateTokenRequest) error {
	payload := map[string]interface{}{
		"name": req.Name,
	}
	if req.ExpiredTime > 0 {
		payload["expires_in_days"] = int(time.Until(time.Unix(req.ExpiredTime, 0)).Hours() / 24)
	}
	if req.UnlimitedQuota {
		payload["quota"] = 0
	} else if req.RemainQuota > 0 {
		payload["quota"] = float64(req.RemainQuota) / quotaPerUSD // quota to USD
	}
	_, err := sub2apiFetch[interface{}](ctx, site, http.MethodPost, "/api/v1/keys", payload)
	return err
}

// ── Channels (not supported) ────────────────────────────────────────────────

func (a *Adapter) ListChannels(_ context.Context, _ *model.RemoteSite) ([]hub.RemoteChannel, error) {
	return nil, nil
}

func (a *Adapter) CreateChannel(_ context.Context, _ *model.RemoteSite, _ hub.RemoteChannelCreateReq) error {
	return fmt.Errorf("channel management not supported for Sub2API sites")
}

func (a *Adapter) UpdateChannel(_ context.Context, _ *model.RemoteSite, _ hub.RemoteChannelUpdateReq) error {
	return fmt.Errorf("channel management not supported for Sub2API sites")
}

func (a *Adapter) DeleteChannel(_ context.Context, _ *model.RemoteSite, _ int) error {
	return fmt.Errorf("channel management not supported for Sub2API sites")
}

// ── Announcements ───────────────────────────────────────────────────────────

type sub2apiAnnouncement struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Message string `json:"message"`
	Body    string `json:"body"`
}

func (a *Adapter) FetchAnnouncement(ctx context.Context, site *model.RemoteSite) (string, error) {
	announcements, err := sub2apiFetch[[]sub2apiAnnouncement](ctx, site, http.MethodGet, "/api/v1/announcements", nil)
	if err != nil {
		return "", err
	}
	if len(announcements) == 0 {
		return "", nil
	}
	// Return the most recent announcement
	a0 := announcements[0]
	content := a0.Content
	if content == "" {
		content = a0.Body
	}
	if content == "" {
		content = a0.Message
	}
	if a0.Title != "" {
		content = a0.Title + "\n\n" + content
	}
	return content, nil
}

// ── Status ──────────────────────────────────────────────────────────────────

func (a *Adapter) FetchSiteStatus(_ context.Context, _ *model.RemoteSite) (*hub.SiteStatusInfo, error) {
	return &hub.SiteStatusInfo{
		SystemName:     "Sub2API",
		CheckInEnabled: false,
	}, nil
}

func (a *Adapter) RedeemCode(_ context.Context, _ *model.RemoteSite, _ string) (*hub.RedeemResult, error) {
	return nil, nil
}

// sub2apiUsageList is the paginated response from GET /api/v1/usage (user-facing).
type sub2apiUsageList struct {
	Items []sub2apiUsageEntry `json:"items"`
	Total int                 `json:"total"`
}

// sub2apiUsageEntry represents a single usage record returned by Sub2API.
// Real responses from /api/v1/usage are nested for api_key and group.
type sub2apiUsageEntry struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	APIKeyID  int64  `json:"api_key_id"`
	AccountID int64  `json:"account_id"`
	RequestID string `json:"request_id"`
	Model     string `json:"model"`

	RequestedModel *string `json:"requested_model,omitempty"`
	UpstreamModel  *string `json:"upstream_model,omitempty"`

	GroupID        *int64 `json:"group_id,omitempty"`
	SubscriptionID *int64 `json:"subscription_id,omitempty"`

	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheReadTokens     int `json:"cache_read_tokens"`
	CacheCreationTokens int `json:"cache_creation_tokens"`

	TotalTokens int `json:"total_tokens"`

	InputCost         float64 `json:"input_cost"`
	OutputCost        float64 `json:"output_cost"`
	CacheReadCost     float64 `json:"cache_read_cost"`
	CacheCreationCost float64 `json:"cache_creation_cost"`
	TotalCost         float64 `json:"total_cost"`
	ActualCost        float64 `json:"actual_cost"`

	BillingType int8   `json:"billing_type"`
	BillingMode string `json:"billing_mode,omitempty"`

	RequestType  string `json:"request_type"`
	Stream       bool   `json:"stream"`
	OpenAIWSMode bool   `json:"openai_ws_mode"`

	DurationMs   *int `json:"duration_ms,omitempty"`
	FirstTokenMs *int `json:"first_token_ms,omitempty"`

	ImageCount int `json:"image_count"`
	VideoCount int `json:"video_count"`

	CreatedAt string `json:"created_at"`

	// Nested objects present in real Sub2API responses (user-facing usage).
	APIKey *struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"api_key,omitempty"`

	Group *struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"group,omitempty"`
}

func (a *Adapter) FetchUsageLogs(ctx context.Context, site *model.RemoteSite, page, pageSize int) ([]hub.RemoteUsageLog, error) {
	// Sub2API pagination is 1-based; callers use 0-based page indexes.
	if page < 0 {
		page = 0
	}
	if pageSize <= 0 || pageSize > sub2apiPageSize {
		pageSize = sub2apiPageSize
	}

	endpoint := fmt.Sprintf("/api/v1/usage?page=%d&page_size=%d", page+1, pageSize)

	list, err := sub2apiFetch[sub2apiUsageList](ctx, site, http.MethodGet, endpoint, nil)
	if err != nil {
		// Try flat array response as fallback
		flat, err2 := sub2apiFetch[[]sub2apiUsageEntry](ctx, site, http.MethodGet, endpoint, nil)
		if err2 != nil {
			return nil, fmt.Errorf("fetch usage logs: %w (fallback: %w)", err, err2)
		}
		list.Items = flat
	}

	out := make([]hub.RemoteUsageLog, 0, len(list.Items))
	for _, e := range list.Items {
		createdAt := parseSub2APITime(e.CreatedAt)

		totalTokens := int64(e.TotalTokens)
		if totalTokens == 0 {
			totalTokens = int64(e.InputTokens + e.OutputTokens + e.CacheReadTokens + e.CacheCreationTokens)
		}

		// USD cost to Lodestar quota, consistent with RedeemCode
		quota := e.TotalCost * quotaPerUSD

		// Extract TokenName from nested api_key.name (real Sub2API behavior).
		tokenName := ""
		if e.APIKey != nil {
			tokenName = e.APIKey.Name
		}

		// Prefer nested group.name when available; fall back to group_id as string.
		groupName := ""
		if e.Group != nil && e.Group.Name != "" {
			groupName = e.Group.Name
		} else {
			groupName = derefInt64ToString(e.GroupID)
		}

		out = append(out, hub.RemoteUsageLog{
			ID:               e.ID,
			CreatedAt:        createdAt,
			ModelName:        e.Model,
			TokenName:        tokenName,
			PromptTokens:     int64(e.InputTokens),
			CompletionTokens: int64(e.OutputTokens),
			TotalTokens:      totalTokens,
			Quota:            quota,

			CacheReadTokens:     int64(e.CacheReadTokens),
			CacheCreationTokens: int64(e.CacheCreationTokens),
			RequestedModel:      derefString(e.RequestedModel),
			UpstreamModel:       derefString(e.UpstreamModel),
			Group:               groupName,
			SubscriptionID:      derefInt64ToString(e.SubscriptionID),
		})
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// parseSub2APITime converts a Sub2API time field (string) to unix seconds.
// It accepts RFC3339, unix seconds, or unix milliseconds.
func parseSub2APITime(s string) int64 {
	if s == "" {
		return 0
	}
	// RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix()
	}
	// unix seconds or millis as string
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n > 1_000_000_000_000 { // looks like millis
			return n / 1000
		}
		return n
	}
	return 0
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefInt64ToString(p *int64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatInt(*p, 10)
}
