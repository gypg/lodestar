package condition

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/gypg/lodestar/internal/utils/xregexp"
)

// ConditionRule represents a single routing condition.
type ConditionRule struct {
	Key   string `json:"key"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// RequestContext provides request metadata for condition evaluation.
type RequestContext struct {
	Model       string
	APIKeyID    int
	APIKeyName  string
	Headers     map[string]string
	ContentType string
	Hour        int // 0-23 UTC
}

// Evaluate returns true if all rules pass (AND logic). An empty condition always passes.
func Evaluate(conditionJSON string, ctx RequestContext) (bool, error) {
	if strings.TrimSpace(conditionJSON) == "" {
		return true, nil
	}
	var rules []ConditionRule
	if err := json.Unmarshal([]byte(conditionJSON), &rules); err != nil {
		return false, err
	}
	if len(rules) == 0 {
		return true, nil
	}
	for _, rule := range rules {
		if !evaluateRule(rule, ctx) {
			return false, nil
		}
	}
	return true, nil
}

func evaluateRule(rule ConditionRule, ctx RequestContext) bool {
	actual, known := resolveValue(rule.Key, ctx)
	if !known {
		return false
	}
	switch rule.Op {
	case "equals":
		return strings.EqualFold(actual, rule.Value)
	case "not_equals":
		return !strings.EqualFold(actual, rule.Value)
	case "contains":
		return strings.Contains(strings.ToLower(actual), strings.ToLower(rule.Value))
	case "not_contains":
		return !strings.Contains(strings.ToLower(actual), strings.ToLower(rule.Value))
	case "starts_with":
		return strings.HasPrefix(strings.ToLower(actual), strings.ToLower(rule.Value))
	case "ends_with":
		return strings.HasSuffix(strings.ToLower(actual), strings.ToLower(rule.Value))
	case "regex":
		re, err := xregexp.CompileECMAScript(rule.Value)
		if err != nil {
			return false
		}
		match, err := re.MatchString(actual)
		if err != nil {
			return false
		}
		return match
	case "in_list":
		return inList(actual, rule.Value)
	case "gt":
		cmp, ok := compareInt(actual, rule.Value)
		return ok && cmp > 0
	case "lt":
		cmp, ok := compareInt(actual, rule.Value)
		return ok && cmp < 0
	default:
		return false
	}
}

// resolveValue maps a condition key to its request value. The second return
// value reports whether the key is recognised at all; unknown keys must not be
// treated as an empty string, because that silently turns a typo into an
// always-true rule.
func resolveValue(key string, ctx RequestContext) (string, bool) {
	switch {
	case key == "model":
		return ctx.Model, true
	case key == "api_key_id":
		return strconv.Itoa(ctx.APIKeyID), true
	case key == "api_key_name":
		return ctx.APIKeyName, true
	case key == "content_type":
		return ctx.ContentType, true
	case key == "hour":
		return strconv.Itoa(ctx.Hour), true
	case strings.HasPrefix(key, "header_"):
		headerName := strings.TrimPrefix(key, "header_")
		if v, ok := ctx.Headers[strings.ToLower(headerName)]; ok {
			return v, true
		}
		return "", true
	default:
		return "", false
	}
}

func inList(value, csv string) bool {
	for _, item := range strings.Split(csv, ",") {
		if strings.EqualFold(strings.TrimSpace(item), value) {
			return true
		}
	}
	return false
}

// compareInt compares two decimal strings numerically. The second return value
// reports whether both operands parsed; a non-numeric operand must not silently
// become 0, because that turns a mistyped rule into an always-true comparison.
func compareInt(a, b string) (int, bool) {
	av, err := strconv.Atoi(strings.TrimSpace(a))
	if err != nil {
		return 0, false
	}
	bv, err := strconv.Atoi(strings.TrimSpace(b))
	if err != nil {
		return 0, false
	}
	if av > bv {
		return 1, true
	}
	if av < bv {
		return -1, true
	}
	return 0, true
}
