package relay

import (
	"testing"

	dbmodel "github.com/gypg/lodestar/internal/model"
)

func TestResolveRelayLogEndpointType(t *testing.T) {
	tests := []struct {
		name     string
		request  string
		matched  string
		expected string
	}{
		{
			name:     "preserves matched deepseek endpoint",
			request:  dbmodel.EndpointTypeChat,
			matched:  dbmodel.EndpointTypeDeepSeek,
			expected: dbmodel.EndpointTypeDeepSeek,
		},
		{
			name:     "falls back to requested endpoint when group is wildcard",
			request:  dbmodel.EndpointTypeAudioSpeech,
			matched:  dbmodel.EndpointTypeAll,
			expected: dbmodel.EndpointTypeAudioSpeech,
		},
		{
			name:     "keeps embeddings request when group endpoint missing",
			request:  dbmodel.EndpointTypeEmbeddings,
			matched:  "",
			expected: dbmodel.EndpointTypeEmbeddings,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveRelayLogEndpointType(tc.request, tc.matched); got != tc.expected {
				t.Fatalf("resolveRelayLogEndpointType(%q, %q) = %q, want %q", tc.request, tc.matched, got, tc.expected)
			}
		})
	}
}
