package client

import (
	"net"
	"net/http"
	"testing"
	"time"
)

// WO-051 回归：default 档客户端的时限语义。
//
// 历史bug：default 档（relay 转发主链路）把 600s 硬编码为 http.Client.Timeout。
// Client.Timeout 覆盖 connection、重定向**以及整个 response body 的读取**，
// 且计时器在 Do 返回后仍在运行——任何超过 600s 的流式（SSE）响应都会被从中间
// 砍断，客户端收不到 [DONE]，relay 侧记录为普通读取错误并计入熔断。
//
// 修复后的契约：
//   - Client.Timeout == 0（不限总时长，流式 body 随上游持续多久读多久）；
//   - Transport.ResponseHeaderTimeout == defaultResponseHeaderTimeout（只防
//     「上游连响应头都不给」的挂死）；
//   - short 档（后台探测/同步）不变，仍是整体 30s 超时。
func TestDefaultClientsUseHeaderTimeoutNotOverallTimeout(t *testing.T) {
	t.Run("direct", func(t *testing.T) {
		client, err := newHTTPClientNoProxy()
		if err != nil {
			t.Fatalf("newHTTPClientNoProxy: %v", err)
		}
		assertHeaderTimeoutSemantics(t, client)
	})

	t.Run("custom-proxy", func(t *testing.T) {
		// 代理地址不真实没关系：本测试只读取 Transport 配置，不发请求。
		client, err := newHTTPClientCustomProxy("http://127.0.0.1:1")
		if err != nil {
			t.Fatalf("newHTTPClientCustomProxy: %v", err)
		}
		assertHeaderTimeoutSemantics(t, client)
	})

	t.Run("cached-system-direct", func(t *testing.T) {
		resetClientCaches(t)
		seedProxySetting(t, "")

		client, err := GetHTTPClientSystemProxy(false)
		if err != nil {
			t.Fatalf("GetHTTPClientSystemProxy(false): %v", err)
		}
		assertHeaderTimeoutSemantics(t, client)
	})
}

func assertHeaderTimeoutSemantics(t *testing.T, client *http.Client) {
	t.Helper()

	if client.Timeout != 0 {
		t.Errorf("Client.Timeout = %v, want 0 (overall timeout must stay off so long SSE bodies are not cut mid-stream)", client.Timeout)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", client.Transport)
	}
	if transport.ResponseHeaderTimeout != defaultResponseHeaderTimeout {
		t.Errorf("Transport.ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, defaultResponseHeaderTimeout)
	}
}

// short 档必须维持整体超时：后台任务要的就是「到点放弃」，改成 header timeout
// 会让 body 读取无限挂住。
func TestShortTimeoutClientsKeepOverallTimeout(t *testing.T) {
	t.Run("with-timeout-direct", func(t *testing.T) {
		client, err := newHTTPClientNoProxyWithTimeout(shortTaskTimeout)
		if err != nil {
			t.Fatalf("newHTTPClientNoProxyWithTimeout: %v", err)
		}
		if client.Timeout != shortTaskTimeout {
			t.Errorf("Client.Timeout = %v, want %v", client.Timeout, shortTaskTimeout)
		}
	})

	t.Run("with-timeout-custom-proxy", func(t *testing.T) {
		client, err := GetHTTPClientCustomProxyWithTimeout("http://127.0.0.1:1", shortTaskTimeout)
		if err != nil {
			t.Fatalf("GetHTTPClientCustomProxyWithTimeout: %v", err)
		}
		if client.Timeout != shortTaskTimeout {
			t.Errorf("Client.Timeout = %v, want %v", client.Timeout, shortTaskTimeout)
		}
	})
}

// 流式语义的行为级验证：等响应头超过 ResponseHeaderTimeout 会被掐断，
// 而拿到了响应头之后的慢速 body 不会被任何总时长上限切断。
func TestHeaderTimeoutCutsStalledHeadersNotSlowBodies(t *testing.T) {
	// 用一个小到测试可快速触发的等头超时。不改生产常量，走 WithTimeout 变体
	// 不行（那是 Client.Timeout），因此这里直接构造 transport 复刻同款语义。
	client, err := newHTTPClientNoProxy()
	if err != nil {
		t.Fatalf("newHTTPClientNoProxy: %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T", client.Transport)
	}
	transport.ResponseHeaderTimeout = 50 * time.Millisecond

	// 慢速 body 服务器：立即发响应头，然后慢慢滴 body（总时长远超 50ms）。
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for i := 0; i < 6; i++ {
			_, _ = w.Write([]byte("chunk"))
			w.(http.Flusher).Flush()
			time.Sleep(30 * time.Millisecond)
		}
	})
	server := &http.Server{Addr: "127.0.0.1:0", Handler: handler}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = server.Close() })

	resp, err := client.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("GET (headers arrived immediately, body trickles): %v", err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 512)
	total := 0
	for {
		n, readErr := resp.Body.Read(buf)
		total += n
		if readErr != nil {
			t.Fatalf("body read failed after %d bytes (slow trickle must not be cut by any overall deadline): %v", total, readErr)
		}
		if total == 6*len("chunk") {
			break
		}
	}
}
