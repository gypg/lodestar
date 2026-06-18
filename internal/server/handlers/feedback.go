package handlers

// GGZERO — 意见反馈端点：用户提交（任意登录用户），管理员查看列表。

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/op/feedback"
	"github.com/lingyuins/octopus/internal/server/auth"
	"github.com/lingyuins/octopus/internal/server/middleware"
	"github.com/lingyuins/octopus/internal/server/resp"
	"github.com/lingyuins/octopus/internal/server/router"
)

func init() {
	router.NewGroupRouter("/api/v1/feedback").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/submit", http.MethodPost).
				Handle(submitFeedback),
		)

	router.NewGroupRouter("/api/v1/feedback").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		Use(middleware.RequirePermission(auth.PermUsersRead)).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listFeedback),
		)
}

func submitFeedback(c *gin.Context) {
	var req struct {
		Content string `json:"content"`
		Contact string `json:"contact"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := feedback.Create(uint(c.GetInt("user_id")), req.Content, req.Contact, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func listFeedback(c *gin.Context) {
	items, err := feedback.List(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, items)
}
