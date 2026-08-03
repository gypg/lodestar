package condition

import (
	"strings"
	"testing"
)

// testCtx is the shared RequestContext used unless a case needs different fields.
func testCtx() RequestContext {
	return RequestContext{
		Model:       "GPT-4o",
		APIKeyID:    42,
		APIKeyName:  "prod",
		ContentType: "application/json",
		Hour:        9,
		Headers:     map[string]string{"x-tenant": "acme"},
	}
}

// TestEvaluateRules locks in the current routing/condition evaluation behavior.
// Behavior notes locked here:
//   - Empty, whitespace-only, and "[]" conditions all pass (fail-open).
//   - Non-JSON input returns an error.
//   - "equals" is case-insensitive via strings.EqualFold, so "gpt-4o" matches
//     the ctx Model "GPT-4o".
//   - "header_X-Tenant" matches because the source lowercases the part after
//     "header_" before consulting the Headers map.
//   - An unknown key fails closed: the rule does not match, so a mistyped key
//     cannot turn into an always-true rule.
//   - "gt" against a non-numeric operand fails closed rather than coercing it
//     to 0.
//   - "regex" with a bad pattern fails closed.
func TestEvaluateRules(t *testing.T) {
	ctx := testCtx()
	tests := []struct {
		name    string
		cond    string
		wantOK  bool
		wantErr bool
	}{
		{name: "empty string passes", cond: ``, wantOK: true},
		{name: "whitespace passes", cond: `   `, wantOK: true},
		{name: "empty array passes", cond: `[]`, wantOK: true},
		{name: "invalid json errors", cond: `not json`, wantOK: false, wantErr: true},
		{name: "model equals case-insensitive", cond: `[{"key":"model","op":"equals","value":"gpt-4o"}]`, wantOK: true},
		{name: "header key case-insensitive lookup", cond: `[{"key":"header_X-Tenant","op":"equals","value":"acme"}]`, wantOK: true},
		{name: "hour gt numeric", cond: `[{"key":"hour","op":"gt","value":"5"}]`, wantOK: true},
		{name: "model gt non-numeric fails", cond: `[{"key":"model","op":"gt","value":"5"}]`, wantOK: false},
		{name: "unknown key fails closed", cond: `[{"key":"unknown_key","op":"equals","value":""}]`, wantOK: false},
		{name: "unknown op fails closed", cond: `[{"key":"model","op":"bogus_op","value":"x"}]`, wantOK: false},
		{name: "regex bad pattern fails", cond: `[{"key":"model","op":"regex","value":"["}]`, wantOK: false},
		{name: "in_list case-insensitive", cond: `[{"key":"model","op":"in_list","value":"a, GPT-4O ,b"}]`, wantOK: true},
		{name: "in_list no match", cond: `[{"key":"model","op":"in_list","value":"a,b,c"}]`, wantOK: false},
		{name: "hour gt equal numeric", cond: `[{"key":"hour","op":"gt","value":"9"}]`, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := Evaluate(tt.cond, ctx)
			if ok != tt.wantOK {
				t.Errorf("Evaluate(%q) ok = %v, want %v", tt.cond, ok, tt.wantOK)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("Evaluate(%q) err = %v, wantErr=%v", tt.cond, err, tt.wantErr)
			}
		})
	}
}

// TestEvaluateAndSemantics locks in that multiple rules act as AND: when one
// rule passes and the next fails, the whole condition evaluates to false.
func TestEvaluateAndSemantics(t *testing.T) {
	ctx := testCtx()
	// First rule (model equals gpt-4o) passes, second rule (model equals WRONG) fails.
	cond := `[{"key":"model","op":"equals","value":"gpt-4o"},{"key":"model","op":"equals","value":"WRONG"}]`
	ok, err := Evaluate(cond, ctx)
	if err != nil {
		t.Fatalf("Evaluate(%q) unexpected err: %v", cond, err)
	}
	if ok {
		t.Errorf("Evaluate(%q) = true, want false (AND semantics)", cond)
	}
}

