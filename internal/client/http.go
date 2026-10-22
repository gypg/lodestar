package client

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/utils/proxydial"
)

const (
	// shortTaskTimeout 后台任务使用的短超时时间（延迟探测、模型同步）
	shortTaskTimeout = 30 * time.Second

	// defaultResponseHeaderTimeout 是 default 档（relay 转发/长任务）等待响应头的
	// 超时（WO-051，对齐上游 e8c6a0ef）。
	//
	// 历史bug：这一档曾是 http.Client.Timeout = 600s。Client.Timeout 的语义覆盖
	// 整个 response body 的读取过程（stdlib net/http 明文 "will interrupt reading
	// of the Response.Body"），计时器在 Do 返回后仍在运行——于是超过 10 分钟的流式
	// SSE 响应被从流中间砍断：客户端收不到 [DONE]，relay 侧看到普通读取错误而非
	// 断连哨兵，还会计入熔断。深度 reasoning 模型的长输出完全可能超 10 分钟。
	//
	// 现改为 Client.Timeout = 0（不限）+ Transport.ResponseHeaderTimeout：后者只
	// 覆盖「等响应头」阶段，不影响流式 body 读取，同时仍能防住「上游连响应头都
	// 不给」的挂死。
	defaultResponseHeaderTimeout = 600 * time.Second
)

var (
	systemDirectClient *http.Client
	systemProxyClient  *http.Client
	systemProxyURL     string
	// shortTimeoutProxyURL is the short-timeout cache's own key. It must not share
	// systemProxyURL: that variable is only ever written by
	// GetHTTPClientSystemProxy, so keying this cache off it meant a proxy change
	// picked up by the long-timeout path made the short-timeout path believe its
	// stale client was current, and background probes kept using the old proxy
	// until restart.
	shortTimeoutProxyURL     string
	shortTimeoutDirectClient *http.Client
	shortTimeoutProxyClient  *http.Client
	clientLock               sync.RWMutex
)

// GetHTTPClientSystemProxy returns a cached http.Client.
// - useProxy=false: bypass proxy
// - useProxy=true: use proxy settings from system/app settings (setting key: proxy_url)
func GetHTTPClientSystemProxy(useProxy bool) (*http.Client, error) {
	if useProxy {
		currentProxyURL, err := setting.GetString(model.SettingKeyProxyURL)
		if err != nil {
			return nil, err
		}
		if currentProxyURL == "" {
			return nil, fmt.Errorf("proxy url is empty")
		}

		clientLock.RLock()
		if systemProxyClient != nil && systemProxyURL == currentProxyURL {
			clientLock.RUnlock()
			return systemProxyClient, nil
		}
		clientLock.RUnlock()

		clientLock.Lock()
		defer clientLock.Unlock()

		// Re-check after acquiring write lock.
		if systemProxyClient != nil && systemProxyURL == currentProxyURL {
			return systemProxyClient, nil
		}

		client, err := newHTTPClientCustomProxy(currentProxyURL)
		if err != nil {
			return nil, err
		}
		systemProxyClient = client
		systemProxyURL = currentProxyURL
		return systemProxyClient, nil
	}

	clientLock.RLock()
	if !useProxy && systemDirectClient != nil {
		clientLock.RUnlock()
		return systemDirectClient, nil
	}
	clientLock.RUnlock()

	clientLock.Lock()
	defer clientLock.Unlock()

	if systemDirectClient != nil {
		return systemDirectClient, nil
	}
	client, err := newHTTPClientNoProxy()
	if err != nil {
		return nil, err
	}
	systemDirectClient = client
	return systemDirectClient, nil
}

// GetHTTPClientCustomProxy returns a NEW http.Client every time (no reuse).
// proxyURL supports: http, https, socks, socks5
func GetHTTPClientCustomProxy(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return nil, fmt.Errorf("proxy url is empty")
	}
	return newHTTPClientCustomProxy(proxyURL)
}

