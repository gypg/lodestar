package proxydial

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// socksRecorder 是一个只支持「无认证 + CONNECT」的最小 SOCKS5 服务端。
// 它存在的意义是让测试能断言「流量真的穿过了代理」——
// 光断 DialContext 非 nil、或断连接失败时的地址，都证明不了握手真的走通。
type socksRecorder struct {
	addr string

	mu      sync.Mutex
	targets []string
}

func (r *socksRecorder) observed() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.targets...)
}

func (r *socksRecorder) record(target string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.targets = append(r.targets, target)
}

func startSocks5Recorder(t *testing.T) *socksRecorder {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for socks recorder: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	rec := &socksRecorder{addr: listener.Addr().String()}

	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return // listener 已关闭
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				if err := rec.serve(conn); err != nil && !errors.Is(err, io.EOF) {
					t.Logf("socks recorder connection ended: %v", err)
				}
			}()
		}
	}()

	return rec
}

// serve 走一遍 RFC 1928 的无认证 CONNECT 流程，然后双向对拷。
func (r *socksRecorder) serve(client net.Conn) error {
	_ = client.SetDeadline(time.Now().Add(10 * time.Second))

	// 1) 版本 + 认证方法协商
	header := make([]byte, 2)
	if _, err := io.ReadFull(client, header); err != nil {
		return fmt.Errorf("read greeting: %w", err)
	}
	if header[0] != 0x05 {
		return fmt.Errorf("socks version = %d, want 5", header[0])
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(client, methods); err != nil {
		return fmt.Errorf("read auth methods: %w", err)
	}
	// 0x00 = 无需认证
	if _, err := client.Write([]byte{0x05, 0x00}); err != nil {
		return fmt.Errorf("write method choice: %w", err)
	}

	// 2) 请求
	request := make([]byte, 4)
	if _, err := io.ReadFull(client, request); err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	if request[1] != 0x01 {
		return fmt.Errorf("socks command = %d, want CONNECT(1)", request[1])
	}

	var host string
	switch request[3] {
	case 0x01: // IPv4
		buf := make([]byte, 4)
		if _, err := io.ReadFull(client, buf); err != nil {
			return fmt.Errorf("read ipv4: %w", err)
		}
		host = net.IP(buf).String()
	case 0x03: // 域名
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(client, lenBuf); err != nil {
			return fmt.Errorf("read domain length: %w", err)
		}
		buf := make([]byte, int(lenBuf[0]))
		if _, err := io.ReadFull(client, buf); err != nil {
			return fmt.Errorf("read domain: %w", err)
		}
		host = string(buf)
	case 0x04: // IPv6
		buf := make([]byte, 16)
		if _, err := io.ReadFull(client, buf); err != nil {
			return fmt.Errorf("read ipv6: %w", err)
		}
		host = net.IP(buf).String()
	default:
		return fmt.Errorf("unsupported address type %d", request[3])
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(client, portBuf); err != nil {
		return fmt.Errorf("read port: %w", err)
	}
	port := int(portBuf[0])<<8 | int(portBuf[1])
	target := net.JoinHostPort(host, fmt.Sprint(port))
	r.record(target)

	upstream, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		// 0x05 = connection refused
		_, _ = client.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return fmt.Errorf("dial target %s: %w", target, err)
	}
	defer upstream.Close()
	_ = upstream.SetDeadline(time.Now().Add(10 * time.Second))

	// 3) 成功应答（BND.ADDR/BND.PORT 填零，客户端不校验）
	if _, err := client.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return fmt.Errorf("write reply: %w", err)
	}

	// 4) 对拷。任一方向结束就关掉两端，否则 keep-alive 的连接会一直挂到 deadline
	//    才收场，测试就变成「靠超时兜出来的绿」（每个子测试白等 10 秒）。
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(upstream, client)
		_ = upstream.Close()
	}()
	_, _ = io.Copy(client, upstream)
	_ = client.Close()
	<-done
	return nil
}

// 这是唯一一条真正走完 SOCKS5 握手的测试：请求必须**穿过**代理抵达 origin。
//
// 为什么需要它：其余 socks 用例断的都是「连不上时错误里出现代理地址」，那能证明拨号目标对了，
// 但证明不了握手协议真的走通。而「返回 200」单独也不够 —— origin 是本地的，
// 就算 DialContext 根本没装上、直连过去也会 200。所以判据是**代理是否观察到那次 CONNECT**。
func TestApply_SocksTrafficActuallyTraversesTheProxy(t *testing.T) {
	for _, scheme := range []string{"socks", "socks5"} {
		t.Run(scheme, func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("through-the-proxy"))
			}))
			t.Cleanup(origin.Close)

			recorder := startSocks5Recorder(t)

			transport := &http.Transport{Proxy: sentinelProxy}
			if err := Apply(transport, scheme+"://"+recorder.addr); err != nil {
				t.Fatalf("Apply(%s) error = %v, want nil", scheme, err)
			}

			client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
			resp, err := client.Get(origin.URL)
			if err != nil {
				t.Fatalf("GET through socks proxy error = %v", err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			// 主动收掉 keep-alive 连接，让代理侧的对拷 goroutine 立刻结束，
			// 而不是等 deadline。少了这一句，整个测试会白等 10 秒才通过。
			transport.CloseIdleConnections()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			if got := string(body); got != "through-the-proxy" {
				t.Fatalf("body = %q, want %q", got, "through-the-proxy")
			}

			// 决定性断言：代理必须看到这次 CONNECT。若 DialContext 没装上，
			// 请求会直连 origin，上面三条断言照样全过，只有这条会红。
			wantTarget := strings.TrimPrefix(origin.URL, "http://")
			observed := recorder.observed()
			if len(observed) == 0 {
				t.Fatal("the socks proxy saw no CONNECT; traffic bypassed it and went straight to the origin")
			}
			found := false
			for _, target := range observed {
				if target == wantTarget {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("socks proxy observed %v, want a CONNECT to %s", observed, wantTarget)
			}
		})
	}
}
