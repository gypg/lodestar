package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/relay/guardrail"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
	stg "github.com/gypg/lodestar/internal/op/setting"
)

func init() {
	router.NewGroupRouter("/api/v1/guardrail").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermSettingsRead)).
		AddRoute(router.NewRoute("/status", http.MethodGet).Handle(getGuardrailStatus)).
		AddRoute(router.NewRoute("/config", http.MethodPost).
			Use(middleware.RequirePermission(auth.PermSettingsWrite)).
			Use(middleware.RequireJSON()).
			Handle(updateGuardrailConfig))
}

// getGuardrailStatus returns the current guardrail configuration.
func getGuardrailStatus(c *gin.Context) {
	cfg := guardrail.LoadConfig()
	resp.Success(c, cfg)
}

// guardrailConfigPayload is the request body for updating guardrail settings.
type guardrailConfigPayload struct {
	Enabled        *bool   `json:"enabled"`
	BannedWords    []string `json:"banned_words"`
	MaxInputLength *int    `json:"max_input_length"`
	MaxOutputLength *int   `json:"max_output_length"`
	PIIDetection   *bool   `json:"pii_detection"`
	BlockMessage   *string `json:"block_message"`
}

// updateGuardrailConfig merges the supplied fields into the stored guardrail
// rules JSON and persists the changes.
func updateGuardrailConfig(c *gin.Context) {
	var req guardrailConfigPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}

	// Load existing config to merge with.
	existing := guardrail.LoadConfig()
	merged := guardrail.GuardrailConfig{
		Enabled:        existing.Enabled,
		BannedWords:    existing.BannedWords,
		MaxInputLength: existing.MaxInputLength,
		MaxOutputLength: existing.MaxOutputLength,
		PIIDetection:   existing.PIIDetection,
		BlockMessage:   existing.BlockMessage,
	}

	if req.Enabled != nil {
		enabledStr := "false"
		if *req.Enabled {
			enabledStr = "true"
		}
		if err := stg.SetString(model.SettingKeyGuardrailEnabled, enabledStr); err != nil {
			resp.InternalError(c)
			return
		}
		merged.Enabled = *req.Enabled
	}

	if req.BannedWords != nil {
		merged.BannedWords = req.BannedWords
	}
	if req.MaxInputLength != nil {
		merged.MaxInputLength = *req.MaxInputLength
	}
	if req.MaxOutputLength != nil {
		merged.MaxOutputLength = *req.MaxOutputLength
	}
	if req.PIIDetection != nil {
		merged.PIIDetection = *req.PIIDetection
	}
	if req.BlockMessage != nil {
		merged.BlockMessage = *req.BlockMessage
	}

	rulesJSON, err := json.Marshal(merged)
	if err != nil {
		resp.InternalError(c)
		return
	}
	if err := stg.SetString(model.SettingKeyGuardrailRules, string(rulesJSON)); err != nil {
		resp.InternalError(c)
		return
	}

	resp.Success(c, merged)
}
