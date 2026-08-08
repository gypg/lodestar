package relay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	dbmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	transmodel "github.com/gypg/lodestar/internal/transformer/model"
)

// sseFrameSuffix 是 SSE 帧的结束分隔符（空行）。单独提出来是为了让直通 adapter
// 的拼接一眼可读。
const sseFrameSuffix = "\n\n"

// newEmptyStreamAttempt 搭一个最小的流式尝试：SSE body 由调用方给出，出/入站
// adapter 用直通实现，所以 firstToken 只取决于上游是否给了有效数据帧。
func newEmptyStreamAttempt(t *testing.T, session *relayStreamSession) (*relayAttempt, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	clientCtx := context.Background()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(clientCtx)

	stream := true
	ra := &relayAttempt{
		relayRequest: &relayRequest{
			c:               c,
			clientCtx:       clientCtx,
			operationCtx:    context.Background(),
			inAdapter:       passthroughInbound{},
			internalRequest: &transmodel.InternalLLMRequest{Stream: &stream},
			streamSession:   session,
			// 非空流会走到 metrics.SetFirstTokenTime，缺了就是 nil deref。
			metrics: NewRelayMetrics(1, "test-model", "", "", "127.0.0.1", nil),
		},
		outAdapter: passthroughOutbound{},
		channel:    &dbmodel.Channel{Name: "test-channel"},
	}
	return ra, recorder
}

func runEmptyStreamAttempt(t *testing.T, ra *relayAttempt, body string) error {
	t.Helper()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	done := make(chan error, 1)
	go func() { done <- ra.handleStreamResponse(context.Background(), resp) }()

	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("handleStreamResponse() did not return")
		return nil
	}
}

// 上游以 EOF 正常结束却一个数据帧都没给：必须返回 errEmptyOutput 触发换 Key 重试。
// 修复前这段判断在函数末尾的不可达代码里，流式路径永远返回 nil。
func TestHandleStreamResponseEmptyStreamTriggersRetry(t *testing.T) {
	ra, recorder := newEmptyStreamAttempt(t, nil)

	err := runEmptyStreamAttempt(t, ra, "")
	if !errors.Is(err, errEmptyOutput) {
		t.Fatalf("handleStreamResponse() err = %v, want errEmptyOutput", err)
	}
	if body := recorder.Body.String(); body != "" {
		t.Fatalf("empty stream wrote %q to client; retry is only safe when nothing was written", body)
	}
}

// 只有注释帧、没有任何有效 payload 时同样算空流。
func TestHandleStreamResponseCommentOnlyStreamTriggersRetry(t *testing.T) {
	ra, _ := newEmptyStreamAttempt(t, nil)

	if err := runEmptyStreamAttempt(t, ra, ": ping\n\n: ping\n\n"); !errors.Is(err, errEmptyOutput) {
		t.Fatalf("handleStreamResponse() err = %v, want errEmptyOutput", err)
	}
}

// 有数据帧就不是空流：必须返回 nil，且数据要写给客户端。
// 这一条守住"把空流判断写宽了"的反向变异。
func TestHandleStreamResponseNonEmptyStreamDoesNotRetry(t *testing.T) {
	ra, recorder := newEmptyStreamAttempt(t, nil)

	if err := runEmptyStreamAttempt(t, ra, "data: hello\n\n"); err != nil {
		t.Fatalf("handleStreamResponse() err = %v, want nil", err)
	}
	if got := recorder.Body.String(); !strings.Contains(got, "hello") {
		t.Fatalf("client body = %q, want it to contain the streamed chunk", got)
	}
}

// 关掉 retry_empty_output 时空流按正常结束处理，不得劫持成重试。
func TestHandleStreamResponseEmptyStreamRespectsDisabledSetting(t *testing.T) {
	seedRetryEmptyOutputSetting(t, "false")

	ra, _ := newEmptyStreamAttempt(t, nil)
	if err := runEmptyStreamAttempt(t, ra, ""); err != nil {
		t.Fatalf("handleStreamResponse() err = %v, want nil when retry_empty_output is off", err)
	}
}

// 空流重试必须保留 session 给下一次尝试复用：提前 Finish 会让重试成功的数据
// 落进一个已 done 的会话，重连的客户端只能看到失败终态。
func TestHandleStreamResponseEmptyStreamKeepsSessionOpenForRetry(t *testing.T) {
	resetTestStreamSessionStore()

	session, created, err := acquireRelayStreamSession("conv-empty-stream", 1, 42)
	if err != nil || !created || session == nil {
		t.Fatalf("acquireRelayStreamSession() = (%v, %t, %v)", session, created, err)
	}

	ra, _ := newEmptyStreamAttempt(t, session)
	if err := runEmptyStreamAttempt(t, ra, ""); !errors.Is(err, errEmptyOutput) {
		t.Fatalf("handleStreamResponse() err = %v, want errEmptyOutput", err)
	}
	if session.IsDone() {
		t.Fatal("session was finished on empty-stream retry; the next attempt has nowhere to deliver events")
	}
}

