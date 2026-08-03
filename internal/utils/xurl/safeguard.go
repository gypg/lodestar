package xurl

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// IsDisallowedIP reports whether ip points at a host that server-side requests
// must never reach: loopback, private (RFC 1918 / ULA), link-local, multicast,
// unspecified, or the link-local metadata range (169.254.0.0/16) used by cloud
// instance metadata services such as AWS IMDS. It is the core of the SSRF
// mitigation applied to user-controlled outbound URLs.
func IsDisallowedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		// 169.254.0.0/16 — link-local, also covers cloud metadata endpoints.
		return v4[0] == 169 && v4[1] == 254
	}
	return ip.IsPrivate()
}

// AssertSafeHost validates that target.Host is safe for a server-side request.
// It rejects empty hosts, "localhost" and ".local" names, any literal IP that
// is disallowed (see IsDisallowedIP), and any hostname whose DNS records
// resolve to a disallowed address. Resolving up front mitigates — but does not
// fully prevent — DNS rebinding.
func AssertSafeHost(target *url.URL) error {
	if target == nil {
		return fmt.Errorf("url is required")
	}
	host := strings.TrimSpace(target.Hostname())
	if host == "" {
		host = strings.TrimSpace(target.Host)
	}
	host = strings.Trim(strings.ToLower(host), ".")
	if host == "" {
		return fmt.Errorf("url must have a host")
	}
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return fmt.Errorf("url host is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if IsDisallowedIP(ip) {
			return fmt.Errorf("url host is not allowed")
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve url host: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("url host did not resolve")
	}
	for _, ip := range ips {
		if IsDisallowedIP(ip) {
			return fmt.Errorf("url host resolves to a disallowed address")
		}
	}
	return nil
}

// AssertSafeURL parses rawURL, requires an http(s) scheme with a host, and
// validates the host via AssertSafeHost. Use it at request boundaries to guard
// server-side fetches driven by user input.
func AssertSafeURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("url must be a valid http or https url")
	}
	return AssertSafeHost(parsed)
}
