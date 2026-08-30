package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/helper"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	ch "github.com/gypg/lodestar/internal/op/channel"
	st "github.com/gypg/lodestar/internal/op/stats"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
	"github.com/gypg/lodestar/internal/task"
	"github.com/gypg/lodestar/internal/transformer/outbound"
	"github.com/gypg/lodestar/internal/utils/log"
	"github.com/gypg/lodestar/internal/utils/secretmask"
)

func init() {
	router.NewGroupRouter("/api/v1/channel").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermChannelsRead)).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listChannel),
		).
		AddRoute(
			router.NewRoute("/create", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(createChannel),
		).
		AddRoute(
			router.NewRoute("/update", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(updateChannel),
		).
		AddRoute(
			router.NewRoute("/enable", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(enableChannel),
		).
		AddRoute(
			router.NewRoute("/delete/:id", http.MethodDelete).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(deleteChannel),
		).
		AddRoute(
			router.NewRoute("/fetch-model", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(fetchModel),
		).
		AddRoute(
			router.NewRoute("/test", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(testChannel),
		).
		AddRoute(
			router.NewRoute("/check-keys/:id", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(checkChannelKeys),
		).
		AddRoute(
			router.NewRoute("/group/list", http.MethodGet).
				Handle(listChannelGroup),
		).
		AddRoute(
			router.NewRoute("/group/create", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(createChannelGroup),
		).
		AddRoute(
			router.NewRoute("/group/update", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(updateChannelGroup),
		).
		AddRoute(
			router.NewRoute("/group/delete/:id", http.MethodDelete).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(deleteChannelGroup),
		).
		// Registered last on purpose: a bare /:id sits at the same level as the
		// static /list and /group/list routes above. gin resolves static
		// segments first so they still win, but keeping the wildcard at the
		// bottom makes that precedence obvious to the next reader.
		AddRoute(
			router.NewRoute("/:id", http.MethodGet).
				Handle(getChannel),
		)
	router.NewGroupRouter("/api/v1/channel").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermChannelsRead)).
		AddRoute(
			router.NewRoute("/sync", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(syncChannel),
		).
		AddRoute(
			router.NewRoute("/last-sync-time", http.MethodGet).
				Handle(getLastSyncTime),
		)
}

func listChannel(c *gin.Context) {
	channels, err := ch.List(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	role := c.GetString("user_role")
	canViewRawKeys := auth.HasPermission(role, auth.PermChannelsWrite)
	if isViewerRole(role) {
		redactChannelBaseURLsForViewer(channels)
	}
	for i, channel := range channels {
		if !canViewRawKeys {
			channels[i].Keys = maskChannelKeys(channel.Keys)
		}
		normalizeChannelListSlices(&channels[i])
		stats := st.ChannelGet(channel.ID)
		channels[i].Stats = &stats
	}
	resp.Success(c, channels)
}

// getChannel returns a single channel with full detail. This backs
// AIRouteConfig's fallback: when the cached /channel/list entry for the model's
// channel carries no usable base_urls/keys, the config dialog refetches the
// channel by id so it can still auto-fill the AI route settings. Parity with
// listChannel: viewers get masked keys and redacted base URLs, so this
// endpoint cannot be used to bypass the list's redaction.
func getChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	channel, err := ch.Get(id, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusNotFound, resp.ErrResourceNotFound)
		return
	}
	role := c.GetString("user_role")
	// Redact through a one-element slice and read the result back out: the
	// redactor now REPLACES each BaseUrls slice (it must not write through to
	// the shared cache backing array), so passing a throwaway literal would
	// silently drop the masking.
	if isViewerRole(role) {
		wrapped := []model.Channel{*channel}
		redactChannelBaseURLsForViewer(wrapped)
		channel = &wrapped[0]
	}
	if !auth.HasPermission(role, auth.PermChannelsWrite) {
		channel.Keys = maskChannelKeys(channel.Keys)
	}
	normalizeChannelListSlices(channel)
	resp.Success(c, channel)
}

func createChannel(c *gin.Context) {
	var req channelRequestPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	channel := req.toChannel()
	// Channel.Enabled 与 Keys 的 Enabled 都是 default:true 裸 bool：Create 前快照
	// 显式停用意图（create 回调会把结构体字段回写成 true，意图之后不可恢复）。
	channelEnabledIntent := req.Enabled
	channelExplicitlyDisabled := req.EnabledSet && !req.Enabled
	// Enabled 的 default:true 标签会让 create 回调把结构体里的 Enabled=false
	// 回写成 true（意图在 Create 之后不可恢复），必须在 Create 之前快照
	// 显式停用意图，事后按回填的 ID 用 UPDATE 补写停用态并刷新缓存。
	// 补偿失败不回滚已创建的渠道，但必须大声记日志——静默启用正是本缺陷
	// 要消灭的症状。
	explicitlyDisabledIdx := make([]int, 0)
	for i, key := range channel.Keys {
		if key.EnabledSet && !key.Enabled {
			explicitlyDisabledIdx = append(explicitlyDisabledIdx, i)
		}
	}
	if err := ch.Create(&channel, c.Request.Context()); err != nil {
		if status, msg, ok := classifyChannelMutationError(err); ok {
			resp.Error(c, status, msg)
			return
		}
		resp.InternalError(c)
		return
	}
	disabledKeyIDs := make([]int, 0)
	for _, idx := range explicitlyDisabledIdx {
		if channel.Keys[idx].ID != 0 {
			disabledKeyIDs = append(disabledKeyIDs, channel.Keys[idx].ID)
			channel.Keys[idx].Enabled = false // 回写被 create 回调篡改的字段，保证响应体如实
		}
	}
	if len(disabledKeyIDs) > 0 {
		if err := ch.KeysForceDisabled(channel.ID, disabledKeyIDs, c.Request.Context()); err != nil {
			log.Errorf("createChannel: failed to persist explicitly disabled keys (channel_id=%d): %v", channel.ID, err)
		}
	}
	// 与上面 Keys 补偿的先后无约束：ch.Enabled 先写 DB 再写 chCache，所以
	// KeysForceDisabled 里的 RefreshCacheByID 无论在其前后重载，读到的都是
	// 已修正的 DB 值。两种顺序终态一致，由 combined 测试守着。
	if channelExplicitlyDisabled {
		if err := ch.Enabled(channel.ID, false, c.Request.Context()); err != nil {
			log.Errorf("createChannel: failed to persist explicitly disabled channel (id=%d): %v", channel.ID, err)
		} else {
			channel.Enabled = channelEnabledIntent // 响应体如实
		}
	}
	stats := st.ChannelGet(channel.ID)
	channel.Stats = &stats
	go func(channel *model.Channel) {
		defer func() {
			if r := recover(); r != nil {
				log.Warnf("post-create channel async task panic recovered: channel_id=%d panic=%v", channel.ID, r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		modelStr := channel.Model + "," + channel.CustomModel
		modelArray := strings.Split(modelStr, ",")
		helper.LLMPriceAddToDB(modelArray, ctx)
		helper.ChannelBaseUrlDelayUpdate(channel, ctx)
		helper.ChannelAutoGroup(channel, ctx)
	}(&channel)
	resp.Success(c, channel)
}

func updateChannel(c *gin.Context) {
	var req model.ChannelUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	channel, err := ch.Update(&req, c.Request.Context())
	if err != nil {
		if status, msg, ok := classifyChannelMutationError(err); ok {
			resp.Error(c, status, msg)
			return
		}
		resp.InternalError(c)
		return
	}
	stats := st.ChannelGet(channel.ID)
	channel.Stats = &stats
	go func(channel *model.Channel) {
		defer func() {
			if r := recover(); r != nil {
				log.Warnf("post-update channel async task panic recovered: channel_id=%d panic=%v", channel.ID, r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		modelStr := channel.Model + "," + channel.CustomModel
		modelArray := strings.Split(modelStr, ",")
		helper.LLMPriceAddToDB(modelArray, ctx)
		helper.ChannelBaseUrlDelayUpdate(channel, ctx)
		helper.ChannelAutoGroup(channel, ctx)
	}(channel)
	resp.Success(c, channel)
}

func enableChannel(c *gin.Context) {
	var request struct {
		ID      int  `json:"id"`
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := ch.Enabled(request.ID, request.Enabled, c.Request.Context()); err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func deleteChannel(c *gin.Context) {
	id := c.Param("id")
	idNum, err := strconv.Atoi(id)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	if err := ch.Delete(idNum, c.Request.Context()); err != nil {
		resp.InternalError(c)
		return
	}
	st.OnChannelDeleted(idNum)
	resp.Success(c, nil)
}
func fetchModel(c *gin.Context) {
	var payload channelRequestPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	request := payload.toChannel()
	models, err := helper.FetchModels(c.Request.Context(), request)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, models)
}

func testChannel(c *gin.Context) {
	var payload channelRequestPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	request := payload.toChannel()
	summary, err := helper.TestChannel(c.Request.Context(), request)
	if err != nil {
		log.Errorf("testChannel failed: %v", err)
		resp.Error(c, http.StatusBadRequest, "Channel test failed")
		return
	}
	resp.Success(c, summary)
}

func syncChannel(c *gin.Context) {
	task.SyncModelsTask()
	resp.Success(c, nil)
}

// checkChannelKeys 针对已保存的渠道，按其当前的 base_urls × keys 组合做一次连通性
// 探测，返回与 /test 相同的汇总结构。前端据此判断"全部 key 是否都不可用"
// （summary.passed === false 即代表没有任何可用组合）。只读操作，不回写 key 状态。
func checkChannelKeys(c *gin.Context) {
	id := c.Param("id")
	idNum, err := strconv.Atoi(id)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	channel, err := ch.Get(idNum, c.Request.Context())
	if err != nil {
		log.Errorf("checkChannelKeys: channel get failed (id=%d): %v", idNum, err)
		resp.Error(c, http.StatusNotFound, resp.ErrResourceNotFound)
		return
	}
	summary, err := helper.TestChannel(c.Request.Context(), *channel)
	if err != nil {
		log.Errorf("checkChannelKeys: test failed (id=%d): %v", idNum, err)
		resp.Error(c, http.StatusBadRequest, "Channel test failed")
		return
	}
	resp.Success(c, summary)
}

type channelRequestPayload struct {
	ID             int                         `json:"id"`
	Name           string                      `json:"name"`
	GroupID        int                         `json:"group_id"`
	Type           outbound.OutboundType       `json:"type"`
	Enabled        bool                        `json:"enabled"`
	EnabledSet     bool                        `json:"-"` // 由 UnmarshalJSON 填充：区分"字段缺失"与"显式 false"
	BaseUrls       []model.BaseUrl             `json:"base_urls"`
	Keys           []channelKeyRequestPayload  `json:"keys"`
	Model          string                      `json:"model"`
	CustomModel    string                      `json:"custom_model"`
	Proxy          bool                        `json:"proxy"`
	ProxyMode      model.ProxyUsageMode        `json:"proxy_mode"`
	ProxyConfigID  *int                        `json:"proxy_config_id"`
	AutoSync       bool                        `json:"auto_sync"`
	AutoGroup      model.AutoGroupType         `json:"auto_group"`
	CustomHeader   []model.CustomHeader        `json:"custom_header"`
	ParamOverride  *string                     `json:"param_override"`
	ChannelProxy   *string                     `json:"channel_proxy"`
	RequestRewrite *model.RequestRewriteConfig `json:"request_rewrite"`
	MatchRegex     *string                     `json:"match_regex"`
	Stats          *model.StatsChannel         `json:"stats"`
}

type channelKeyRequestPayload struct {
	ID               int     `json:"id"`
	ChannelID        int     `json:"channel_id"`
	Enabled          bool    `json:"enabled"`
	EnabledSet       bool    `json:"-"`
	ChannelKey       string  `json:"channel_key"`
	StatusCode       int     `json:"status_code"`
	LastUseTimeStamp int64   `json:"last_use_time_stamp"`
	TotalCost        float64 `json:"total_cost"`
	Remark           string  `json:"remark"`
}

// UnmarshalJSON 记录 "enabled" 键是否出现：裸 bool 下"字段缺失"与"显式 false"
// 到达 Go 时都是 false，创建级联的停用补偿需要区分这两种请求。
func (p *channelKeyRequestPayload) UnmarshalJSON(data []byte) error {
	type alias channelKeyRequestPayload
	aux := struct {
		*alias
	}{alias: (*alias)(p)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	_, p.EnabledSet = raw["enabled"]
	return nil
}

// UnmarshalJSON 记录 "enabled" 键是否出现：裸 bool 下"字段缺失"与"显式 false"
// 到达 Go 时都是 false，创建停用渠道的补偿需要区分这两种请求。
func (p *channelRequestPayload) UnmarshalJSON(data []byte) error {
	type alias channelRequestPayload
	aux := struct {
		*alias
	}{alias: (*alias)(p)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	_, p.EnabledSet = raw["enabled"]
	return nil
}

func (p channelRequestPayload) toChannel() model.Channel {
	keys := make([]model.ChannelKey, 0, len(p.Keys))
	for _, key := range p.Keys {
		if strings.TrimSpace(key.ChannelKey) == "" {
			continue
		}
		keys = append(keys, model.ChannelKey{
			Enabled:    key.Enabled,
			ChannelKey: key.ChannelKey,
			Remark:     key.Remark,
			EnabledSet: key.EnabledSet,
		})
	}
	// proxy_mode 空值兜底为 direct：老客户端不发这个字段，而 resolveChannelProxy
	// 把空串也当 direct，但落库时留空会让 DB 里出现非法枚举值。
	proxyMode := p.ProxyMode
	if proxyMode == "" {
		proxyMode = model.ProxyUsageModeDirect
	}
	// 非 pool 模式不保留 config id：留一个不生效的 ID 只会误导排查（与 op.Update 同策略）。
	proxyConfigID := p.ProxyConfigID
	if proxyMode != model.ProxyUsageModePool {
		proxyConfigID = nil
	}
	return model.Channel{
		Name:           p.Name,
		GroupID:        p.GroupID,
		Type:           p.Type,
		Enabled:        p.Enabled,
		BaseUrls:       p.BaseUrls,
		Keys:           keys,
		Model:          p.Model,
		CustomModel:    p.CustomModel,
		Proxy:          p.Proxy,
		ProxyMode:      proxyMode,
		ProxyConfigID:  proxyConfigID,
		AutoSync:       p.AutoSync,
		AutoGroup:      p.AutoGroup,
		CustomHeader:   p.CustomHeader,
		ParamOverride:  p.ParamOverride,
		ChannelProxy:   p.ChannelProxy,
		RequestRewrite: p.RequestRewrite,
		MatchRegex:     p.MatchRegex,
	}
}

func listChannelGroup(c *gin.Context) {
	groups, err := op.ChannelGroupList(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, groups)
}

func createChannelGroup(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}

	group, err := op.ChannelGroupCreate(req.Name, c.Request.Context())
	if err != nil {
		if status, msg, ok := classifyChannelMutationError(err); ok {
			resp.Error(c, status, msg)
			return
		}
		resp.InternalError(c)
		return
	}
	resp.Success(c, group)
}

func updateChannelGroup(c *gin.Context) {
	var req struct {
		ID   int    `json:"id" binding:"required"`
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}

	group, err := op.ChannelGroupUpdate(req.ID, req.Name, c.Request.Context())
	if err != nil {
		if status, msg, ok := classifyChannelMutationError(err); ok {
			resp.Error(c, status, msg)
			return
		}
		resp.InternalError(c)
		return
	}
	resp.Success(c, group)
}

func deleteChannelGroup(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}

	if err := op.ChannelGroupDelete(id, c.Request.Context()); err != nil {
		if status, msg, ok := classifyChannelMutationError(err); ok {
			resp.Error(c, status, msg)
			return
		}
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func getLastSyncTime(c *gin.Context) {
	time := task.GetLastSyncModelsTime()
	resp.Success(c, time)
}

func classifyChannelMutationError(err error) (int, string, bool) {
	if err == nil {
		return 0, "", false
	}

	msg := strings.ToLower(err.Error())

	switch {
	case strings.Contains(msg, "channel not found"):
		return http.StatusNotFound, "channel not found", true
	case strings.Contains(msg, "channel group not found"):
		return http.StatusNotFound, "channel group not found", true
	case strings.Contains(msg, "channel group name is required"):
		return http.StatusBadRequest, "channel group name is required", true
	case strings.Contains(msg, "default channel group not found"):
		return http.StatusServiceUnavailable, "default channel group not found", true
	case strings.Contains(msg, "default channel group cannot be deleted"):
		return http.StatusBadRequest, "default channel group cannot be deleted", true
	case strings.Contains(msg, "channel group is not empty"):
		return http.StatusConflict, "channel group is not empty", true
	case strings.Contains(msg, "unsupported proxy mode"),
		strings.Contains(msg, "proxy config id is required when proxy mode is pool"):
		return http.StatusBadRequest, err.Error(), true
	case strings.Contains(msg, "request rewrite profile is required when enabled"),
		strings.Contains(msg, "unsupported request rewrite profile"),
		strings.Contains(msg, "unsupported tool role strategy"),
		strings.Contains(msg, "unsupported system message strategy"),
		strings.Contains(msg, "request rewrite profile") && strings.Contains(msg, "is not supported for channel type"):
		return http.StatusBadRequest, err.Error(), true
	case strings.Contains(msg, "request_rewrite") &&
		(strings.Contains(msg, "no such column") ||
			strings.Contains(msg, "has no column named") ||
			strings.Contains(msg, "unknown column")):
		return http.StatusServiceUnavailable, "database schema is outdated", true
	case strings.Contains(msg, "unique constraint failed: channels.name"),
		strings.Contains(msg, "duplicate entry") && strings.Contains(msg, "channels.name"):
		return http.StatusConflict, "channel name already exists", true
	case strings.Contains(msg, "unique constraint failed: channel_groups.name"),
		strings.Contains(msg, "duplicate entry") && strings.Contains(msg, "channel_groups.name"):
		return http.StatusConflict, "channel group name already exists", true
	default:
		return 0, "", false
	}
}

func maskChannelKeys(keys []model.ChannelKey) []model.ChannelKey {
	if len(keys) == 0 {
		return nil
	}

	masked := make([]model.ChannelKey, len(keys))
	for i, key := range keys {
		key.ChannelKey = maskChannelKeyValue(key.ChannelKey)
		masked[i] = key
	}
	return masked
}

func maskChannelKeyValue(raw string) string {
	return secretmask.Stars(raw)
}

func normalizeChannelListSlices(channel *model.Channel) {
	if channel == nil {
		return
	}
	if channel.BaseUrls == nil {
		channel.BaseUrls = make([]model.BaseUrl, 0)
	}
	if channel.Keys == nil {
		channel.Keys = make([]model.ChannelKey, 0)
	}
	if channel.CustomHeader == nil {
		channel.CustomHeader = make([]model.CustomHeader, 0)
	}
}
