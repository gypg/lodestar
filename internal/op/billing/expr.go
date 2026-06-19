package billing

/*
Lodestar — expression-based billing integration (billingexpr).

When a model has a billing expression defined (JSON map in the billing_expr setting),
the expression defines the complete pricing contract. Otherwise, falls back to
upstream USD cost passthrough (existing behavior).

Example expression for GPT-4o:
  p * 2.5 + c * 10

This means $2.50/1M input tokens, $10/00/1M output tokens.
*/

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/pkg/billingexpr"
)

var (
	exprCache   map[string]string
	exprCacheMu sync.RWMutex
)

// LoadBillingExprMap returns the per-model billing expression map from settings.
func LoadBillingExprMap() map[string]string {
	raw, _ := setting.GetString(model.SettingKeyBillingExpr)
	if raw == "" || raw == "{}" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// GetExprForModel returns the billing expression for a model (normalized lowercase lookup).
func GetExprForModel(modelName string) (string, bool) {
	m := LoadBillingExprMap()
	if m == nil {
		return "", false
	}
	normalized := strings.ToLower(strings.TrimSpace(modelName))
	if expr, ok := m[normalized]; ok && strings.TrimSpace(expr) != "" {
		return expr, true
	}
	// Try exact match
	if expr, ok := m[modelName]; ok && strings.TrimSpace(expr) != "" {
		return expr, true
	}
	return "", false
}

// ComputeExprCost computes cost in USD for a request using billing expression.
// Returns (costUSD, matchedTier, true) if expression exists for model, or (0, "", false) if not.
func ComputeExprCost(modelName string, inputTokens, outputTokens int) (float64, string, bool) {
	expr, ok := GetExprForModel(modelName)
	if !ok {
		return 0, "", false
	}
	params := billingexpr.TokenParams{
		P: float64(inputTokens),
		C: float64(outputTokens),
	}
	cost, trace, err := billingexpr.RunExpr(expr, params)
	if err != nil {
		return 0, "", false
	}
	return cost, trace.MatchedTier, true
}

// ComputeExprCostFull computes cost with all token dimensions.
func ComputeExprCostFull(modelName string, params billingexpr.TokenParams) (float64, string, bool) {
	expr, ok := GetExprForModel(modelName)
	if !ok {
		return 0, "", false
	}
	cost, trace, err := billingexpr.RunExpr(expr, params)
	if err != nil {
		return 0, "", false
	}
	return cost, trace.MatchedTier, true
}
