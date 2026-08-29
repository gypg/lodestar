package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/server/auth"
)

/*
GET /api/v1/model/capabilities must be reachable by the end-customer role.

It answers "which models exist, on which endpoint types, and are they up" — what
the API-keys page shows a customer so they know what their own key can call. It
originally sat on the /api/v1/model group behind settings:read, purely by
proximity to the admin model routes, and the `user` role deliberately has no
settings:read. So opening the API-keys page 403'd, and because the global query
error handler toasts every rejection, the customer got "permission denied" on
arrival with nothing on screen to explain it.

Loosening it is safe in a way that /market is not: ModelCapability is
{name, endpoints, conversation, available}, while ModelMarketItem embeds
Channels []ModelMarketChannel — channel ids, channel names and per-channel key
counts, i.e. which upstreams are being resold. The tests below pin that
distinction so a future "tidy these two routes together" cannot quietly undo it.

★ These assert on the ROUTE TABLE, not on live requests. Driving a request through
Auth() needs a database (it resolves the user via user.GetByID), and this package's
engine is shared and registered once — see getProductionEngine. handlers/rbac_test.go
owns the DB-backed variant. So the guard here is: the two routes exist, and they do
not share a middleware chain. That is weaker than a real 403/200, and is stated
rather than implied.
*/

func TestModelCapabilitiesAndMarketBothRegistered(t *testing.T) {
	engine := getProductionEngine(t)

	want := map[string]bool{
		"GET /api/v1/model/capabilities": false,
		"GET /api/v1/model/market":       false,
	}
	seen := make([]string, 0, 16)
	for _, r := range engine.Routes() {
		key := r.Method + " " + r.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
		if strings.HasPrefix(r.Path, "/api/v1/model") {
			seen = append(seen, key)
		}
	}
	for key, found := range want {
		if !found {
			t.Fatalf("%s is not registered; registered model routes = %v", key, seen)
		}
	}
}

// /capabilities must not be registered inside a group that applies
// RequirePermission — folding it back under the settings:read group is exactly the
// regression, and it 403s the API-keys page for every paying customer.
//
// ★ This reads the source rather than the route table, because gin exposes only the
// final handler per route (RouteInfo.HandlerFunc is one func, not the chain), so the
// middleware a route carries is not observable from the registered engine. A source
// assertion catches the regression; it does not prove runtime behaviour.
func TestModelCapabilitiesIsNotInAPermissionGatedGroup(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("handlers", "model.go"))
	if err != nil {
		t.Fatalf("read handlers/model.go: %v", err)
	}
	text := string(src)

	const marker = `router.NewGroupRouter("/api/v1/model")`
	// Split on the group boundaries and find the block that registers /capabilities.
	blocks := strings.Split(text, marker)
	if len(blocks) < 3 {
		t.Fatalf("expected at least two /api/v1/model groups (one gated, one not), "+
			"found %d occurrences of %s", len(blocks)-1, marker)
	}

	var found bool
	for i, b := range blocks[1:] {
		if !strings.Contains(b, `"/capabilities"`) {
			continue
		}
		found = true
		// Only inspect up to the next group or the end of init, so a later group's
		// RequirePermission is not attributed to this one.
		if end := strings.Index(b, "router.NewGroupRouter("); end != -1 {
			b = b[:end]
		}
		if strings.Contains(b, "RequirePermission") {
			t.Fatalf("group #%d registers /capabilities and also applies "+
				"RequirePermission; the end-customer role has no settings:read, so the "+
				"API-keys page would 403 on every visit. block=%q", i+1, b)
		}
	}
	if !found {
		t.Fatal(`no /api/v1/model group registers "/capabilities"`)
	}
}

// The premise both of the above rest on, asserted directly against the RBAC table:
// the end-customer role has no settings:read, so any route requiring it is closed
// to them. If this flips, the gating above stops being about this role.
func TestEndCustomerLacksSettingsReadButHoldsAPIKeyAccess(t *testing.T) {
	if auth.HasPermission(model.UserRoleUser, auth.PermSettingsRead) {
		t.Errorf("role %q now holds settings:read; the capabilities/market split was "+
			"designed around it lacking that permission", model.UserRoleUser)
	}
	// Held, so the check above is not passing merely because the role has nothing.
	if !auth.HasPermission(model.UserRoleUser, auth.PermAPIKeysRead) {
		t.Errorf("role %q lost apikeys:read; it cannot use the API-keys page at all",
			model.UserRoleUser)
	}
	// viewer is read-only STAFF and does hold settings:read -- the reason the
	// frontend gates on permissions rather than an admin-or-editor role test.
	if !auth.HasPermission(model.UserRoleViewer, auth.PermSettingsRead) {
		t.Errorf("role %q lost settings:read", model.UserRoleViewer)
	}
}
