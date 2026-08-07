package backup

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// S-5: base_url 来自设置页（settings:write，editor 角色也持有），是用户可控的
// 出站目标。校验放在构造函数里，因此这张表同时锁住 scheduler.go 的四条路径。
func TestNewWebDAVClientRejectsUnsafeBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
	}{
		{"empty", ""},
		{"no scheme", "dav.example.com/backup"},
		{"file scheme", "file:///etc/passwd"},
		{"loopback", "http://127.0.0.1:8080/dav"},
		{"loopback name", "http://localhost/dav"},
		{"dotlocal", "http://nas.local/dav"},
		{"private 10/8", "http://10.0.0.5/dav"},
		{"private 192.168/16", "http://192.168.1.10/dav"},
		{"cloud metadata", "http://169.254.169.254/latest/meta-data/"},
		{"unspecified", "http://0.0.0.0/dav"},
		// 尾斜杠先被裁掉再校验，裁剪不能成为绕过点。
		{"loopback trailing slash", "http://127.0.0.1/"},
		{"loopback with spaces", "  http://127.0.0.1/dav  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewWebDAVClient(tt.baseURL, "u", "p")
			if err == nil {
				t.Fatalf("NewWebDAVClient(%q) error = nil, want rejection", tt.baseURL)
			}
			if client != nil {
				t.Fatalf("NewWebDAVClient(%q) returned a usable client alongside the error", tt.baseURL)
			}
		})
	}
}

func TestNewWebDAVClientAcceptsPublicBaseURL(t *testing.T) {
	// 字面公网 IP 不走 DNS，测试因此不依赖网络。
	client, err := NewWebDAVClient("https://8.8.8.8/dav/", "u", "p")
	if err != nil {
		t.Fatalf("NewWebDAVClient() error = %v, want nil", err)
	}
	if client == nil {
		t.Fatal("NewWebDAVClient() returned nil client without an error")
	}
	if client.baseURL != "https://8.8.8.8/dav" {
		t.Fatalf("baseURL = %q, want %q (trailing slash trimmed)", client.baseURL, "https://8.8.8.8/dav")
	}
}

// AssertSafeURL 只覆盖第一跳。若不拦重定向，一个公网主机可以用
// 302 → 169.254.169.254 把带着 Basic-Auth 的请求引到云元数据端点。
func TestWebDAVClientRefusesRedirectToInternalHost(t *testing.T) {
	var internalHits int
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		internalHits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("internal-secret"))
	}))
	defer internal.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL+"/latest/meta-data/", http.StatusFound)
	}))
	defer redirector.Close()

	// httptest 只监听 127.0.0.1，构造函数会拒绝它；直接构造以隔离出重定向守卫。
	client := newTestWebDAVClient(t, redirector.URL)

	data, err := client.Download("/backup.json")
	if err == nil {
		t.Fatalf("Download() error = nil, want redirect rejection; body=%q", string(data))
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("Download() error = %v, want a redirect-not-allowed error", err)
	}
	if internalHits != 0 {
		t.Fatalf("internal host received %d request(s); the redirect guard let the request through", internalHits)
	}
}

// 反向锁：重定向守卫不得把所有重定向一律拒掉，公网目标必须仍然跟随，
// 否则「拦内网」会退化成「WebDAV 全不可用」。
func TestWebDAVClientFollowsRedirectToAllowedHost(t *testing.T) {
	const payload = `{"version":1}`

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer target.Close()

	targetPort := portOf(t, target.URL)
	// 把 127.0.0.1 换成一个公网字面地址，再把该地址的拨号重定向回本地监听端口：
	// 校验看到的是公网 IP，实际连接仍留在测试进程内。
	publicTarget := fmt.Sprintf("http://8.8.8.8:%s/redirected.json", targetPort)

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, publicTarget, http.StatusFound)
	}))
	defer redirector.Close()

	client := newTestWebDAVClient(t, redirector.URL)
	client.client.Transport = &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if strings.HasPrefix(addr, "8.8.8.8:") {
				addr = "127.0.0.1:" + targetPort
			}
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}

	data, err := client.Download("/backup.json")
	if err != nil {
		t.Fatalf("Download() error = %v, want the redirect to a public host to be followed", err)
	}
	if string(data) != payload {
		t.Fatalf("Download() = %q, want %q", string(data), payload)
	}
}

// newTestWebDAVClient builds a client against a loopback test server, bypassing
// the constructor's base-URL check so that redirect behaviour can be tested in
// isolation. It copies the constructor's CheckRedirect so the guard under test
// is the real one.
func newTestWebDAVClient(t *testing.T, baseURL string) *WebDAVClient {
	t.Helper()
	real, err := NewWebDAVClient("https://8.8.8.8/dav", "u", "p")
	if err != nil {
		t.Fatalf("build reference client: %v", err)
	}
	real.baseURL = strings.TrimSuffix(baseURL, "/")
	return real
}

func portOf(t *testing.T, rawURL string) string {
	t.Helper()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(rawURL, "http://"))
	if err != nil {
		t.Fatalf("split host port of %q: %v", rawURL, err)
	}
	return port
}
