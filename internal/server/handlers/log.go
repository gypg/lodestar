package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/model"
	ak "github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/op/relaylog"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
)

func init() {
	router.NewGroupRouter("/api/v1/log").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermLogsRead)).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listLog),
		).
		AddRoute(
			router.NewRoute("/detail", http.MethodGet).
				Handle(logDetail),
		).
		AddRoute(
			router.NewRoute("/clear", http.MethodDelete).
				Use(middleware.RequirePermission(auth.PermLogsWrite)).
				Handle(clearLog),
		).
		AddRoute(
			router.NewRoute("/stream", http.MethodGet).
				Handle(streamLog),
		)
}

// isLogStaff reports whether the caller is admin or editor (staff).
// Staff sees all logs; regular users are scoped to their own API keys.
func isLogStaff(c *gin.Context) bool {
	role := c.GetString("user_role")
	return role == model.UserRoleAdmin || role == model.UserRoleEditor
}

// getUserAPIKeyIDs returns the API key IDs owned by the authenticated user.
// Used to scope log and analytics queries for non-staff users.
func getUserAPIKeyIDs(c *gin.Context) []int {
	uid := uint(c.GetInt("user_id"))
	keys, err := ak.ListByUser(uid, c.Request.Context())
	if err != nil || len(keys) == 0 {
		return nil
	}
	ids := make([]int, len(keys))
	for i, k := range keys {
		ids[i] = k.ID
	}
	return ids
}

func listLog(c *gin.Context) {
	page, pageSize := parsePagination(c.DefaultQuery("page", "1"), c.DefaultQuery("page_size", "20"))

	filter := relaylog.LogFilter{}

	// Multi-tenant isolation: non-staff users can only see their own API keys' logs.
	if !isLogStaff(c) {
		filter.APIKeyIDs = getUserAPIKeyIDs(c)
		if len(filter.APIKeyIDs) == 0 {
			resp.Success(c, []model.RelayLogListItem{})
			return
		}
	}

	// include_attempts 默认：显式传 "false" 才关闭；否则当筛选了 channel_id 时默认开启，
	// 让"在渠道A 失败→重试到B 成功"的请求也能被渠道A 命中（issue #67）。
	includeAttemptsExplicit := false
	if v := c.Query("include_attempts"); v != "" {
		switch strings.ToLower(v) {
		case "true", "1":
			filter.IncludeAttempts = true
			includeAttemptsExplicit = true
		case "false", "0":
			filter.IncludeAttempts = false
			includeAttemptsExplicit = true
		default:
			resp.Error(c, http.StatusBadRequest, "invalid include_attempts (must be 'true' or 'false')")
			return
		}
	}

	if v := c.Query("start_time"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			resp.Error(c, http.StatusBadRequest, "invalid start_time")
			return
		}
		filter.StartTime = &n
	}
	if v := c.Query("end_time"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			resp.Error(c, http.StatusBadRequest, "invalid end_time")
			return
		}
		filter.EndTime = &n
	}
	if v := c.Query("model"); v != "" {
		filter.Model = v
	}
	if v := c.Query("channel_id"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			resp.Error(c, http.StatusBadRequest, "invalid channel_id")
			return
		}
		filter.ChannelID = &n
		// 未显式指定 include_attempts 时，按渠道筛选默认穿透尝试维度（issue #67）。
		if !includeAttemptsExplicit {
			filter.IncludeAttempts = true
		}
	}
	if v := c.Query("api_key_id"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			resp.Error(c, http.StatusBadRequest, "invalid api_key_id")
			return
		}
		filter.APIKeyID = &n
	}
	if v := c.Query("endpoint_type"); v != "" {
		filter.EndpointType = v
	}
	if v := c.Query("status"); v != "" {
		switch v {
		case "success":
			b := false
			filter.HasError = &b
		case "error":
			b := true
			filter.HasError = &b
		default:
			resp.Error(c, http.StatusBadRequest, "invalid status (must be 'success' or 'error')")
			return
		}
	}
	if v := c.Query("is_test"); v != "" {
		switch strings.ToLower(v) {
		case "true", "1":
			b := true
			filter.IsTest = &b
		case "false", "0":
			b := false
			filter.IsTest = &b
		default:
			resp.Error(c, http.StatusBadRequest, "invalid is_test (must be 'true' or 'false')")
			return
		}
	}

	logs, err := relaylog.RelayLogList(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		resp.InternalError(c)
		return
	}

	resp.Success(c, logs)
}

func logDetail(c *gin.Context) {
	idStr := c.Query("id")
	if idStr == "" {
		resp.Error(c, http.StatusBadRequest, "id is required")
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	log, err := relaylog.RelayLogGetByID(c.Request.Context(), id)
	if err != nil {
		resp.InternalError(c)
		return
	}
	if log == nil {
		resp.Error(c, http.StatusNotFound, "log not found")
		return
	}

	// Multi-tenant isolation: non-staff users can only view their own logs.
	if !isLogStaff(c) {
		userKeyIDs := getUserAPIKeyIDs(c)
		allowed := false
		for _, kid := range userKeyIDs {
			if log.RequestAPIKeyID == kid {
				allowed = true
				break
			}
		}
		if !allowed {
			resp.Error(c, http.StatusForbidden, "access denied")
			return
		}
	}

	resp.Success(c, log)
}

func clearLog(c *gin.Context) {
	if err := relaylog.RelayLogClear(c.Request.Context()); err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func streamLog(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Multi-tenant isolation: pre-compute allowed API key IDs for non-staff.
	var allowedKeyIDs map[int]struct{}
	if !isLogStaff(c) {
		ids := getUserAPIKeyIDs(c)
		if len(ids) == 0 {
			// User has no API keys; send connected marker then close.
			if _, err := c.Writer.Write([]byte(": connected\n\n")); err != nil {
				return
			}
			c.Writer.Flush()
			<-c.Request.Context().Done()
			return
		}
		allowedKeyIDs = make(map[int]struct{}, len(ids))
		for _, id := range ids {
			allowedKeyIDs[id] = struct{}{}
		}
	}

	logChan := relaylog.RelayLogSubscribe()
	defer relaylog.RelayLogUnsubscribe(logChan)

	heartbeatTicker := time.NewTicker(conf.SSEHeartbeatInterval)
	defer heartbeatTicker.Stop()

	if _, err := c.Writer.Write([]byte(": connected\n\n")); err != nil {
		return
	}
	c.Writer.Flush()

	ctx := c.Request.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeatTicker.C:
			if _, err := c.Writer.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			c.Writer.Flush()
		case log, ok := <-logChan:
			if !ok {
				return
			}
			if relaylog.RelayLogStreamExcluded(log.RequestModelName) {
				continue
			}
			// Multi-tenant isolation: skip logs not belonging to the user's API keys.
			if allowedKeyIDs != nil {
				if _, ok := allowedKeyIDs[log.RequestAPIKeyID]; !ok {
					continue
				}
			}
			// 仅推送列表所需的轻量字段，剥离 request_content / response_content
			// 大字段（详情按需单独拉取），避免高 QPS 下用大 payload 拖慢前端。
			data, err := json.Marshal(log.ToListItem())
			if err != nil {
				continue
			}
			if _, err := c.Writer.Write([]byte(fmt.Sprintf("data: %s\n\n", data))); err != nil {
				return
			}
			c.Writer.Flush()
		}
	}
}
