package airoute

import (
	"testing"

	"github.com/gypg/lodestar/internal/model"
)

// TestSelectAIRouteForGroupMatchesAfterFreeTierRename pins the interaction that
// made single-group AI routing fail outright.
//
// normalizeAIRouteRequestedModel appends `-free` to the requested model whenever
// any candidate upstream looks like a free tier (see aiRouteLooksLikeFreeTierModel:
// it matches on the substring "free"). That rename happens BEFORE
// selectAIRouteForGroup runs, and the lookup used to require exact equality — so
// for a group named `gpt-4o` whose upstreams include `gpt-4o-free`, the route was
// renamed to `gpt-4o-free` and then never matched its own group. Every run of that
// shape failed with "AI 返回结果未包含目标分组对应路由".
func TestSelectAIRouteForGroupMatchesAfterFreeTierRename(t *testing.T) {
	group := model.Group{ID: 7, Name: "gpt-4o"}
	routes := []model.AIRouteEntry{
		{RequestedModel: "some-other-group", Items: []model.AIRouteItemSpec{{ChannelID: 1, UpstreamModel: "gpt-4o"}}},
		// The renamed route for our target group.
		{RequestedModel: "gpt-4o-free", Items: []model.AIRouteItemSpec{{ChannelID: 2, UpstreamModel: "gpt-4o-free"}}},
	}

	selected, err := selectAIRouteForGroup(group, routes)
	if err != nil {
		t.Fatalf("selectAIRouteForGroup() error = %v, want the -free renamed route to match", err)
	}
	if selected.RequestedModel != "gpt-4o-free" {
		t.Fatalf("selected RequestedModel = %q, want gpt-4o-free", selected.RequestedModel)
	}
	if len(selected.Items) != 1 || selected.Items[0].ChannelID != 2 {
		t.Fatalf("selected wrong route: %+v", selected)
	}
}

// TestSelectAIRouteForGroupPrefersExactMatch is the paired control: when both an
// exact match and a suffixed variant are present, the exact one must win.
// Without this, "strip the suffix from everything" would satisfy the test above
// while quietly picking the wrong route for free-tier groups.
func TestSelectAIRouteForGroupPrefersExactMatch(t *testing.T) {
	group := model.Group{ID: 8, Name: "gpt-4o"}
	routes := []model.AIRouteEntry{
		{RequestedModel: "gpt-4o-free", Items: []model.AIRouteItemSpec{{ChannelID: 2, UpstreamModel: "gpt-4o-free"}}},
		{RequestedModel: "gpt-4o", Items: []model.AIRouteItemSpec{{ChannelID: 3, UpstreamModel: "gpt-4o"}}},
	}

	selected, err := selectAIRouteForGroup(group, routes)
	if err != nil {
		t.Fatalf("selectAIRouteForGroup() error = %v", err)
	}
	if selected.Items[0].ChannelID != 3 {
		t.Fatalf("selected channel = %d, want 3: the exact match must win over the -free variant", selected.Items[0].ChannelID)
	}
}

// TestSelectAIRouteForGroupMatchesFreeTierGroupToBareRoute covers the mirror
// direction: a group that is itself named `...-free` must still match a route the
// model returned without the suffix.
func TestSelectAIRouteForGroupMatchesFreeTierGroupToBareRoute(t *testing.T) {
	group := model.Group{ID: 9, Name: "gemini-2.5-pro-free"}
	routes := []model.AIRouteEntry{
		{RequestedModel: "gemini-2.5-pro", Items: []model.AIRouteItemSpec{{ChannelID: 4, UpstreamModel: "gemini-2.5-pro"}}},
	}

	selected, err := selectAIRouteForGroup(group, routes)
	if err != nil {
		t.Fatalf("selectAIRouteForGroup() error = %v", err)
	}
	if selected.Items[0].ChannelID != 4 {
		t.Fatalf("selected channel = %d, want 4", selected.Items[0].ChannelID)
	}
}

// TestSelectAIRouteForGroupStillRejectsUnrelatedRoutes guards against the
// fallback becoming a fuzzy free-for-all. Stripping a tier suffix must not let a
// different model's route be adopted, otherwise a failed run would silently
// write the wrong upstreams into the group instead of reporting an error.
func TestSelectAIRouteForGroupStillRejectsUnrelatedRoutes(t *testing.T) {
	group := model.Group{ID: 10, Name: "gpt-4o"}
	routes := []model.AIRouteEntry{
		{RequestedModel: "claude-sonnet-4-free", Items: []model.AIRouteItemSpec{{ChannelID: 5, UpstreamModel: "claude-sonnet-4-free"}}},
		{RequestedModel: "gpt-4o-mini", Items: []model.AIRouteItemSpec{{ChannelID: 6, UpstreamModel: "gpt-4o-mini"}}},
	}

	if _, err := selectAIRouteForGroup(group, routes); err == nil {
		t.Fatal("selectAIRouteForGroup() error = nil, want failure: no route belongs to this group")
	}
}

func TestStripAIRouteTierSuffix(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"gpt-4o-free", "gpt-4o"},
		{"gpt-4o-FREE", "gpt-4o"},
		{"gpt-4o-公益", "gpt-4o"},
		{"gpt-4o", "gpt-4o"},
		{"  gpt-4o-free  ", "gpt-4o"},
		// A bare suffix is not a model name; leave it alone rather than
		// collapsing it to the empty string, which would match everything.
		{"-free", "-free"},
		{"", ""},
		// Mid-string occurrences must not be touched.
		{"free-tier-model", "free-tier-model"},
	}

	for _, tt := range tests {
		if got := stripAIRouteTierSuffix(tt.in); got != tt.want {
			t.Errorf("stripAIRouteTierSuffix(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
