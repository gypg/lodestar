// Package proxydial applies a proxy URL to an *http.Transport.
//
// It exists to keep a single copy of the scheme dispatch: http/https go through
// Transport.Proxy, while socks/socks5 need a SOCKS dialer wired into DialContext
// (Transport.Proxy cannot speak SOCKS). Three call sites used to carry
// byte-identical copies of that switch — internal/client, internal/op/airoute
// and internal/op — which meant a scheme fix had to land three times.
package proxydial

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"golang.org/x/net/proxy"
)

// Apply routes transport's traffic through proxyURLStr.
//
// Supported schemes: http, https, socks, socks5 ("socks" being a SOCKS5 alias).
// Any other scheme — including the empty string — is rejected as unsupported;
// callers that treat an empty proxy URL as "connect directly" must
// short-circuit before calling Apply.
//
// Every failure path returns before touching transport, so a failed Apply never
// leaves a half-configured transport behind.
func Apply(transport *http.Transport, proxyURLStr string) error {
	proxyURL, err := url.Parse(proxyURLStr)
	if err != nil {
		return fmt.Errorf("invalid proxy url: %w", err)
	}

	switch proxyURL.Scheme {
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)
	case "socks", "socks5":
		// x/net/proxy only registers socks5 and socks5h, so handing it the bare
		// "socks" spelling fails with "proxy: unknown scheme: socks" — even
		// though model.NormalizeProxyURL accepts that spelling, so proxy-pool
		// rows can carry it. "socks" is a SOCKS5 alias; canonicalise it on a
		// copy rather than mutating the caller's parsed URL.
		dialURL := *proxyURL
		dialURL.Scheme = "socks5"
		socksDialer, err := proxy.FromURL(&dialURL, proxy.Direct)
		if err != nil {
			return fmt.Errorf("invalid socks proxy: %w", err)
		}
		transport.Proxy = nil
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return socksDialer.Dial(network, addr)
		}
	default:
		return fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
	}

	return nil
}