// TestEvaluateRemainingOps locks in the string ops not covered by the existing
// suite. All are case-insensitive, but via different mechanisms: not_equals via
// strings.EqualFold, and contains/not_contains/starts_with/ends_with by lowercasing
// both sides. Each case proves a differently-cased input still matches.
func TestEvaluateRemainingOps(t *testing.T) {
	ctx := testCtx()
	tests := []struct {
		name   string
		cond   string
		wantOK bool
	}{
		{name: "not_equals different value", cond: `[{"key":"model","op":"not_equals","value":"WRONG"}]`, wantOK: true},
		{name: "not_equals case-insensitive equal", cond: `[{"key":"model","op":"not_equals","value":"gpt-4o"}]`, wantOK: false},
		{name: "contains uppercase", cond: `[{"key":"model","op":"contains","value":"GPT"}]`, wantOK: true},
		{name: "contains lowercase", cond: `[{"key":"model","op":"contains","value":"gpt"}]`, wantOK: true},
		{name: "not_contains absent", cond: `[{"key":"model","op":"not_contains","value":"zzz"}]`, wantOK: true},
		{name: "not_contains present", cond: `[{"key":"model","op":"not_contains","value":"gpt"}]`, wantOK: false},
		{name: "starts_with lowercase", cond: `[{"key":"model","op":"starts_with","value":"gpt"}]`, wantOK: true},
		{name: "ends_with uppercase", cond: `[{"key":"model","op":"ends_with","value":"4O"}]`, wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := Evaluate(tt.cond, ctx)
			if err != nil {
				t.Fatalf("Evaluate(%q) unexpected err: %v", tt.cond, err)
			}
			if ok != tt.wantOK {
				t.Errorf("Evaluate(%q) = %v, want %v", tt.cond, ok, tt.wantOK)
			}
		})
	}
}

// TestEvaluateLtNumeric locks in the lt op on its numeric path: with hour=9,
// lt "10" is true. lt shares compareInt with gt, so a non-numeric operand
// fails closed instead of being coerced to 0.
func TestEvaluateLtNumeric(t *testing.T) {
	ctx := testCtx()
	tests := []struct {
		name   string
		cond   string
		wantOK bool
	}{
		{name: "hour lt larger", cond: `[{"key":"hour","op":"lt","value":"10"}]`, wantOK: true},
		{name: "hour lt smaller", cond: `[{"key":"hour","op":"lt","value":"5"}]`, wantOK: false},
		{name: "non-numeric operand fails closed", cond: `[{"key":"model","op":"lt","value":"9"}]`, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := Evaluate(tt.cond, ctx)
			if err != nil {
				t.Fatalf("Evaluate(%q) unexpected err: %v", tt.cond, err)
			}
			if ok != tt.wantOK {
				t.Errorf("Evaluate(%q) = %v, want %v", tt.cond, ok, tt.wantOK)
			}
		})
	}
}

// TestEvaluateResolveKeys locks in the key resolution paths not covered by the
// existing suite: api_key_id (int stringified), api_key_name, content_type, and
// the header_ prefix miss that yields "" but is still a recognised key (an
// absent request header is normal input, unlike an unknown key).
func TestEvaluateResolveKeys(t *testing.T) {
	ctx := testCtx()
	tests := []struct {
		name   string
		cond   string
		wantOK bool
	}{
		{name: "api_key_id equals stringified", cond: `[{"key":"api_key_id","op":"equals","value":"42"}]`, wantOK: true},
		{name: "api_key_id gt", cond: `[{"key":"api_key_id","op":"gt","value":"41"}]`, wantOK: true},
		{name: "api_key_id lt", cond: `[{"key":"api_key_id","op":"lt","value":"43"}]`, wantOK: true},
		{name: "api_key_name equals", cond: `[{"key":"api_key_name","op":"equals","value":"prod"}]`, wantOK: true},
		{name: "content_type equals", cond: `[{"key":"content_type","op":"equals","value":"application/json"}]`, wantOK: true},
		{name: "header miss equals empty passes", cond: `[{"key":"header_X-Missing","op":"equals","value":""}]`, wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := Evaluate(tt.cond, ctx)
			if err != nil {
				t.Fatalf("Evaluate(%q) unexpected err: %v", tt.cond, err)
			}
			if ok != tt.wantOK {
				t.Errorf("Evaluate(%q) = %v, want %v", tt.cond, ok, tt.wantOK)
			}
		})
	}
}

// TestEvaluateEmptyStringContainsSemantics locks in the standard-library
// behavior that strings.Contains(x, "") is always true: contains with an empty
// value always passes, and therefore not_contains with an empty value always
// fails. This is current behavior, not a bug.
func TestEvaluateEmptyStringContainsSemantics(t *testing.T) {
	ctx := testCtx()
	tests := []struct {
		name   string
		cond   string
		wantOK bool
	}{
		{name: "contains empty always true", cond: `[{"key":"model","op":"contains","value":""}]`, wantOK: true},
		{name: "not_contains empty always false", cond: `[{"key":"model","op":"not_contains","value":""}]`, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := Evaluate(tt.cond, ctx)
			if err != nil {
				t.Fatalf("Evaluate(%q) unexpected err: %v", tt.cond, err)
			}
			if ok != tt.wantOK {
				t.Errorf("Evaluate(%q) = %v, want %v", tt.cond, ok, tt.wantOK)
			}
		})
	}
}

