package xurl

import (
	"net"
	"testing"
)

func TestIsDisallowedIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{"loopback v4", "127.0.0.1", true},
		{"loopback v4 high", "127.255.255.255", true},
		{"loopback v6", "::1", true},
		{"private 10/8", "10.0.0.1", true},
		{"private 172.16/12", "172.16.0.1", true},
		{"private 192.168/16", "192.168.1.1", true},
		{"link-local metadata 169.254.169.254", "169.254.169.254", true},
		{"link-local 169.254 other", "169.254.0.1", true},
		{"link-local v6 fe80", "fe80::1", true},
		{"unspecified v4", "0.0.0.0", true},
		{"unspecified v6", "::", true},
		{"multicast v4", "224.0.0.1", true},
		{"multicast v6 ff02", "ff02::1", true},
		{"public 8.8.8.8", "8.8.8.8", false},
		{"public 1.1.1.1", "1.1.1.1", false},
		{"public v6", "2606:4700:4700::1111", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("invalid test ip %q", tt.ip)
			}
			if got := IsDisallowedIP(ip); got != tt.want {
				t.Errorf("IsDisallowedIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestIsDisallowedIP_Nil(t *testing.T) {
	if !IsDisallowedIP(nil) {
		t.Error("IsDisallowedIP(nil) = false, want true")
	}
}

func TestAssertSafeURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"empty", "", true},
		{"file scheme", "file:///etc/passwd", true},
		{"ftp scheme", "ftp://example.com/", true},
		{"gopher scheme", "gopher://127.0.0.1/", true},
		{"no scheme", "example.com/", true},
		{"loopback ip", "http://127.0.0.1/", true},
		{"metadata ip", "http://169.254.169.254/latest/meta-data/", true},
		{"private ip", "http://10.0.0.1/", true},
		{"localhost", "http://localhost/", true},
		{"dotlocal", "http://foo.local/", true},
		// Literal public IPs are validated without DNS, so this stays stable.
		{"public ip", "http://8.8.8.8/", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AssertSafeURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("AssertSafeURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestAssertSafeHost_Nil(t *testing.T) {
	if err := AssertSafeHost(nil); err == nil {
		t.Error("AssertSafeHost(nil) = nil, want error")
	}
}
