package op

import (
	"context"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/analytics"
)

// Deprecated: Use analytics.AnalyticsOverviewGet from internal/op/analytics instead.
func AnalyticsOverviewGet(ctx context.Context, r model.AnalyticsRange, userID *uint) (*model.AnalyticsOverview, error) {
	return analytics.AnalyticsOverviewGet(ctx, r, userID)
}

// Deprecated: Use analytics.AnalyticsUtilizationGet from internal/op/analytics instead.
func AnalyticsUtilizationGet(ctx context.Context, r model.AnalyticsRange) (*model.AnalyticsUtilization, error) {
	return analytics.AnalyticsUtilizationGet(ctx, r)
}

// Deprecated: Use analytics.AnalyticsProviderBreakdownGet from internal/op/analytics instead.
func AnalyticsProviderBreakdownGet(ctx context.Context, r model.AnalyticsRange) ([]model.AnalyticsProviderBreakdownItem, error) {
	return analytics.AnalyticsProviderBreakdownGet(ctx, r)
}

// Deprecated: Use analytics.AnalyticsModelBreakdownGet from internal/op/analytics instead.
func AnalyticsModelBreakdownGet(ctx context.Context, r model.AnalyticsRange) ([]model.AnalyticsModelBreakdownItem, error) {
	return analytics.AnalyticsModelBreakdownGet(ctx, r)
}

// Deprecated: Use analytics.AnalyticsAPIKeyBreakdownGet from internal/op/analytics instead.
func AnalyticsAPIKeyBreakdownGet(ctx context.Context, r model.AnalyticsRange) ([]model.AnalyticsAPIKeyBreakdownItem, error) {
	return analytics.AnalyticsAPIKeyBreakdownGet(ctx, r)
}

// Deprecated: Use analytics.AnalyticsGroupHealthGet from internal/op/analytics instead.
func AnalyticsGroupHealthGet(ctx context.Context) ([]model.AnalyticsGroupHealthItem, error) {
	return analytics.AnalyticsGroupHealthGet(ctx)
}

// Deprecated: Use analytics.AnalyticsEvaluationGet from internal/op/analytics instead.
func AnalyticsEvaluationGet(ctx context.Context) (*model.AnalyticsEvaluationSummary, error) {
	return analytics.AnalyticsEvaluationGet(ctx)
}
