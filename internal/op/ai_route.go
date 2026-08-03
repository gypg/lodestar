package op

import (
	"context"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/airoute"
)

type AIRoutePartialFailureError = airoute.AIRoutePartialFailureError

// Deprecated: Use airoute.GenerateAIRoute from internal/op/airoute instead.
func GenerateAIRoute(
	ctx context.Context,
	req model.GenerateAIRouteRequest,
	report func(progress model.GenerateAIRouteProgress),
) (*model.GenerateAIRouteResult, error) {
	return airoute.GenerateAIRoute(ctx, req, report)
}
