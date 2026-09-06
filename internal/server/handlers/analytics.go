package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/analytics"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
)

func init() {
	router.NewGroupRouter("/api/v1/analytics").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermStatsRead)).
		AddRoute(
			router.NewRoute("/overview", http.MethodGet).
				Handle(getAnalyticsOverview),
		).
		AddRoute(
			router.NewRoute("/utilization", http.MethodGet).
				Handle(getAnalyticsUtilization),
		).
		AddRoute(
			router.NewRoute("/evaluation", http.MethodGet).
				Handle(getAnalyticsEvaluation),
		).
		AddRoute(
			router.NewRoute("/group-health", http.MethodGet).
				Handle(getAnalyticsGroupHealth),
		).
		AddRoute(
			router.NewRoute("/provider-breakdown", http.MethodGet).
				Handle(getAnalyticsProviderBreakdown),
		).
		AddRoute(
			router.NewRoute("/model-breakdown", http.MethodGet).
				Handle(getAnalyticsModelBreakdown),
		).
		AddRoute(
			router.NewRoute("/channel-model", http.MethodGet).
				Handle(getAnalyticsChannelModel),
		).
		AddRoute(
			router.NewRoute("/auto-strategy", http.MethodGet).
				Handle(getAnalyticsAutoStrategy),
		).
		AddRoute(
			router.NewRoute("/apikey-breakdown", http.MethodGet).
				Handle(getAnalyticsAPIKeyBreakdown),
		).
		AddRoute(
			router.NewRoute("/latency-distribution", http.MethodGet).
				Handle(getAnalyticsLatencyDistribution),
		).
		AddRoute(
			router.NewRoute("/latency-models", http.MethodGet).
				Handle(getAnalyticsLatencyModels),
		).
		AddRoute(
			router.NewRoute("/model-latency", http.MethodGet).
				Handle(getAnalyticsModelLatency),
		)
}

// canSeeSiteWideAnalytics reports whether the caller may see site-wide
// analytics. Decided by permission, not by role name: anyone holding
// channels:read is staff-side (admin / editor / viewer — a viewer is a
// read-only staff member), while user-role customers hold neither
// channels:read nor any other staff permission and must only ever see their
// own key-scoped numbers (WO-040 ②).
func canSeeSiteWideAnalytics(c *gin.Context) bool {
	return auth.HasPermission(c.GetString("user_role"), auth.PermChannelsRead)
}

func getAnalyticsOverview(c *gin.Context) {
	analyticsRange, ok := parseAnalyticsRange(c)
	if !ok {
		return
	}

	// Multi-tenant isolation: non-staff users only see their own API key scope.
	var userID *uint
	if !isStaff(c) {
		uid := uint(c.GetInt("user_id"))
		userID = &uid
	}

	data, err := analytics.AnalyticsOverviewGet(c.Request.Context(), analyticsRange, userID)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, data)
}

func getAnalyticsUtilization(c *gin.Context) {
	// WO-040 ②：渠道/站点维度的分析对客户返回空集——这些数字来自全站，
	// 对客户既无意义也泄露站点规模（判定用权限，user 无 channels:read）。
	if !canSeeSiteWideAnalytics(c) {
		resp.Success(c, nil)
		return
	}
	analyticsRange, ok := parseAnalyticsRange(c)
	if !ok {
		return
	}

	data, err := analytics.AnalyticsUtilizationGet(c.Request.Context(), analyticsRange)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, data)
}

func getAnalyticsEvaluation(c *gin.Context) {
	// WO-040 ②：渠道/站点维度的分析对客户返回空集——这些数字来自全站，
	// 对客户既无意义也泄露站点规模（判定用权限，user 无 channels:read）。
	if !canSeeSiteWideAnalytics(c) {
		resp.Success(c, nil)
		return
	}
	data, err := analytics.AnalyticsEvaluationGet(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, data)
}

func getAnalyticsGroupHealth(c *gin.Context) {
	// WO-040 ②：渠道/站点维度的分析对客户返回空集——这些数字来自全站，
	// 对客户既无意义也泄露站点规模（判定用权限，user 无 channels:read）。
	if !canSeeSiteWideAnalytics(c) {
		resp.Success(c, nil)
		return
	}
	data, err := analytics.AnalyticsGroupHealthGet(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, data)
}

func getAnalyticsProviderBreakdown(c *gin.Context) {
	// WO-040 ②：渠道/站点维度的分析对客户返回空集——这些数字来自全站，
	// 对客户既无意义也泄露站点规模（判定用权限，user 无 channels:read）。
	if !canSeeSiteWideAnalytics(c) {
		resp.Success(c, nil)
		return
	}
	analyticsRange, ok := parseAnalyticsRange(c)
	if !ok {
		return
	}

	data, err := analytics.AnalyticsProviderBreakdownGet(c.Request.Context(), analyticsRange)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, data)
}