// TestEvaluateRegexMatching locks in the regex op's success and non-match paths
// using only literal anchors (no \d / \w / \s shorthand, which behave unlike the
// standard library under regexp2's ECMAScript mode). regex is the only
// case-sensitive op: ^GPT matches "GPT-4o" but ^gpt does not.
func TestEvaluateRegexMatching(t *testing.T) {
	ctx := testCtx()
	tests := []struct {
		name   string
		cond   string
		wantOK bool
	}{
		{name: "literal anchor matches", cond: `[{"key":"model","op":"regex","value":"^GPT"}]`, wantOK: true},
		{name: "case-sensitive does not match", cond: `[{"key":"model","op":"regex","value":"^gpt"}]`, wantOK: false},
		{name: "end anchor matches", cond: `[{"key":"model","op":"regex","value":"4o$"}]`, wantOK: true},
		{name: "no match", cond: `[{"key":"model","op":"regex","value":"zzz"}]`, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := Evaluate(tt.cond, ctx)
			if err != nil {
				t.Fatalf("Evaluate(%q) unexpected err: %v", tt.cond, err)
			}
			if ok != tt.wantOK {
				t.Errorf("Evaluate(%q) = %v, want %v", tt.cond, ok, tt.wantOK)
			}
		})
	}
}

// TestEvaluateRegexTimeout locks in the match-timeout path: a catastrophic
// pattern against a long input busts the 250ms MatchTimeout and evaluateRule
// returns false (an internal error, not surfaced), so Evaluate returns ok=false
// with no error. This is the only way to reach that branch without a long Model.
func TestEvaluateRegexTimeout(t *testing.T) {
	ctx := RequestContext{Model: strings.Repeat("a", 30) + "b"}
	cond := `[{"key":"model","op":"regex","value":"(a+)+$"}]`
	ok, err := Evaluate(cond, ctx)
	if err != nil {
		t.Fatalf("Evaluate(%q) unexpected err: %v", cond, err)
	}
	if ok {
		t.Errorf("Evaluate(%q) = true, want false (match timeout swallowed)", cond)
	}
}

// TestUnknownKeyFailsClosedForEveryOp locks in that an unknown key makes the
// rule fail closed under every operator. Fail-open is blocked at the top of
// evaluateRule, so every op must be covered here; a rule whose key is a typo
// must never match.
func TestUnknownKeyFailsClosedForEveryOp(t *testing.T) {
	ctx := testCtx()
	tests := []struct {
		name   string
		cond   string
		wantOK bool
	}{
		{name: "equals", cond: `[{"key":"nonsense","op":"equals","value":"x"}]`, wantOK: false},
		{name: "not_equals", cond: `[{"key":"nonsense","op":"not_equals","value":"x"}]`, wantOK: false},
		{name: "contains", cond: `[{"key":"nonsense","op":"contains","value":"x"}]`, wantOK: false},
		{name: "not_contains", cond: `[{"key":"nonsense","op":"not_contains","value":"x"}]`, wantOK: false},
		{name: "starts_with", cond: `[{"key":"nonsense","op":"starts_with","value":"x"}]`, wantOK: false},
		{name: "ends_with", cond: `[{"key":"nonsense","op":"ends_with","value":"x"}]`, wantOK: false},
		{name: "in_list", cond: `[{"key":"nonsense","op":"in_list","value":"a,b"}]`, wantOK: false},
		{name: "regex", cond: `[{"key":"nonsense","op":"regex","value":".*"}]`, wantOK: false},
		{name: "gt", cond: `[{"key":"nonsense","op":"gt","value":"-1"}]`, wantOK: false},
		{name: "lt", cond: `[{"key":"nonsense","op":"lt","value":"5"}]`, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := Evaluate(tt.cond, ctx)
			if err != nil {
				t.Fatalf("Evaluate(%q) unexpected err: %v", tt.cond, err)
			}
			if ok != tt.wantOK {
				t.Errorf("Evaluate(%q) = %v, want %v", tt.cond, ok, tt.wantOK)
			}
		})
	}
}

