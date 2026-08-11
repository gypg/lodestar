package server

import (
	"net/http"
	"strings"
	"testing"
)

// Issue #4 cause 6 regression guard: analytics/AIRouteConfig.tsx falls back to
// GET /api/v1/channel/:id when the cached /channel/list entry for the selected
// model carries no usable base_urls/keys, and that route was never registered —
// so the fallback could only ever 404, leaving the AI route base_url/api_key
// unfilled with no way forward.
//
// ★ This lives in internal/server, not internal/server/handlers: RegisterAll
// nils the global route table when it completes (router.go:124), so it can only
// succeed once per process, and handlers/rbac_test.go already consumes that one
// call. Asserting on the production table from there yields an empty table and
// order-dependent results. getProductionEngine (webauthn_ratelimit_route_test.go)
// is this package's shared, single-registration engine.
func TestChannelDetailRouteIsRegistered(t *testing.T) {
	engine := getProductionEngine(t)

	channelRoutes := make([]string, 0, 16)
	for _, r := range engine.Routes() {
		if r.Method == http.MethodGet && r.Path == "/api/v1/channel/:id" {
			return
		}
		if strings.HasPrefix(r.Path, "/api/v1/channel/") {
			channelRoutes = append(channelRoutes, r.Method+" "+r.Path)
		}
	}

	t.Fatalf("GET /api/v1/channel/:id is not registered, so AIRouteConfig's channel refetch can only 404; registered channel routes = %v", channelRoutes)
}

// The bare /:id wildcard sits at the same tree level as the static /list and
// /group/list routes. gin resolves static segments before wildcards, but a
// careless reordering could shadow them, and gin would not complain — the
// static route would simply stop being reachable. This pins that all three
// survive registration together.
func TestChannelDetailRouteDoesNotShadowStaticSiblings(t *testing.T) {
	engine := getProductionEngine(t)

	required := map[string]bool{
		"GET /api/v1/channel/list":       false,
		"GET /api/v1/channel/group/list": false,
		"GET /api/v1/channel/:id":        false,
	}
	for _, r := range engine.Routes() {
		key := r.Method + " " + r.Path
		if _, ok := required[key]; ok {
			required[key] = true
		}
	}

	for key, found := range required {
		if !found {
			t.Errorf("route %q is missing from the production table", key)
		}
	}
}
