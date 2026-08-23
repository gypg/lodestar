package proxydial

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// sentinelProxy is a non-nil Transport.Proxy used to prove Apply actively
// clears it on the socks path. A socks proxy reached through Transport.Proxy
// would be sent an HTTP CONNECT instead of a SOCKS handshake, so leaving a
// stale Proxy set is a real failure mode, not a cosmetic one.
func sentinelProxy(*http.Request) (*url.URL, error) {
	return &url.URL{Scheme: "http", Host: "sentinel.invalid"}, nil
}

func TestApply_HTTPSchemesSetProxyAndLeaveDialContextAlone(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			transport := &http.Transport{}
			raw := scheme + "://proxy.example.com:8080"

			if err := Apply(transport, raw); err != nil {
				t.Fatalf("Apply(%q) error = %v, want nil", raw, err)
			}
			if transport.Proxy == nil {
				t.Fatal("Proxy = nil, want the proxy URL to be installed")
			}
			if transport.DialContext != nil {
				t.Fatal("DialContext was set; http/https must route via Transport.Proxy, not a custom dialer")
			}

			got, err := transport.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "upstream.example.com"}})
			if err != nil {
				t.Fatalf("Proxy() error = %v", err)
			}
			if got == nil || got.String() != raw {
				t.Fatalf("Proxy() = %v, want %q", got, raw)
			}
		})
	}
}

// "socks" is accepted by model.NormalizeProxyURL, so proxy-pool rows can carry
// that spelling — but x/net/proxy only registers socks5/socks5h. Both spellings
// must end up dialling the proxy, which is why this runs the installed
// DialContext instead of only checking that it is non-nil: a caller starting
// from an http.DefaultTransport clone already has a non-nil DialContext, so
// "is set" alone would be a weak assertion there.
func TestApply_SocksSchemesClearProxyAndInstallDialer(t *testing.T) {
	// Nothing listens on port 9 (discard), so the dial is refused immediately
	// and the error names the address that was dialled. No DNS, no network.
	//
	// The assertion below matches `proxyAddr + "->"`, not proxyAddr alone: when a
	// socks URL carries no port, x/net defaults it to 1080 (proxy/proxy.go), and
	// a bare Contains(":1") would still match ":1080" — so dropping the port
	// would survive as a mutation. The "->" suffix pins the whole address.
	const proxyAddr = "127.0.0.1:9"

	for _, scheme := range []string{"socks", "socks5"} {
		t.Run(scheme, func(t *testing.T) {
			transport := &http.Transport{Proxy: sentinelProxy}

			if err := Apply(transport, scheme+"://"+proxyAddr); err != nil {
				t.Fatalf("Apply(%s) error = %v, want nil", scheme, err)
			}
			if transport.Proxy != nil {
				t.Fatal("Proxy is still set; socks must go through DialContext or the upstream gets an HTTP CONNECT")
			}
			if transport.DialContext == nil {
				t.Fatal("DialContext = nil, want the socks dialer to be installed")
			}

			_, dialErr := transport.DialContext(context.Background(), "tcp", "upstream.example.com:443")
			if dialErr == nil {
				t.Fatal("dial succeeded against a closed port, want a connection error")
			}
			if !strings.Contains(dialErr.Error(), proxyAddr+"->") {
				t.Fatalf("dial error = %v, want it to dial the socks proxy %s (traffic went elsewhere)", dialErr, proxyAddr)
			}
		})
	}
}

func TestApply_RejectsUnsupportedSchemes(t *testing.T) {
	// The empty string lands here on purpose: callers that mean "connect
	// directly" must short-circuit before Apply rather than rely on it.
	for name, raw := range map[string]string{
		"ftp":    "ftp://proxy.example.com",
		"socks4": "socks4://proxy.example.com:1080",
		"empty":  "",
	} {
		t.Run(name, func(t *testing.T) {
			transport := &http.Transport{}
			err := Apply(transport, raw)
			if err == nil {
				t.Fatalf("Apply(%q) error = nil, want unsupported scheme", raw)
			}
			if !strings.Contains(err.Error(), "unsupported proxy scheme") {
				t.Fatalf("Apply(%q) error = %v, want it to name the unsupported scheme", raw, err)
			}
			if transport.Proxy != nil || transport.DialContext != nil {
				t.Fatal("transport was mutated on a rejected scheme")
			}
		})
	}
}

func TestApply_RejectsUnparseableURL(t *testing.T) {
	transport := &http.Transport{}
	// A raw control character is rejected by url.Parse itself, which is the
	// only way to reach the parse branch.
	err := Apply(transport, "http://proxy.example.com/\x7f")
	if err == nil {
		t.Fatal("Apply() error = nil, want a parse failure")
	}
	if !strings.Contains(err.Error(), "invalid proxy url") {
		t.Fatalf("Apply() error = %v, want it to report an invalid proxy url", err)
	}
	if transport.Proxy != nil || transport.DialContext != nil {
		t.Fatal("transport was mutated on an unparseable URL")
	}
}
