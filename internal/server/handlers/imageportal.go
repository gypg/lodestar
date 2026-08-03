package handlers

// Lodestar portal image playground — persisted generation history per user.

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/op/imageportal"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
	"gorm.io/gorm"
)

func init() {
	router.NewGroupRouter("/api/v1/image").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(router.NewRoute("/records", http.MethodGet).Handle(listImageRecords)).
		AddRoute(router.NewRoute("/records", http.MethodPost).Handle(createImageRecord)).
		AddRoute(router.NewRoute("/records/:id", http.MethodGet).Handle(getImageRecord)).
		AddRoute(router.NewRoute("/records/:id", http.MethodDelete).Handle(deleteImageRecord))
}

func listImageRecords(c *gin.Context) {
	uid := uint(c.GetInt("user_id"))
	items, err := imageportal.ListForUser(uid, c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	if items == nil {
		items = []imageportal.RecordSummary{}
	}
	resp.Success(c, items)
}

func createImageRecord(c *gin.Context) {
	uid := uint(c.GetInt("user_id"))
	var req struct {
		Model    string `json:"model"`
		Prompt   string `json:"prompt"`
		Size     string `json:"size"`
		APIKeyID int    `json:"api_key_id"`
		URL      string `json:"url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	detail, err := imageportal.Create(uid, imageportal.CreateInput{
		Model: req.Model, Prompt: req.Prompt, Size: req.Size,
		APIKeyID: req.APIKeyID, URL: req.URL,
	}, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, detail)
}

func getImageRecord(c *gin.Context) {
	uid := uint(c.GetInt("user_id"))
	id, err := parseImageRecordID(c)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid record id")
		return
	}
	detail, err := imageportal.GetForUser(uid, id, c.Request.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			resp.Error(c, http.StatusNotFound, "image record not found")
			return
		}
		resp.InternalError(c)
		return
	}
	resp.Success(c, detail)
}

func deleteImageRecord(c *gin.Context) {
	uid := uint(c.GetInt("user_id"))
	id, err := parseImageRecordID(c)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid record id")
		return
	}
	if err := imageportal.Delete(uid, id, c.Request.Context()); err != nil {
		if err.Error() == "image record not found" {
			resp.Error(c, http.StatusNotFound, err.Error())
			return
		}
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func parseImageRecordID(c *gin.Context) (int, error) {
	return strconv.Atoi(c.Param("id"))
}
