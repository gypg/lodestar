package op

import (
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/airoute"
)

// normalizeAIRouteServiceName is retained for backward compatibility (used by ops.go and airoute tests).
func normalizeAIRouteServiceName(cfg model.AIRouteServiceConfig, ordinal int) string {
	return airoute.NormalizeAIRouteServiceName(cfg, ordinal)
}
