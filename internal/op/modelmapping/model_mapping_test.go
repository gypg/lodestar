package modelmapping

import (
	"context"
	"regexp"
	"testing"

	"github.com/gypg/lodestar/internal/model"
)

// setCache replaces the in-memory cache + compiled regex map under the lock.
// Lets us exercise Resolve/matchPattern without a database.
func setCache(mappings []*model.ModelMapping, regexByID map[int]*regexp.Regexp) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	mappingsCache = mappings
	compiledRegex = regexByID
}

func ptrInt(v int) *int { return &v }

func TestMatchWildcard(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		// star
		{"*", "anything", true},
		{"*", "", true},
		{"**", "anything", true},
		{"gpt-*", "gpt-4o", true},
		{"*-4o", "gpt-4o", true},
		{"g*t-4", "gpt-4", true},
		{"a*b*c", "aXXbYYc", true},
		{"a*b*c", "aXXbYY", false}, // trailing c missing
		// question mark
		{"gpt-?", "gpt-4", true},
		{"gpt-?", "gpt-4o", false}, // ? is exactly one char
		{"a?c", "abc", true},
		{"a?c", "ac", false},
		// exact-ish (no wildcard chars)
		{"gpt-4", "gpt-4", true},
		{"gpt-4", "gpt-3", false},
		// case-insensitive
		{"GPT-4", "gpt-4", true},
		{"gpt-*", "GPT-4O", true},
		// empties
		{"", "", true},
		{"", "a", false},
		{"a", "", false},
	}
	for _, c := range cases {
		if got := matchWildcard(c.pattern, c.s); got != c.want {
			t.Errorf("matchWildcard(%q, %q) = %v, want %v", c.pattern, c.s, got, c.want)
		}
	}
}

func TestMatchPatternExact(t *testing.T) {
	m := &model.ModelMapping{ID: 1, MatchType: model.MatchExact, Pattern: "GPT-4"}
	if !matchPattern(m, "gpt-4") {
		t.Errorf("exact match GPT-4 vs gpt-4: want true (case-insensitive)")
	}
	if matchPattern(m, "gpt-3") {
		t.Errorf("exact match GPT-4 vs gpt-3: want false")
	}
}

func TestMatchPatternWildcard(t *testing.T) {
	m := &model.ModelMapping{ID: 2, MatchType: model.MatchWildcard, Pattern: "claude-*"}
	if !matchPattern(m, "claude-sonnet") {
		t.Errorf("wildcard claude-* vs claude-sonnet: want true")
	}
	if matchPattern(m, "gpt-4") {
		t.Errorf("wildcard claude-* vs gpt-4: want false")
	}
}

func TestMatchPatternRegex(t *testing.T) {
	re := regexp.MustCompile(`^gpt-(3|4)$`)
	m := &model.ModelMapping{ID: 3, MatchType: model.MatchRegex, Pattern: `^gpt-(3|4)$`}
	// matchPattern reads compiledRegex[id]; missing entry ⇒ no match.
	if matchPattern(m, "gpt-4") {
		t.Errorf("regex match before compile registered: want false")
	}
	setCache(nil, map[int]*regexp.Regexp{3: re})
	if !matchPattern(m, "gpt-4") {
		t.Errorf("regex ^gpt-(3|4)$ vs gpt-4: want true")
	}
	if matchPattern(m, "gpt-5") {
		t.Errorf("regex ^gpt-(3|4)$ vs gpt-5: want false")
	}
}

func TestMatchPatternUnknownType(t *testing.T) {
	m := &model.ModelMapping{ID: 4, MatchType: "bogus", Pattern: "x"}
	if matchPattern(m, "x") {
		t.Errorf("unknown match type should not match")
	}
}

func TestResolveEmptyCacheReturnsOriginal(t *testing.T) {
	setCache(nil, nil)
	if got := Resolve(context.Background(), "gpt-4", 1); got != "gpt-4" {
		t.Errorf("empty cache Resolve = %q, want %q", got, "gpt-4")
	}
}

func TestResolveFirstMatchWinsByCacheOrder(t *testing.T) {
	// InitCache sorts by priority DESC then id ASC; we simulate that order
	// directly: the first matching rule in cache order wins.
	setCache([]*model.ModelMapping{
		{ID: 1, MatchType: model.MatchWildcard, Pattern: "gpt-*", TargetModel: "gpt-first"},
		{ID: 2, MatchType: model.MatchExact, Pattern: "gpt-4", TargetModel: "gpt-second"},
	}, nil)
	if got := Resolve(context.Background(), "gpt-4", 0); got != "gpt-first" {
		t.Errorf("first-match-wins: Resolve = %q, want %q", got, "gpt-first")
	}
}

func TestResolveNoMatchReturnsOriginal(t *testing.T) {
	setCache([]*model.ModelMapping{
		{ID: 1, MatchType: model.MatchExact, Pattern: "gpt-4", TargetModel: "gpt-4o"},
	}, nil)
	if got := Resolve(context.Background(), "claude-3", 0); got != "claude-3" {
		t.Errorf("no-match Resolve = %q, want %q", got, "claude-3")
	}
}

func TestResolveScopeGroupGlobalAppliesToAll(t *testing.T) {
	// ScopeGroupID == nil means the rule is global.
	setCache([]*model.ModelMapping{
		{ID: 1, MatchType: model.MatchExact, Pattern: "gpt-4", TargetModel: "global-4o", ScopeGroupID: nil},
	}, nil)
	if got := Resolve(context.Background(), "gpt-4", 999); got != "global-4o" {
		t.Errorf("global rule on group 999: Resolve = %q, want %q", got, "global-4o")
	}
}

func TestResolveScopeGroupSpecificSkipsOtherGroups(t *testing.T) {
	// Rule scoped to group 7 must NOT apply to group 8.
	setCache([]*model.ModelMapping{
		{ID: 1, MatchType: model.MatchExact, Pattern: "gpt-4", TargetModel: "group7-only", ScopeGroupID: ptrInt(7)},
	}, nil)
	if got := Resolve(context.Background(), "gpt-4", 8); got != "gpt-4" {
		t.Errorf("group-scoped rule leaked to other group: Resolve = %q, want %q", got, "gpt-4")
	}
	if got := Resolve(context.Background(), "gpt-4", 7); got != "group7-only" {
		t.Errorf("group-scoped rule on its own group: Resolve = %q, want %q", got, "group7-only")
	}
}

func TestResolveScopeGroupFallsThroughToNextRule(t *testing.T) {
	// A scoped rule that doesn't apply to the current group should be skipped
	// and a later global rule should still match.
	setCache([]*model.ModelMapping{
		{ID: 1, MatchType: model.MatchExact, Pattern: "gpt-4", TargetModel: "scoped", ScopeGroupID: ptrInt(7)},
		{ID: 2, MatchType: model.MatchExact, Pattern: "gpt-4", TargetModel: "global", ScopeGroupID: nil},
	}, nil)
	if got := Resolve(context.Background(), "gpt-4", 8); got != "global" {
		t.Errorf("fall-through to global: Resolve = %q, want %q", got, "global")
	}
}