func getAnalyticsModelBreakdown(c *gin.Context) {
	// WO-040 ②：渠道/站点维度的分析对客户返回空集——这些数字来自全站，
	// 对客户既无意义也泄露站点规模（判定用权限，user 无 channels:read）。
	if !canSeeSiteWideAnalytics(c) {
		resp.Success(c, nil)
		return
	}
	analyticsRange, ok := parseAnalyticsRange(c)
	if !ok {
		return
	}

	data, err := analytics.AnalyticsModelBreakdownGet(c.Request.Context(), analyticsRange)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, data)
}

func getAnalyticsChannelModel(c *gin.Context) {
	// WO-040 ②：渠道/站点维度的分析对客户返回空集——这些数字来自全站，
	// 对客户既无意义也泄露站点规模（判定用权限，user 无 channels:read）。
	if !canSeeSiteWideAnalytics(c) {
		resp.Success(c, nil)
		return
	}
	analyticsRange, ok := parseAnalyticsRange(c)
	if !ok {
		return
	}
	var groupID *int
	if v := c.Query("group_id"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			resp.Error(c, http.StatusBadRequest, "invalid group_id")
			return
		}
		groupID = &n
	}

	data, err := analytics.AnalyticsChannelModelBreakdownGet(c.Request.Context(), analyticsRange, groupID)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, data)
}

func getAnalyticsAutoStrategy(c *gin.Context) {
	// WO-040 ②：渠道/站点维度的分析对客户返回空集——这些数字来自全站，
	// 对客户既无意义也泄露站点规模（判定用权限，user 无 channels:read）。
	if !canSeeSiteWideAnalytics(c) {
		resp.Success(c, nil)
		return
	}
	var groupID *int
	if v := c.Query("group_id"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			resp.Error(c, http.StatusBadRequest, "invalid group_id")
			return
		}
		groupID = &n
	}

	data, err := analytics.AnalyticsAutoStrategyGet(c.Request.Context(), groupID)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, data)
}

func getAnalyticsAPIKeyBreakdown(c *gin.Context) {
	// WO-040 ②：渠道/站点维度的分析对客户返回空集——这些数字来自全站，
	// 对客户既无意义也泄露站点规模（判定用权限，user 无 channels:read）。
	if !canSeeSiteWideAnalytics(c) {
		resp.Success(c, nil)
		return
	}
	analyticsRange, ok := parseAnalyticsRange(c)
	if !ok {
		return
	}

	data, err := analytics.AnalyticsAPIKeyBreakdownGet(c.Request.Context(), analyticsRange)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, data)
}

func getAnalyticsLatencyDistribution(c *gin.Context) {
	// WO-040 ②：渠道/站点维度的分析对客户返回空集——这些数字来自全站，
	// 对客户既无意义也泄露站点规模（判定用权限，user 无 channels:read）。
	if !canSeeSiteWideAnalytics(c) {
		resp.Success(c, nil)
		return
	}
	analyticsRange, ok := parseAnalyticsRange(c)
	if !ok {
		return
	}
	modelFilter := c.Query("model")
	data, err := analytics.AnalyticsLatencyDistributionGet(c.Request.Context(), analyticsRange, modelFilter)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, data)
}

func getAnalyticsLatencyModels(c *gin.Context) {
	// WO-040 ②：渠道/站点维度的分析对客户返回空集——这些数字来自全站，
	// 对客户既无意义也泄露站点规模（判定用权限，user 无 channels:read）。
	if !canSeeSiteWideAnalytics(c) {
		resp.Success(c, nil)
		return
	}
	analyticsRange, ok := parseAnalyticsRange(c)
	if !ok {
		return
	}
	data, err := analytics.AnalyticsLatencyModelsGet(c.Request.Context(), analyticsRange)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, data)
}

func getAnalyticsModelLatency(c *gin.Context) {
	// WO-040 ②：渠道/站点维度的分析对客户返回空集——这些数字来自全站，
	// 对客户既无意义也泄露站点规模（判定用权限，user 无 channels:read）。
	if !canSeeSiteWideAnalytics(c) {
		resp.Success(c, nil)
		return
	}
	analyticsRange, ok := parseAnalyticsRange(c)
	if !ok {
		return
	}
	data, err := analytics.AnalyticsModelLatencyListGet(c.Request.Context(), analyticsRange)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, data)
}

func parseAnalyticsRange(c *gin.Context) (model.AnalyticsRange, bool) {
	analyticsRange, err := model.ParseAnalyticsRange(c.Query("range"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return "", false
	}
	return analyticsRange, true
}
