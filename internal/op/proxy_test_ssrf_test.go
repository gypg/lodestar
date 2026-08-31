package op

import (
	"context"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/model"
)

// ProxyConfigurationTest validated only the target URL, never the proxy URL it
// was told to dial. NormalizeProxyURL checks the scheme and that a host is
// present, nothing more, so an internal proxy_url made this process connect to
// that address and the reply distinguished "connection refused" (port closed)
// from "no route to host" (filtered) from a 20s timeout (reachable, silent) —
// an internal port-scan oracle. Reproduced against production before the fix.
//
// The permission gate (settings:write) is the first line and is pinned in
// internal/server/proxy_pool_route_test.go. This is the second: even an
// operator who legitimately holds that permission should not be able to point
// the tester at the loopback or the cloud metadata address.
func TestProxyConfigurationTestRejectsInternalProxyURL(t *testing.T) {
	cases := []struct {
		name     string
		proxyURL string
	}{
		{"loopback", "http://127.0.0.1:9999"},
		{"loopback name", "http://localhost:9999"},
		{"cloud metadata", "http://169.254.169.254:80"},
		{"private range", "http://10.0.0.1:8080"},
		{"docker host gateway", "http://172.17.0.1:8081"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ProxyConfigurationTest(model.ProxyTestRequest{
				URL:      "https://example.com",
				ProxyURL: tc.proxyURL,
			}, context.Background())
			if err != nil {
				t.Fatalf("ProxyConfigurationTest returned a transport error: %v", err)
			}
			if res.Success {
				t.Fatalf("proxy_url %q was accepted (success=true); an internal address must be refused", tc.proxyURL)
			}
			if !strings.Contains(res.Message, "proxy url is not allowed") {
				t.Fatalf("proxy_url %q rejected with %q, want the SSRF refusal "+
					"(\"proxy url is not allowed\"). A dial-level failure such as "+
					"\"connection refused\" or \"no route to host\" means the request was "+
					"actually attempted, which is exactly the port-scan oracle.",
					tc.proxyURL, res.Message)
			}
		})
	}
}

// A public proxy address must still be accepted — the guard has to reject
// internal targets, not every proxy. The dial itself is expected to fail here
// (nothing proxies on that port), so this asserts only that the rejection is
// NOT the SSRF one, i.e. validation was passed.
//
// The host must actually resolve: AssertSafeHost refuses anything it cannot
// resolve, so a made-up hostname fails at DNS and looks like an SSRF refusal
// without exercising the internal-address check at all. Refusing an
// unresolvable proxy is correct — it just cannot serve as this test's control.
func TestProxyConfigurationTestAllowsPublicProxyURL(t *testing.T) {
	res, err := ProxyConfigurationTest(model.ProxyTestRequest{
		URL:      "https://example.com",
		ProxyURL: "http://example.com:8080",
	}, context.Background())
	if err != nil {
		t.Fatalf("ProxyConfigurationTest returned a transport error: %v", err)
	}
	if strings.Contains(res.Message, "proxy url is not allowed") {
		t.Fatalf("a public proxy address was refused by the SSRF guard: %q — the "+
			"guard must reject internal addresses only", res.Message)
	}
}