// 反例：非空流正常结束时 session 必须被置为终态，否则 Handler 的 defer 会补一个
// "relay stream ended without a terminal result"。这守住上一条豁免被写得过宽。
func TestHandleStreamResponseFinishesSessionOnNormalEnd(t *testing.T) {
	resetTestStreamSessionStore()

	session, created, err := acquireRelayStreamSession("conv-normal-end", 1, 43)
	if err != nil || !created || session == nil {
		t.Fatalf("acquireRelayStreamSession() = (%v, %t, %v)", session, created, err)
	}

	ra, _ := newEmptyStreamAttempt(t, session)
	if err := runEmptyStreamAttempt(t, ra, "data: hello\n\n"); err != nil {
		t.Fatalf("handleStreamResponse() err = %v, want nil", err)
	}
	if !session.IsDone() {
		t.Fatal("session was left open after a normal stream end")
	}
}

// ctx 被取消/超时导致的中断，即便一个 token 都没收到，也不是"上游返回空流"。
// 判断顺序必须是先看 ctx.Err() 再看空流：反过来会把超时和客户端取消误判成
// 可重试的空流，白白把重试预算烧在其它 Key 上。
func TestHandleStreamResponseInterruptedContextIsNotEmptyStream(t *testing.T) {
	ra, _ := newEmptyStreamAttempt(t, nil)

	// io.Pipe 永不返回 EOF，所以 results 只可能因 ctx 取消而关闭。
	reader, writer := io.Pipe()
	defer writer.Close()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       reader,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ra.handleStreamResponse(ctx, resp) }()

	// 先取消，再喂一个完整事件。SSE reader 只在 sse.Read 产出事件后才轮询
	// ctx.Done()，所以必须让它醒过来一次才会走中断退出；因为 ctx 已经取消，
	// 这个事件不会被投递给主循环，firstToken 保持 true —— 正是"零 token 中断"。
	cancel()
	go func() { _, _ = writer.Write([]byte("data: {}\n\n")) }()

	select {
	case err := <-done:
		if errors.Is(err, errEmptyOutput) {
			t.Fatal("interrupted stream reported as empty output; a cancelled/timed-out request would burn retries on other keys")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("handleStreamResponse() err = %v, want it to wrap context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handleStreamResponse() did not return after context cancellation")
	}
}

// errEmptyOutput 必须落到"可重试"分支，而不是 ScopeAbortAll。
func TestEmptyOutputDecisionIsRetryable(t *testing.T) {
	if got := ClassifyRelayError(http.StatusOK, errEmptyOutput, false); got.Scope == ScopeAbortAll {
		t.Fatalf("ClassifyRelayError(errEmptyOutput) Scope = %v, want a retryable scope", got.Scope)
	}
}

// passthroughOutbound / passthroughInbound 是最小的直通 adapter：把 SSE 的 data
// 原样搬运，让测试只聚焦"上游有没有给出有效数据帧"这一个变量。
type passthroughOutbound struct{}

func (passthroughOutbound) TransformRequest(context.Context, *transmodel.InternalLLMRequest, string, string) (*http.Request, error) {
	return nil, errors.New("not used in this test")
}

func (passthroughOutbound) TransformResponse(context.Context, *http.Response) (*transmodel.InternalLLMResponse, error) {
	return nil, errors.New("not used in this test")
}

func (passthroughOutbound) TransformStream(_ context.Context, eventData []byte) (*transmodel.InternalLLMResponse, error) {
	if len(eventData) == 0 {
		return nil, nil
	}
	return &transmodel.InternalLLMResponse{Object: string(eventData)}, nil
}

type passthroughInbound struct{}

func (passthroughInbound) TransformRequest(context.Context, []byte) (*transmodel.InternalLLMRequest, error) {
	return nil, errors.New("not used in this test")
}

func (passthroughInbound) TransformResponse(context.Context, *transmodel.InternalLLMResponse) ([]byte, error) {
	return nil, errors.New("not used in this test")
}

func (passthroughInbound) TransformStream(_ context.Context, stream *transmodel.InternalLLMResponse) ([]byte, error) {
	if stream == nil || stream.Object == "" {
		return nil, nil
	}
	return []byte("data: " + stream.Object + sseFrameSuffix), nil
}

func (passthroughInbound) GetInternalResponse(context.Context) (*transmodel.InternalLLMResponse, error) {
	return nil, errors.New("not used in this test")
}

// seedRetryEmptyOutputSetting 直接改设置缓存：retry_empty_output 未列在
// DefaultSettings 里，缓存缺键时 isRetryEmptyOutputEnabled 走"默认启用"分支，
// 所以只有需要关掉它的用例才必须显式种值。
func seedRetryEmptyOutputSetting(t *testing.T, value string) {
	t.Helper()
	cache := setting.GetCache()
	previous, had := cache.Get(dbmodel.SettingKeyRetryEmptyOutput)
	cache.Set(dbmodel.SettingKeyRetryEmptyOutput, value)
	t.Cleanup(func() {
		if had {
			cache.Set(dbmodel.SettingKeyRetryEmptyOutput, previous)
			return
		}
		cache.Del(dbmodel.SettingKeyRetryEmptyOutput)
	})
}
