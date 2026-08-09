package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// stripeWebhook 是**匿名可达**的（无 Auth middleware，Stripe S2S 回调靠签名），
// 原先 io.ReadAll(c.Request.Body) 无上限。测试入口挂在路由上而非直接调
// stripeWebhook 内部，断言落在"上游实际被读走多少字节"这个副作用上——
// 只看状态码的话，把闸门挪到 ReadAll 之后仍会返回 413 而照绿。

func newStripeWebhookTestEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 照生产 init()：public webhook 组，无任何鉴权 middleware。
	engine.POST("/api/v1/webhook/stripe", stripeWebhook)
	return engine
}

func TestStripeWebhookRejectsOversizedBodyAndStopsReading(t *testing.T) {
	engine := newStripeWebhookTestEngine()

	const upload = 8 << 20 // 8 MiB，远超 1 MiB 上限
	body := &countingReader{remaining: upload}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/stripe", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", "t=0,v1=deadbeef")
	req.ContentLength = -1 // chunked，迫使服务端真去读
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	// 副作用断言：闸门必须截断读取，不能把 8MiB 全吞进内存。
	if body.read >= upload {
		t.Fatalf("server read the entire %d-byte upload (%d); limit not enforced", upload, body.read)
	}
	if body.read > maxStripeWebhookBytes*4 {
		t.Fatalf("server read %d bytes, want <= %d", body.read, maxStripeWebhookBytes*4)
	}
}

// 反向守卫：正常大小的事件体必须被读完并进入签名校验，
// 否则把上限误设成 0 或把判断写反都会照绿。
func TestStripeWebhookAcceptsNormalSizedBody(t *testing.T) {
	engine := newStripeWebhookTestEngine()

	payload := `{"id":"evt_test","type":"checkout.session.completed"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/stripe", io.NopCloser(stringReader(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", "t=0,v1=deadbeef")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code == http.StatusRequestEntityTooLarge {
		t.Fatalf("normal-sized webhook body was rejected as too large")
	}
	// Stripe 未配置 → StripeWebhookDisabled → 403。这证明 body 被完整读入
	// 并走到了 HandleWebhook，而不是在闸门处早退。
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (stripe not configured ⇒ disabled)", rec.Code, http.StatusForbidden)
	}
}

type stringReader string

func (s stringReader) Read(p []byte) (int, error) {
	n := copy(p, s)
	if n == len(s) {
		return n, io.EOF
	}
	return n, nil
}
