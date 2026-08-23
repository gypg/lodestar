package handlers

import (
	"net/url"
	"strings"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/utils/secretmask"
)

const viewerMaskedDomain = "***"

func isViewerRole(role string) bool {
	return strings.TrimSpace(role) == model.UserRoleViewer
}

func maskURLDomainForViewer(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}

	parsed, err := url.Parse(trimmed)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		parsed.User = nil
		parsed.Host = viewerMaskedDomain
		return parsed.String()
	}

	return viewerMaskedDomain
}

// redactChannelBaseURLsForViewer masks the upstream host of every base URL.
//
// It must REPLACE each BaseUrls slice instead of rewriting its elements.
// chCache stores model.Channel by value (op/channel/channel.go:15), so the
// copies handed out by ch.Get / ch.List still carry slice headers pointing at
// the cache's backing array. An in-place rewrite therefore edited the live
// cache entry the relay routes on, and a single viewer-role /channel/list call
// permanently repointed every channel at "https://***" until the next restart
// or cache refresh — for all roles, not just viewers.
func redactChannelBaseURLsForViewer(channels []model.Channel) {
	for channelIndex := range channels {
		original := channels[channelIndex].BaseUrls
		if len(original) == 0 {
			continue
		}
		redacted := make([]model.BaseUrl, len(original))
		for baseURLIndex, baseURL := range original {
			baseURL.URL = maskURLDomainForViewer(baseURL.URL)
			redacted[baseURLIndex] = baseURL
		}
		channels[channelIndex].BaseUrls = redacted
	}
}

func redactCredentialBaseURLsForViewer(profiles []model.APICredentialProfile) {
	for profileIndex := range profiles {
		profiles[profileIndex].BaseURL = maskURLDomainForViewer(profiles[profileIndex].BaseURL)
	}
}

func redactSiteBaseURLsForViewer(sites []model.Site) {
	for siteIndex := range sites {
		sites[siteIndex].BaseURL = maskURLDomainForViewer(sites[siteIndex].BaseURL)
	}
}

func redactSettingsURLsForViewer(settings []model.Setting) {
	for settingIndex := range settings {
		switch settings[settingIndex].Key {
		case model.SettingKeyProxyURL,
			model.SettingKeyPublicAPIBaseURL,
			model.SettingKeySemanticCacheEmbeddingBaseURL,
			model.SettingKeyAIRouteBaseURL,
			model.SettingKeyImageBedEndpoint:
			settings[settingIndex].Value = maskURLDomainForViewer(settings[settingIndex].Value)
		}
	}
}

// sensitiveSettingKeys are setting keys whose values must never be returned in
// full via the list endpoint. Only the set/write endpoint exposes raw values.
var sensitiveSettingKeys = map[string]struct{}{
	"epay_key":                         {},
	"smtp_pass":                        {},
	"semantic_cache_embedding_api_key": {},
	"ai_route_api_key":                 {},
	"webdav_config":                    {},
	"image_bed_token":                  {},
	"stripe_api_key":                   {},
	"stripe_webhook_secret":            {},
	"turnstile_secret_key":             {},
	"github_oauth_client_secret":       {},
}

// maskSensitiveSettings replaces the values of known-secret setting keys with
// a masked form. Called for ALL roles (including admin) on the list endpoint
// to limit blast radius of an account compromise.
func maskSensitiveSettings(settings []model.Setting) {
	for i := range settings {
		if _, ok := sensitiveSettingKeys[string(settings[i].Key)]; ok {
			settings[i].Value = secretmask.Stars(settings[i].Value)
		}
	}
}

// maskSiteAccountCredentials zeroes out credential fields on site accounts.
func maskSiteAccountCredentials(accounts []model.SiteAccount) {
	for i := range accounts {
		accounts[i].Password = secretmask.Stars(accounts[i].Password)
		accounts[i].AccessToken = secretmask.Stars(accounts[i].AccessToken)
		accounts[i].APIKey = secretmask.Stars(accounts[i].APIKey)
		accounts[i].RefreshToken = secretmask.Stars(accounts[i].RefreshToken)
	}
}