// TestNonNumericOperandFailsClosed locks in that a non-numeric operand in gt/lt
// fails closed instead of silently coercing to 0. This is the compareInt path
// that previously turned a mistyped rule into an always-true comparison.
func TestNonNumericOperandFailsClosed(t *testing.T) {
	ctx := testCtx()
	tests := []struct {
		name   string
		cond   string
		wantOK bool
	}{
		{name: "model gt -1", cond: `[{"key":"model","op":"gt","value":"-1"}]`, wantOK: false},
		{name: "model lt 9", cond: `[{"key":"model","op":"lt","value":"9"}]`, wantOK: false},
		{name: "hour lt abc", cond: `[{"key":"hour","op":"lt","value":"abc"}]`, wantOK: false},
		{name: "hour gt empty", cond: `[{"key":"hour","op":"gt","value":""}]`, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := Evaluate(tt.cond, ctx)
			if err != nil {
				t.Fatalf("Evaluate(%q) unexpected err: %v", tt.cond, err)
			}
			if ok != tt.wantOK {
				t.Errorf("Evaluate(%q) = %v, want %v", tt.cond, ok, tt.wantOK)
			}
		})
	}
}

// TestNumericComparisonStillWorks locks in the regression guard: valid numeric
// comparisons still work after compareInt gained a second return value. Trimming
// keeps " 10 " usable as a value.
func TestNumericComparisonStillWorks(t *testing.T) {
	ctx := testCtx()
	tests := []struct {
		name   string
		cond   string
		wantOK bool
	}{
		{name: "hour lt 10", cond: `[{"key":"hour","op":"lt","value":"10"}]`, wantOK: true},
		{name: "hour gt 5", cond: `[{"key":"hour","op":"gt","value":"5"}]`, wantOK: true},
		{name: "hour gt 9", cond: `[{"key":"hour","op":"gt","value":"9"}]`, wantOK: false},
		{name: "api_key_id gt 40", cond: `[{"key":"api_key_id","op":"gt","value":"40"}]`, wantOK: true},
		{name: "hour lt padded", cond: `[{"key":"hour","op":"lt","value":" 10 "}]`, wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := Evaluate(tt.cond, ctx)
			if err != nil {
				t.Fatalf("Evaluate(%q) unexpected err: %v", tt.cond, err)
			}
			if ok != tt.wantOK {
				t.Errorf("Evaluate(%q) = %v, want %v", tt.cond, ok, tt.wantOK)
			}
		})
	}
}

// TestHeaderMissIsKnownKeyNotUnknown locks in the distinction between an absent
// header (a recognised key with an empty value) and an unknown key. An absent
// header is normal input and must not fail closed.
func TestHeaderMissIsKnownKeyNotUnknown(t *testing.T) {
	ctx := testCtx()
	tests := []struct {
		name   string
		cond   string
		wantOK bool
	}{
		{name: "absent header equals empty", cond: `[{"key":"header_absent","op":"equals","value":""}]`, wantOK: true},
		{name: "absent header not_equals", cond: `[{"key":"header_absent","op":"not_equals","value":"x"}]`, wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := Evaluate(tt.cond, ctx)
			if err != nil {
				t.Fatalf("Evaluate(%q) unexpected err: %v", tt.cond, err)
			}
			if ok != tt.wantOK {
				t.Errorf("Evaluate(%q) = %v, want %v", tt.cond, ok, tt.wantOK)
			}
		})
	}
}

// TestUnknownKeyPoisonsEntireRuleSet locks in the AND semantics combined with
// fail-closed: a single unknown key poisons the whole rule set (false), and the
// control case with all-known keys stays true. The control proves AND is what
// produces the false, not a broken function always returning false.
func TestUnknownKeyPoisonsEntireRuleSet(t *testing.T) {
	ctx := testCtx()
	tests := []struct {
		name   string
		cond   string
		wantOK bool
	}{
		{name: "unknown key poisons set", cond: `[{"key":"model","op":"equals","value":"gpt-4o"},{"key":"typo","op":"lt","value":"5"}]`, wantOK: false},
		{name: "control all known", cond: `[{"key":"model","op":"equals","value":"gpt-4o"},{"key":"hour","op":"lt","value":"10"}]`, wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := Evaluate(tt.cond, ctx)
			if err != nil {
				t.Fatalf("Evaluate(%q) unexpected err: %v", tt.cond, err)
			}
			if ok != tt.wantOK {
				t.Errorf("Evaluate(%q) = %v, want %v", tt.cond, ok, tt.wantOK)
			}
		})
	}
}
