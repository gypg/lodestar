package condition

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidCondition is a sentinel marking a condition that failed validation.
// It is wrapped with %w so callers can use errors.Is to distinguish user-input
// errors from internal ones, instead of matching on error text.
var ErrInvalidCondition = errors.New("invalid condition")

// validKeys lists the exact keys accepted by resolveValue. The header_ prefix
// is handled separately in isValidKey because it matches any header_xxx key.
var validKeys = []string{
	"model",
	"api_key_id",
	"api_key_name",
	"content_type",
	"hour",
}

// validOps lists the ops accepted by evaluateRule.
var validOps = []string{
	"equals",
	"not_equals",
	"contains",
	"not_contains",
	"starts_with",
	"ends_with",
	"regex",
	"in_list",
	"gt",
	"lt",
}

var validKeySet = func() map[string]struct{} {
	s := make(map[string]struct{}, len(validKeys))
	for _, k := range validKeys {
		s[k] = struct{}{}
	}
	return s
}()

var validOpSet = func() map[string]struct{} {
	s := make(map[string]struct{}, len(validOps))
	for _, o := range validOps {
		s[o] = struct{}{}
	}
	return s
}()

// ValidateCondition validates a condition JSON string. It returns nil for an
// empty string, whitespace, or an empty array — all of which mean "no
// restriction", which is the normal state for most groups. Any other input must
// be an array of rules whose key and op are recognised by the evaluator;
// otherwise a descriptive error is returned so the user can fix it themselves.
func ValidateCondition(conditionJSON string) error {
	if strings.TrimSpace(conditionJSON) == "" {
		return nil
	}
	var rules []ConditionRule
	if err := json.Unmarshal([]byte(conditionJSON), &rules); err != nil {
		return fmt.Errorf("invalid condition: invalid JSON: %v: %w", err, ErrInvalidCondition)
	}
	if len(rules) == 0 {
		return nil
	}
	for i, rule := range rules {
		ruleNum := i + 1
		if rule.Key == "" {
			return fmt.Errorf("invalid condition: rule %d: key is empty; valid keys: %s, header_*: %w", ruleNum, strings.Join(validKeys, ", "), ErrInvalidCondition)
		}
		if !isValidKey(rule.Key) {
			return fmt.Errorf("invalid condition: rule %d: unknown key %q; valid keys: %s, header_*: %w", ruleNum, rule.Key, strings.Join(validKeys, ", "), ErrInvalidCondition)
		}
		if rule.Op == "" {
			return fmt.Errorf("invalid condition: rule %d: op is empty; valid ops: %s: %w", ruleNum, strings.Join(validOps, ", "), ErrInvalidCondition)
		}
		if _, ok := validOpSet[rule.Op]; !ok {
			return fmt.Errorf("invalid condition: rule %d: unknown op %q; valid ops: %s: %w", ruleNum, rule.Op, strings.Join(validOps, ", "), ErrInvalidCondition)
		}
	}
	return nil
}

// isValidKey reports whether key is recognised by the evaluator. The header_
// prefix check is case-sensitive to match resolveValue, which uses
// strings.HasPrefix(key, "header_") — HEADER_xxx would be an unknown key, so it
// must be rejected here too. The suffix after header_ is not validated because
// resolveValue lowercases it before lookup, so any casing is legal.
func isValidKey(key string) bool {
	if _, ok := validKeySet[key]; ok {
		return true
	}
	return strings.HasPrefix(key, "header_")
}