// GetHTTPClientShortTimeout returns a cached http.Client with short timeout (30s).
// Used for background tasks like delay probing and model syncing to avoid
// goroutine/connection accumulation when endpoints are unreachable.
// - useProxy=false: bypass proxy
// - useProxy=true: use proxy settings from system/app settings
func GetHTTPClientShortTimeout(useProxy bool) (*http.Client, error) {
	if useProxy {
		currentProxyURL, err := setting.GetString(model.SettingKeyProxyURL)
		if err != nil {
			return nil, err
		}
		if currentProxyURL == "" {
			return nil, fmt.Errorf("proxy url is empty")
		}

		clientLock.RLock()
		if shortTimeoutProxyClient != nil && shortTimeoutProxyURL == currentProxyURL {
			clientLock.RUnlock()
			return shortTimeoutProxyClient, nil
		}
		clientLock.RUnlock()

		clientLock.Lock()
		defer clientLock.Unlock()

		if shortTimeoutProxyClient != nil && shortTimeoutProxyURL == currentProxyURL {
			return shortTimeoutProxyClient, nil
		}

		client, err := newHTTPClientCustomProxyWithTimeout(currentProxyURL, shortTaskTimeout)
		if err != nil {
			return nil, err
		}
		shortTimeoutProxyClient = client
		shortTimeoutProxyURL = currentProxyURL
		return shortTimeoutProxyClient, nil
	}

	clientLock.RLock()
	if shortTimeoutDirectClient != nil {
		clientLock.RUnlock()
		return shortTimeoutDirectClient, nil
	}
	clientLock.RUnlock()

	clientLock.Lock()
	defer clientLock.Unlock()

	if shortTimeoutDirectClient != nil {
		return shortTimeoutDirectClient, nil
	}
	client, err := newHTTPClientNoProxyWithTimeout(shortTaskTimeout)
	if err != nil {
		return nil, err
	}
	shortTimeoutDirectClient = client
	return shortTimeoutDirectClient, nil
}

// GetHTTPClientCustomProxyWithTimeout returns a NEW http.Client with custom timeout.
// proxyURL supports: http, https, socks, socks5
func GetHTTPClientCustomProxyWithTimeout(proxyURL string, timeout time.Duration) (*http.Client, error) {
	if proxyURL == "" {
		return nil, fmt.Errorf("proxy url is empty")
	}
	return newHTTPClientCustomProxyWithTimeout(proxyURL, timeout)
}

func clonedDefaultTransport() (*http.Transport, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default transport is not *http.Transport")
	}
	return transport.Clone(), nil
}

func newHTTPClientNoProxy() (*http.Client, error) {
	// default 档：不限总时长（流式 body 可能远超 600s），只限等响应头。
	// 见 defaultResponseHeaderTimeout 的注释。
	cloned, err := clonedDefaultTransport()
	if err != nil {
		return nil, err
	}
	cloned.Proxy = nil
	cloned.ResponseHeaderTimeout = defaultResponseHeaderTimeout
	return &http.Client{Transport: cloned}, nil
}

func newHTTPClientNoProxyWithTimeout(timeout time.Duration) (*http.Client, error) {
	cloned, err := clonedDefaultTransport()
	if err != nil {
		return nil, err
	}
	cloned.Proxy = nil
	return &http.Client{Transport: cloned, Timeout: timeout}, nil
}

func newHTTPClientCustomProxy(proxyURLStr string) (*http.Client, error) {
	// default 档：同 newHTTPClientNoProxy，不限总时长，只限等响应头。
	cloned, err := clonedDefaultTransport()
	if err != nil {
		return nil, err
	}
	cloned.ResponseHeaderTimeout = defaultResponseHeaderTimeout

	if err := proxydial.Apply(cloned, proxyURLStr); err != nil {
		return nil, err
	}

	return &http.Client{Transport: cloned}, nil
}

func newHTTPClientCustomProxyWithTimeout(proxyURLStr string, timeout time.Duration) (*http.Client, error) {
	cloned, err := clonedDefaultTransport()
	if err != nil {
		return nil, err
	}

	if err := proxydial.Apply(cloned, proxyURLStr); err != nil {
		return nil, err
	}

	return &http.Client{Transport: cloned, Timeout: timeout}, nil
}
