package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
	"github.com/gypg/lodestar/internal/utils/log"
)

func init() {
	// 这两组此前只有 Auth()，任何登录用户（含 user 角色的付费客户）都能到达。
	// 三个后果都已在生产实测确认：
	//   - /list 原样返回 ProxyConfiguration.URL，而代理 URL 常带 user:pass@host
	//     → 端客户读得到全部代理凭据；
	//   - /delete/:id 直达业务逻辑（不存在的 id 返 500 而非 403）→ 端客户可删配置；
	//   - /test 的 proxy_url 不经任何 SSRF 校验（NormalizeProxyURL 只查 scheme/host），
	//     响应区分 Not Found / connection refused / no route to host / 超时
	//     → 端客户可枚举服务器内网与端口状态。
	// user 角色的权限集只有 apikeys:read|write / stats:read / subscriptions:read
	// （auth/permissions.go），所以 settings:* 两道门即可挡住它。
	router.NewGroupRouter("/api/v1/proxy-pool").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermSettingsRead)).
		AddRoute(router.NewRoute("/list", http.MethodGet).Handle(listProxyConfigurations)).
		AddRoute(router.NewRoute("/references/:id", http.MethodGet).Handle(listProxyConfigurationReferences))

	router.NewGroupRouter("/api/v1/proxy-pool").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermSettingsWrite)).
		AddRoute(router.NewRoute("/delete/:id", http.MethodDelete).Handle(deleteProxyConfiguration))

	router.NewGroupRouter("/api/v1/proxy-pool").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		Use(middleware.RequirePermission(auth.PermSettingsWrite)).
		AddRoute(router.NewRoute("/create", http.MethodPost).Handle(createProxyConfiguration)).
		AddRoute(router.NewRoute("/update", http.MethodPost).Handle(updateProxyConfiguration)).
		AddRoute(router.NewRoute("/test", http.MethodPost).Handle(testProxyConfiguration))
}

func listProxyConfigurations(c *gin.Context) {
	items, err := op.ProxyConfigurationList(c.Request.Context())
	if err != nil {
		log.Errorf("listProxyConfigurations failed: %v", err)
		resp.InternalError(c)
		return
	}
	resp.Success(c, items)
}

func listProxyConfigurationReferences(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	items, err := op.ProxyConfigurationReferences(idNum, c.Request.Context())
	if err != nil {
		log.Errorf("listProxyConfigurationReferences failed (id=%d): %v", idNum, err)
		resp.InternalError(c)
		return
	}
	resp.Success(c, items)
}

func createProxyConfiguration(c *gin.Context) {
	type proxyConfigurationCreateRequest struct {
		Name    string `json:"name" binding:"required"`
		URL     string `json:"url" binding:"required"`
		Enabled *bool  `json:"enabled,omitempty"`
		Remark  string `json:"remark,omitempty"`
	}

	var req proxyConfigurationCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	item := model.ProxyConfiguration{
		Name:    req.Name,
		URL:     req.URL,
		Enabled: enabled,
		Remark:  req.Remark,
	}
	if err := op.ProxyConfigurationCreate(&item, c.Request.Context()); err != nil {
		log.Errorf("createProxyConfiguration failed: %v", err)
		resp.InternalError(c)
		return
	}
	resp.Success(c, item)
}

func updateProxyConfiguration(c *gin.Context) {
	var req model.ProxyConfigurationUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	item, err := op.ProxyConfigurationUpdate(&req, c.Request.Context())
	if err != nil {
		log.Errorf("updateProxyConfiguration failed: %v", err)
		resp.InternalError(c)
		return
	}
	resp.Success(c, item)
}

func deleteProxyConfiguration(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	if err := op.ProxyConfigurationDelete(idNum, c.Request.Context()); err != nil {
		log.Errorf("deleteProxyConfiguration failed (id=%d): %v", idNum, err)
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func testProxyConfiguration(c *gin.Context) {
	var req model.ProxyTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	result, err := op.ProxyConfigurationTest(req, c.Request.Context())
	if err != nil {
		log.Errorf("testProxyConfiguration failed: %v", err)
		resp.InternalError(c)
		return
	}
	resp.Success(c, result)
}
