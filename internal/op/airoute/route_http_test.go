package airoute

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// newAIRouteHTTPClient 把「空代理 URL = 直连」这层语义留在自己身上：
// proxydial.Apply 刻意把空串当成不支持的 scheme 拒掉，所以这个早返回一旦丢了，
// 未配置代理的部署会在每次 AI 路由请求上拿到 "unsupported proxy scheme: "。
//
// 注意不要顺手断言 DialContext == nil：这里的 transport 是 http.DefaultTransport
// 的克隆，而 DefaultTransport 自带一个 net.Dialer 的 DialContext。真正的直连判据
// 是 Proxy == nil —— 早返回正是靠显式清空 Proxy 覆盖掉 ProxyFromEnvironment，
// 否则设了 HTTP_PROXY 环境变量的机器会静默走系统代理。
func TestNewAIRouteHTTPClient_EmptyProxyMeansDirect(t *testing.T) {
	client, err := newAIRouteHTTPClient("")
	if err != nil {
		t.Fatalf("newAIRouteHTTPClient(\"\") error = %v, want a direct client", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("Proxy is set; an empty proxy URL must mean a direct connection")
	}
}

func TestNewAIRouteHTTPClient_SocksProxyDialsTheProxyNotTheTarget(t *testing.T) {
	// "socks" 这个拼法能存进代理池（model.NormalizeProxyURL 收它），
	// 而 x/net/proxy 只认 socks5/socks5h —— 这条曾经在拨号时直接失败。
	//
	// 断言必须证明流量去了代理地址：DialContext != nil 在这里是弱断言，
	// DefaultTransport 的克隆本来就带一个。127.0.0.1:1 上没人监听，
	// 所以连接立刻被拒且错误里会带上代理地址，无需 DNS 也无需网络。
	const proxyAddr = "127.0.0.1:1"
	client, err := newAIRouteHTTPClient("socks://" + proxyAddr)
	if err != nil {
		t.Fatalf("newAIRouteHTTPClient(socks) error = %v, want nil", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("Proxy is set; socks must go through DialContext or the proxy gets an HTTP CONNECT")
	}
	if transport.DialContext == nil {
		t.Fatal("DialContext = nil, want the socks dialer to be installed")
	}

	_, dialErr := transport.DialContext(context.Background(), "tcp", "upstream.example.com:443")
	if dialErr == nil {
		t.Fatal("dial succeeded against a closed port, want a connection error")
	}
	if !strings.Contains(dialErr.Error(), proxyAddr) {
		t.Fatalf("dial error = %v, want it to name the socks proxy %s (traffic went to the target instead)", dialErr, proxyAddr)
	}
}

func TestNewAIRouteHTTPClient_RejectsUnsupportedScheme(t *testing.T) {
	if _, err := newAIRouteHTTPClient("ftp://proxy.example.com"); err == nil {
		t.Fatal("newAIRouteHTTPClient(ftp) error = nil, want an unsupported-scheme error")
	}
}
