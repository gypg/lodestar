package handlers

// Lodestar portal chat — persisted sessions per user.

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/op/chatportal"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
	"gorm.io/gorm"
)

func init() {
	router.NewGroupRouter("/api/v1/chat").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(router.NewRoute("/sessions", http.MethodGet).Handle(listChatSessions)).
		AddRoute(router.NewRoute("/sessions", http.MethodPost).Handle(createChatSession)).
		AddRoute(router.NewRoute("/sessions/:id", http.MethodGet).Handle(getChatSession)).
		AddRoute(router.NewRoute("/sessions/:id", http.MethodPut).Handle(saveChatSession)).
		AddRoute(router.NewRoute("/sessions/:id", http.MethodDelete).Handle(deleteChatSession))
}

func listChatSessions(c *gin.Context) {
	uid := uint(c.GetInt("user_id"))
	items, err := chatportal.ListForUser(uid, c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	if items == nil {
		items = []chatportal.SessionSummary{}
	}
	resp.Success(c, items)
}

func createChatSession(c *gin.Context) {
	uid := uint(c.GetInt("user_id"))
	var req struct {
		Title    string `json:"title"`
		Model    string `json:"model"`
		APIKeyID int    `json:"api_key_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	detail, err := chatportal.Create(uid, req.Title, req.Model, req.APIKeyID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, detail)
}

func getChatSession(c *gin.Context) {
	uid := uint(c.GetInt("user_id"))
	id, err := parseChatSessionID(c)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid session id")
		return
	}
	detail, err := chatportal.GetForUser(uid, id, c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			resp.Error(c, http.StatusNotFound, "session not found")
			return
		}
		resp.InternalError(c)
		return
	}
	resp.Success(c, detail)
}

func saveChatSession(c *gin.Context) {
	uid := uint(c.GetInt("user_id"))
	id, err := parseChatSessionID(c)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid session id")
		return
	}
	var req struct {
		Model    string             `json:"model"`
		APIKeyID int                `json:"api_key_id"`
		Messages []chatportal.Message `json:"messages"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := chatportal.SaveMessages(uid, id, req.Model, req.APIKeyID, req.Messages, c.Request.Context()); err != nil {
		if err.Error() == "session not found" {
			resp.Error(c, http.StatusNotFound, err.Error())
			return
		}
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func deleteChatSession(c *gin.Context) {
	uid := uint(c.GetInt("user_id"))
	id, err := parseChatSessionID(c)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid session id")
		return
	}
	if err := chatportal.Delete(uid, id, c.Request.Context()); err != nil {
		if err.Error() == "session not found" {
			resp.Error(c, http.StatusNotFound, err.Error())
			return
		}
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func parseChatSessionID(c *gin.Context) (int, error) {
	return strconv.Atoi(c.Param("id"))
}