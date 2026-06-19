package handlers

/*
Lodestar — public, no-auth platform overview.

octopus is an admin-only tool with no public face. This additive endpoint gives
Lodestar a public platform layer: the landing page can show the site name,
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
		).
		AddRoute(
			router.NewRoute("/ping", http.MethodGet).
				Handle(getPublicPing),
		)
}

// getPublicPing is a minimal no-auth probe for browser-side latency checks
// (user portal / API guide). No DB; safe to call frequently.
func getPublicPing(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": true, "service": "lodestar"})
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
	ambient, _ := setting.GetString(model.SettingKeyLandingAmbientMode)
	if ambient != "color4bg" {
		ambient = "photo"
	}
	layout, _ := setting.GetString(model.SettingKeyLandingLayout)
	if layout != "newspaper" {
		layout = "winter"
	}
	bannerOn, _ := setting.GetBool(model.SettingKeySiteBannerEnabled)
	bannerText, _ := setting.GetString(model.SettingKeySiteBannerText)
	bannerTone, _ := setting.GetString(model.SettingKeySiteBannerTone)
	if bannerTone != "warning" && bannerTone != "success" {
		bannerTone = "info"
	}

	models := make([]publicModel, 0)
	if list, err := llm.List(c.Request.Context()); err == nil {
		for _, m := range list {
			models = append(models, publicModel{Name: m.Name, Input: m.Input, Output: m.Output})
		}
	}

	total := stats.TotalGet()

	resp.Success(c, gin.H{
		"site_name":            siteName,
		"description":          description,
		"announcement":         announcement,
		"footer":               footer,
		"landing_ambient_mode": ambient,
		"landing_layout":       layout,
		"site_banner_enabled":  bannerOn,
		"site_banner_text":     bannerText,
		"site_banner_tone":     bannerTone,
		"model_count":          len(models),
		"models":               models,
		"total_requests":       total.RequestSuccess + total.RequestFailed,
		"total_tokens":         total.InputToken + total.OutputToken,
	})
}