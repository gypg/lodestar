package client

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
)

// resetClientCaches 清掉包级缓存并在测试后还原。这些测试必须住在 package client 内，
// 因为被测对象正是这些未导出的缓存变量与它们的键。
func resetClientCaches(t *testing.T) {
	t.Helper()

	clientLock.Lock()
	prevSystemDirect, prevSystemProxy, prevSystemURL := systemDirectClient, systemProxyClient, systemProxyURL
	prevShortDirect, prevShortProxy, prevShortURL := shortTimeoutDirectClient, shortTimeoutProxyClient, shortTimeoutProxyURL
	systemDirectClient, systemProxyClient, systemProxyURL = nil, nil, ""
	shortTimeoutDirectClient, shortTimeoutProxyClient, shortTimeoutProxyURL = nil, nil, ""
	clientLock.Unlock()

	t.Cleanup(func() {
		clientLock.Lock()
		systemDirectClient, systemProxyClient, systemProxyURL = prevSystemDirect, prevSystemProxy, prevSystemURL
		shortTimeoutDirectClient, shortTimeoutProxyClient, shortTimeoutProxyURL = prevShortDirect, prevShortProxy, prevShortURL
		clientLock.Unlock()
	})
}

func seedProxySetting(t *testing.T, value string) {
	t.Helper()

	cache := setting.GetCache()
	prev, had := cache.Get(model.SettingKeyProxyURL)
	cache.Set(model.SettingKeyProxyURL, value)
	t.Cleanup(func() {
		if had {
			cache.Set(model.SettingKeyProxyURL, prev)
		}
	})
}

// proxyTargetOf 读出 client 实际会把请求发到哪个代理。只对 http/https scheme 有意义
// （socks 装在 DialContext 上，不经 Transport.Proxy）。
func proxyTargetOf(t *testing.T, client *http.Client) string {
	t.Helper()

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy == nil {
		t.Fatal("Transport.Proxy = nil, want a proxy to be configured")
	}
	got, err := transport.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "upstream.example.com"}})
	if err != nil {
		t.Fatalf("Transport.Proxy() error = %v", err)
	}
	if got == nil {
		t.Fatal("Transport.Proxy() returned no URL, want the configured proxy")
	}
	return got.String()
}

// 短超时缓存曾经拿 systemProxyURL 当键，而那个变量只有 GetHTTPClientSystemProxy 会写。
// 于是「长超时路径先感知到代理变更」这个顺序会让短超时路径误认为自己的旧客户端还是新的，
// 后台延迟探测与模型同步就一直用着旧代理直到进程重启。
func TestGetHTTPClientShortTimeout_RebuildsAfterProxyChangeSeenByLongTimeoutPath(t *testing.T) {
	const proxyA = "http://proxy-a.example.com:8080"
	const proxyB = "http://proxy-b.example.com:8080"

	resetClientCaches(t)
	seedProxySetting(t, proxyA)

	shortA, err := GetHTTPClientShortTimeout(true)
	if err != nil {
		t.Fatalf("GetHTTPClientShortTimeout(A) error = %v", err)
	}
	if got := proxyTargetOf(t, shortA); got != proxyA {
		t.Fatalf("short-timeout client points at %q, want %q", got, proxyA)
	}

	// 长超时路径先跑一次，把共享的 systemProxyURL 刷成 A。
	if _, err := GetHTTPClientSystemProxy(true); err != nil {
		t.Fatalf("GetHTTPClientSystemProxy(A) error = %v", err)
	}

	// 管理员改代理，长超时路径先感知到，把 systemProxyURL 刷成 B。
	seedProxySetting(t, proxyB)
	if _, err := GetHTTPClientSystemProxy(true); err != nil {
		t.Fatalf("GetHTTPClientSystemProxy(B) error = %v", err)
	}

	shortB, err := GetHTTPClientShortTimeout(true)
	if err != nil {
		t.Fatalf("GetHTTPClientShortTimeout(B) error = %v", err)
	}
	if got := proxyTargetOf(t, shortB); got != proxyB {
		t.Fatalf("short-timeout client still points at %q after the proxy changed to %q", got, proxyB)
	}
}

// 代理没变时必须复用，否则每次后台探测都新建一个 client，连接池就白建了。
func TestGetHTTPClientShortTimeout_ReusesClientWhileProxyIsUnchanged(t *testing.T) {
	const proxyA = "http://proxy-a.example.com:8080"

	resetClientCaches(t)
	seedProxySetting(t, proxyA)

	first, err := GetHTTPClientShortTimeout(true)
	if err != nil {
		t.Fatalf("GetHTTPClientShortTimeout() first error = %v", err)
	}
	second, err := GetHTTPClientShortTimeout(true)
	if err != nil {
		t.Fatalf("GetHTTPClientShortTimeout() second error = %v", err)
	}
	if first != second {
		t.Fatal("short-timeout client was rebuilt even though the proxy did not change")
	}
}

// 长超时缓存也要有同样的性质，且两个缓存不能互相串（各自独立的客户端实例）。
func TestSystemProxyAndShortTimeoutCachesAreIndependent(t *testing.T) {
	const proxyA = "http://proxy-a.example.com:8080"

	resetClientCaches(t)
	seedProxySetting(t, proxyA)

	long, err := GetHTTPClientSystemProxy(true)
	if err != nil {
		t.Fatalf("GetHTTPClientSystemProxy() error = %v", err)
	}
	short, err := GetHTTPClientShortTimeout(true)
	if err != nil {
		t.Fatalf("GetHTTPClientShortTimeout() error = %v", err)
	}
	if long == short {
		t.Fatal("the long- and short-timeout caches handed out the same client; their timeouts differ")
	}
	if long.Timeout == short.Timeout {
		t.Fatalf("both clients have Timeout = %v, want the short-timeout one to be shorter", long.Timeout)
	}
	if short.Timeout != shortTaskTimeout {
		t.Fatalf("short-timeout client Timeout = %v, want %v", short.Timeout, shortTaskTimeout)
	}
}
