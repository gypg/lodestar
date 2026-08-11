package sitesync

import (
	"testing"

	"github.com/gypg/lodestar/internal/model"
)

func TestBuildChannelKeys_MaskedTokenExclusion(t *testing.T) {
	tests := []struct {
		name     string
		tokens   []model.SiteToken
		wantLen  int
		wantKeys []string
	}{
		{
			name: "all valid tokens",
			tokens: []model.SiteToken{
				{Token: "sk-abc123", Enabled: true, ValueStatus: model.SiteTokenValueStatusReady},
				{Token: "sk-def456", Enabled: true, ValueStatus: model.SiteTokenValueStatusReady},
			},
			wantLen:  2,
			wantKeys: []string{"sk-abc123", "sk-def456"},
		},
		{
			name: "masked token excluded",
			tokens: []model.SiteToken{
				{Token: "sk-abc123", Enabled: true, ValueStatus: model.SiteTokenValueStatusReady},
				{Token: "sk-***masked***", Enabled: true, ValueStatus: model.SiteTokenValueStatusReady},
			},
			wantLen:  1,
			wantKeys: []string{"sk-abc123"},
		},
		{
			name: "all masked returns empty",
			tokens: []model.SiteToken{
				{Token: "sk-***masked***", Enabled: true, ValueStatus: model.SiteTokenValueStatusReady},
				{Token: "***masked***", Enabled: true, ValueStatus: model.SiteTokenValueStatusReady},
			},
			wantLen:  0,
			wantKeys: []string{},
		},
		{
			name: "disabled tokens excluded",
			tokens: []model.SiteToken{
				{Token: "sk-abc123", Enabled: false, ValueStatus: model.SiteTokenValueStatusReady},
				{Token: "sk-def456", Enabled: true, ValueStatus: model.SiteTokenValueStatusReady},
			},
			wantLen:  1,
			wantKeys: []string{"sk-def456"},
		},
		{
			name: "masked_pending tokens excluded",
			tokens: []model.SiteToken{
				{Token: "sk-abc123", Enabled: true, ValueStatus: model.SiteTokenValueStatusMaskedPending},
				{Token: "sk-def456", Enabled: true, ValueStatus: model.SiteTokenValueStatusReady},
			},
			wantLen:  1,
			wantKeys: []string{"sk-def456"},
		},
		{
			name: "mixed masked and disabled",
			tokens: []model.SiteToken{
				{Token: "sk-abc123", Enabled: true, ValueStatus: model.SiteTokenValueStatusReady},
				{Token: "sk-***masked***", Enabled: true, ValueStatus: model.SiteTokenValueStatusReady},
				{Token: "sk-def456", Enabled: false, ValueStatus: model.SiteTokenValueStatusReady},
				{Token: "sk-ghi789", Enabled: true, ValueStatus: model.SiteTokenValueStatusMaskedPending},
			},
			wantLen:  1,
			wantKeys: []string{"sk-abc123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildChannelKeys(tt.tokens)
			if len(got) != tt.wantLen {
				t.Errorf("buildChannelKeys() returned %d keys, want %d", len(got), tt.wantLen)
			}
			for i, key := range got {
				if i >= len(tt.wantKeys) {
					t.Errorf("buildChannelKeys() returned extra key at index %d: %s", i, key.ChannelKey)
					continue
				}
				if key.ChannelKey != tt.wantKeys[i] {
					t.Errorf("buildChannelKeys()[%d].ChannelKey = %s, want %s", i, key.ChannelKey, tt.wantKeys[i])
				}
			}
		})
	}
}

func TestProjectAccount_EnabledWithMaskedTokens(t *testing.T) {
	tests := []struct {
		name            string
		groupTokens     []model.SiteToken
		siteEnabled     bool
		accountEnabled  bool
		wantChannelEnabled bool
	}{
		{
			name: "valid tokens enable channel",
			groupTokens: []model.SiteToken{
				{Token: "sk-abc123", Enabled: true, ValueStatus: model.SiteTokenValueStatusReady},
			},
			siteEnabled:        true,
			accountEnabled:     true,
			wantChannelEnabled: true,
		},
		{
			name: "only masked tokens disable channel",
			groupTokens: []model.SiteToken{
				{Token: "sk-***masked***", Enabled: true, ValueStatus: model.SiteTokenValueStatusReady},
			},
			siteEnabled:        true,
			accountEnabled:     true,
			wantChannelEnabled: false,
		},
		{
			name: "mixed tokens enable channel",
			groupTokens: []model.SiteToken{
				{Token: "sk-***masked***", Enabled: true, ValueStatus: model.SiteTokenValueStatusReady},
				{Token: "sk-abc123", Enabled: true, ValueStatus: model.SiteTokenValueStatusReady},
			},
			siteEnabled:        true,
			accountEnabled:     true,
			wantChannelEnabled: true,
		},
		{
			name: "disabled site disables channel",
			groupTokens: []model.SiteToken{
				{Token: "sk-abc123", Enabled: true, ValueStatus: model.SiteTokenValueStatusReady},
			},
			siteEnabled:        false,
			accountEnabled:     true,
			wantChannelEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builtKeys := buildChannelKeys(tt.groupTokens)
			enabled := tt.siteEnabled && tt.accountEnabled && len(builtKeys) > 0
			if enabled != tt.wantChannelEnabled {
				t.Errorf("channel enabled = %v, want %v (built %d keys from %d tokens)",
					enabled, tt.wantChannelEnabled, len(builtKeys), len(tt.groupTokens))
			}
		})
	}
}
