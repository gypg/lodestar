package relay

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newTerminalErrorTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c, w
}

func TestExtractUpstreamErrorDetail(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil error", err: nil, want: ""},
		{
			name: "bare upstream error",
			err:  errors.New(`upstream error: 400: {"error":{"code":"context_length_exceeded"}}`),
			want: `400: {"error":{"code":"context_length_exceeded"}}`,
		},
		{
			// attempt() 会再包一层 "channel X adapter=Y attempt N/M: "，
			// 透传必须仍能剥到上游 body。
			name: "wrapped by attempt()",
			err:  errors.New(`channel foo adapter=chat attempt 1/2: upstream error: 400: prompt is too long`),
			want: `400: prompt is too long`,
		},
		{
			name: "no upstream prefix returned as-is",
			err:  errors.New("failed to create request: invalid payload"),
			want: "failed to create request: invalid payload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractUpstreamErrorDetail(tt.err); got != tt.want {
				t.Errorf("extractUpstreamErrorDetail() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClientErrorType(t *testing.T) {
	tests := []struct {
		statusCode int
		want       string
	}{
		{http.StatusBadRequest, "invalid_request_error"},
		{http.StatusUnauthorized, "authentication_error"},
		{http.StatusForbidden, "authentication_error"},
		{http.StatusTooManyRequests, "rate_limit_error"},
		{http.StatusInternalServerError, "api_error"},
		{http.StatusNotFound, "api_error"},
	}
	for _, tt := range tests {
		if got := clientErrorType(tt.statusCode); got != tt.want {
			t.Errorf("clientErrorType(%d) = %q, want %q", tt.statusCode, got, tt.want)
		}
	}
}

// TestWriteClientTerminalErrorPassesUpstreamJSONVerbatim
// 上游 JSON body 必须原样透传，字节级相同 —— 客户端要靠里面的 code
// （context_length_exceeded）判断该压缩上下文，重新包装会丢掉这个信号。
func TestWriteClientTerminalErrorPassesUpstreamJSONVerbatim(t *testing.T) {
	c, w := newTerminalErrorTestContext()

	upstreamBody := `{"error":{"message":"输入内容过长","type":"invalid_request_error","param":"","code":"context_length_exceeded"}}`
	err := fmt.Errorf("channel demo adapter=chat attempt 1/2: upstream error: 400: %s", upstreamBody)

	writeClientTerminalError(c, http.StatusBadRequest, err)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}
	if got := strings.TrimSpace(w.Body.String()); got != upstreamBody {
		t.Errorf("body not verbatim:\n got = %s\nwant = %s", got, upstreamBody)
	}
}

// TestWriteClientTerminalErrorWrapsPlainTextBody
// 纯文本上游 body（如 "prompt is too long"）要包成 OpenAI 兼容 envelope，
// 但状态码仍用上游的 400，不能升成 502。
func TestWriteClientTerminalErrorWrapsPlainTextBody(t *testing.T) {
	c, w := newTerminalErrorTestContext()

	writeClientTerminalError(c, http.StatusBadRequest, errors.New("upstream error: 400: prompt is too long"))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal body: %v; body=%s", err, w.Body.String())
	}
	if payload.Error.Message != "prompt is too long" {
		t.Errorf("message = %q, want %q", payload.Error.Message, "prompt is too long")
	}
	if payload.Error.Type != "invalid_request_error" {
		t.Errorf("type = %q, want invalid_request_error", payload.Error.Type)
	}
}

// TestWriteClientTerminalErrorPreservesNon400ClientStatus
// 429 也走这条路（媒体链路的 ScopeNone 终态可能带任意状态码）：
// 状态码与错误类型都要跟着上游走。
func TestWriteClientTerminalErrorPreservesNon400ClientStatus(t *testing.T) {
	c, w := newTerminalErrorTestContext()

	writeClientTerminalError(c, http.StatusTooManyRequests, errors.New("upstream error: 429: slow down"))

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "rate_limit_error") {
		t.Errorf("body missing rate_limit_error: %s", w.Body.String())
	}
}

// TestWriteClientTerminalErrorStripsRedundantStatusPrefix
// 错误串里的 "400: " 是我们自己拼的前缀，不该出现在回给客户端的 message 里。
func TestWriteClientTerminalErrorStripsRedundantStatusPrefix(t *testing.T) {
	c, w := newTerminalErrorTestContext()

	writeClientTerminalError(c, http.StatusBadRequest, errors.New("upstream error: 400: bad model"))

	if got := w.Body.String(); strings.Contains(got, `"400:`) || strings.Contains(got, "400: bad model") {
		t.Errorf("status prefix leaked into message: %s", got)
	}
}

// TestWriteClientTerminalErrorFallsBackToBadGatewayWithoutHTTPStatus
// 非 HTTP 错误（连接失败，Code==0）没有上游状态码可透传，回退 502 是对的。
func TestWriteClientTerminalErrorFallsBackToBadGatewayWithoutHTTPStatus(t *testing.T) {
	c, w := newTerminalErrorTestContext()

	writeClientTerminalError(c, 0, errors.New("connection refused"))

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", w.Code, w.Body.String())
	}
}

// TestWriteClientTerminalErrorDropsBodyTooLargeSentinel
// "response body too large" 是 handleForwardResponse 的哨兵串，不是上游说的话；
// 原样回给客户端会冒充成上游的错误消息。
func TestWriteClientTerminalErrorDropsBodyTooLargeSentinel(t *testing.T) {
	c, w := newTerminalErrorTestContext()

	writeClientTerminalError(c, http.StatusBadRequest, errors.New("upstream error: 400: response body too large"))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "response body too large") {
		t.Errorf("internal sentinel leaked to client: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), http.StatusText(http.StatusBadRequest)) {
		t.Errorf("body missing generic status text: %s", w.Body.String())
	}
}

// TestWriteClientTerminalErrorDoesNotOverwriteWrittenResponse
// 流式已写出字节后再写一次会污染 SSE 流。
func TestWriteClientTerminalErrorDoesNotOverwriteWrittenResponse(t *testing.T) {
	c, w := newTerminalErrorTestContext()
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write([]byte("data: partial\n\n"))
	c.Writer.Flush()

	writeClientTerminalError(c, http.StatusBadRequest, errors.New("upstream error: 400: too late"))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (already written)", w.Code)
	}
	if strings.Contains(w.Body.String(), "too late") {
		t.Errorf("wrote error into an already-started response: %s", w.Body.String())
	}
}
