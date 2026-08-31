package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
/api/v1/proxy-pool must not be reachable by the end-customer role.

All three routes in this group used to carry only Auth(), and every consequence
below was reproduced against production with a `user`-role token:

  - GET /list returns ProxyConfiguration.URL verbatim (json:"url", no masking).
    A proxy URL routinely embeds credentials as user:pass@host, so a paying
    customer could read every upstream proxy credential the operator configured.
  - DELETE /delete/:id reached the business logic — a nonexistent id answered
    500 (not found) rather than 403, i.e. authorization never ran. A customer
    could delete proxy configurations.
  - POST /test takes proxy_url through NormalizeProxyURL only, which checks the
    scheme and that a host is present and nothing else. The reply distinguishes
    "Not Found" (open) from "connection refused" (closed) from "no route to
    host" (filtered) from a 20s timeout, so it is an internal port-scan oracle.

The user role holds only apikeys:read|write, stats:read and subscriptions:read
(auth/permissions.go), so gating on settings:read / settings:write shuts all
three. Read routes take settings:read; delete and the write routes take
settings:write — deleting is destructive and does not belong behind a read gate.

★ Why source assertions: gin exposes one final handler per route, not the
middleware chain, so a route's gate is not observable from the registered
engine. handlers-package tests mount their own engines and therefore cannot
see the production registration either (measured on the logout route: wrapping
the real route left every handler test green). Hence this file.
*/

func proxyPoolSource(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("handlers", "proxy_pool.go"))
	if err != nil {
		t.Fatalf("read handlers/proxy_pool.go: %v", err)
	}
	return string(src)
}

// proxyPoolBlockFor returns the group-router block that registers the given
// route path, trimmed at the next group so a sibling's gate is not credited
// to it.
func proxyPoolBlockFor(t *testing.T, text, routePath string) string {
	t.Helper()
	const marker = `router.NewGroupRouter("/api/v1/proxy-pool")`
	blocks := strings.Split(text, marker)
	if len(blocks) < 2 {
		t.Fatalf("no /api/v1/proxy-pool group found in handlers/proxy_pool.go")
	}
	for _, b := range blocks[1:] {
		if end := strings.Index(b, "router.NewGroupRouter("); end != -1 {
			b = b[:end]
		}
		if strings.Contains(b, routePath) {
			return b
		}
	}
	t.Fatalf("no /api/v1/proxy-pool group registers %s", routePath)
	return ""
}

// The read routes leak proxy credentials, so they must sit behind a settings
// gate rather than bare Auth().
func TestProxyPoolReadRoutesRequireSettingsPermission(t *testing.T) {
	text := proxyPoolSource(t)
	for _, route := range []string{`"/list"`, `"/references/:id"`} {
		block := proxyPoolBlockFor(t, text, route)
		if !strings.Contains(block, "RequirePermission") {
			t.Fatalf("the group registering %s carries no RequirePermission; "+
				"ProxyConfiguration.URL is returned verbatim and commonly embeds "+
				"user:pass@host, so the end-customer role would read every proxy "+
				"credential. block=%q", route, block)
		}
		if !strings.Contains(block, "PermSettingsRead") && !strings.Contains(block, "PermSettingsWrite") {
			t.Fatalf("the group registering %s gates on neither settings:read nor "+
				"settings:write; the user role holds apikeys/stats/subscriptions only, "+
				"so any other permission would not shut it out. block=%q", route, block)
		}
	}
}

// Deleting is destructive: a read gate is not enough, it needs write.
func TestProxyPoolDeleteRequiresSettingsWrite(t *testing.T) {
	text := proxyPoolSource(t)
	block := proxyPoolBlockFor(t, text, `"/delete/:id"`)
	if !strings.Contains(block, "PermSettingsWrite") {
		t.Fatalf("the group registering /delete/:id does not require settings:write; "+
			"a viewer (settings:read) or an end customer could delete proxy "+
			"configurations. block=%q", block)
	}
}

// create / update / test all mutate or drive outbound requests.
func TestProxyPoolWriteRoutesRequireSettingsWrite(t *testing.T) {
	text := proxyPoolSource(t)
	for _, route := range []string{`"/create"`, `"/update"`, `"/test"`} {
		block := proxyPoolBlockFor(t, text, route)
		if !strings.Contains(block, "PermSettingsWrite") {
			t.Fatalf("the group registering %s does not require settings:write; "+
				"/test in particular dials an operator-supplied proxy_url and its reply "+
				"distinguishes refused / unreachable / timeout, which is an internal "+
				"port-scan oracle. block=%q", route, block)
		}
	}
}
