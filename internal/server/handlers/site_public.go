package handlers

/*
GGZERO — public, no-auth platform overview.

octopus is an admin-only tool with no public face. This additive endpoint gives
GGZERO a public platform layer: the landing page can show the site name,
announcement, supported model catalog (names + pricing only), and aggregate
usage — all WITHOUT login — while private data stays behind auth.

It deliberately exposes only safe, non-sensitive fields: no API keys, no user
data, no channel addresses, no per-channel internals.
*/

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/llm"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/stats"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
)

func init() {
	router.NewGroupRouter("/api/v1/public").
		AddRoute(
			router.NewRoute("/overview", http.MethodGet).
				Handle(getPublicOverview),
		)
}

type publicModel struct {
	Name   string  `json:"name"`
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

func getPublicOverview(c *gin.Context) {
	siteName, _ := setting.GetString(model.SettingKeySiteName)
	description, _ := setting.GetString(model.SettingKeySiteDescription)
	announcement, _ := setting.GetString(model.SettingKeySiteAnnouncement)
	footer, _ := setting.GetString(model.SettingKeySiteFooter)

	models := make([]publicModel, 0)
	if list, err := llm.List(c.Request.Context()); err == nil {
		for _, m := range list {
			models = append(models, publicModel{Name: m.Name, Input: m.Input, Output: m.Output})
		}
	}

	total := stats.TotalGet()

	resp.Success(c, gin.H{
		"site_name":      siteName,
		"description":    description,
		"announcement":   announcement,
		"footer":         footer,
		"model_count":    len(models),
		"models":         models,
		"total_requests": total.RequestSuccess + total.RequestFailed,
		"total_tokens":   total.InputToken + total.OutputToken,
	})
}
