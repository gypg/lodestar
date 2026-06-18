package op

import (
	"github.com/gypg/lodestar/internal/op/cacheusage"
)

type providerPromptCacheUsageSignals = cacheusage.ProviderPromptCacheUsageSignals

func parseProviderPromptCacheUsageSignals(responseContent string) (providerPromptCacheUsageSignals, bool) {
	return cacheusage.ParseProviderPromptCacheUsageSignals(responseContent)
}
