package handlers

import (
	"net/url"
	"strings"

	"github.com/gypg/lodestar/internal/model"
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

func redactChannelBaseURLsForViewer(channels []model.Channel) {
	for channelIndex := range channels {
		for baseURLIndex := range channels[channelIndex].BaseUrls {
			channels[channelIndex].BaseUrls[baseURLIndex].URL = maskURLDomainForViewer(channels[channelIndex].BaseUrls[baseURLIndex].URL)
		}
	}
}

func redactRemoteSiteBaseURLsForViewer(sites []model.RemoteSite) {
	for siteIndex := range sites {
		sites[siteIndex].BaseURL = maskURLDomainForViewer(sites[siteIndex].BaseURL)
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
	"epay_key":                            {},
	"smtp_pass":                           {},
	"semantic_cache_embedding_api_key":    {},
	"ai_route_api_key":                    {},
	"webdav_config":                       {},
	"image_bed_token":                     {},
}

// maskSensitiveSettings replaces the values of known-secret setting keys with
// a masked form. Called for ALL roles (including admin) on the list endpoint
// to limit blast radius of an account compromise.
func maskSensitiveSettings(settings []model.Setting) {
	for i := range settings {
		if _, ok := sensitiveSettingKeys[string(settings[i].Key)]; ok {
			settings[i].Value = maskSecretValue(settings[i].Value)
		}
	}
}

// maskRemoteSiteCredentials zeroes out credential fields in a copy of the site
// slice so the list API never returns raw tokens/passwords.
func maskRemoteSiteCredentials(sites []model.RemoteSite) {
	for i := range sites {
		sites[i].AccessToken = maskSecretValue(sites[i].AccessToken)
		sites[i].Password = maskSecretValue(sites[i].Password)
	}
}

// maskSiteAccountCredentials zeroes out credential fields on site accounts.
func maskSiteAccountCredentials(accounts []model.SiteAccount) {
	for i := range accounts {
		accounts[i].Password = maskSecretValue(accounts[i].Password)
		accounts[i].AccessToken = maskSecretValue(accounts[i].AccessToken)
		accounts[i].APIKey = maskSecretValue(accounts[i].APIKey)
		accounts[i].RefreshToken = maskSecretValue(accounts[i].RefreshToken)
	}
}
