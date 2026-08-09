package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	stg "github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
)

// S-8 调用点守卫：入口在 HTTP 路由（webauthnLoginBegin 的上游），
// 断言会话表满时映射成 429 而不是 400——若 handler 仍写死 StatusBadRequest，
// 这个测试会红。

func newWebAuthnBeginTestEngine(t *testing.T, configured bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}

	// 未配置时 BeginLogin 在 New() 就以 ErrNotConfigured 早退（返 400），
	// 压根到不了会话表——生产默认就是这个状态，也是 S-8 当前不可触达的原因。
	// 配置后才会真正写入会话表。
	if configured {
		stg.GetCache().Set(model.SettingKeyWebAuthnRPID, "example.com")
		stg.GetCache().Set(model.SettingKeyWebAuthnRPName, "Lodestar")
		stg.GetCache().Set(model.SettingKeyWebAuthnOrigins, "https://example.com")
	} else {
		stg.GetCache().Set(model.SettingKeyWebAuthnRPID, "")
		stg.GetCache().Set(model.SettingKeyWebAuthnOrigins, "")
	}

	engine := gin.New()
	// 照生产 init()：公开路由 + RequireJSON + LoginRateLimit。
	engine.POST("/api/v1/webauthn/login/begin",
		middleware.RequireJSON(), middleware.LoginRateLimit(), webauthnLoginBegin)
	return engine
}

func postBegin(engine *gin.Engine) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webauthn/login/begin", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// 正常情况下匿名 begin 返 200 并给出 session_token（反向守卫：
// 限流接线不能把正常登录一并挡掉）。
func TestWebAuthnLoginBeginSucceedsWhenConfigured(t *testing.T) {
	engine := newWebAuthnBeginTestEngine(t, true)

	rec := postBegin(engine)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response resp.ResponseStruct
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := response.Data.(map[string]any)
	if !ok {
		t.Fatalf("data is not an object: %#v", response.Data)
	}
	if tok, _ := data["session_token"].(string); tok == "" {
		t.Fatal("session_token is empty")
	}
}

// 会话表被打满后，匿名 begin 必须返 429（可重试），而不是 400。
// 429 让客户端与中间层能识别"限流"，400 会被当成请求格式错误。
func TestWebAuthnLoginBeginReturns429WhenSessionTableFull(t *testing.T) {
	engine := newWebAuthnBeginTestEngine(t, true)

	// 打满会话表：全部经由真实 HTTP 入口，不直接操作 webauthn 包内部状态。
	var lastCode int
	for i := 0; ; i++ {
		rec := postBegin(engine)
		lastCode = rec.Code
		if rec.Code != http.StatusOK {
			break
		}
		if i > 1<<16 {
			t.Fatal("session table never filled up; capacity guard appears to be missing")
		}
	}

	if lastCode != http.StatusTooManyRequests {
		t.Fatalf("status once full = %d, want %d", lastCode, http.StatusTooManyRequests)
	}
}

// 已被登录限流封禁的 IP 打 /login/begin 必须在**进入 handler 之前**被挡下。
//
// ★ 用"未配置 webauthn"这个拓扑来做判据，是为了让本测试与进程级会话表解耦：
// 会话表是全局的，上一个测试打满它之后同样会返 429，两种 429 无法区分。
// 未配置时 handler 必返 400（ErrNotConfigured），所以：
//   - 看到 400 ⇒ 请求穿过了限流、到达了 handler ⇒ 限流没接上
//   - 看到 429 ⇒ 限流在 handler 之前 abort 了
//
// ★ 边界要诚实：本测试用手工挂链的 engine，锁的是"LoginRateLimit 挂上后对这条
// 路径有效"，**不是**"生产 init() 真的挂了它"。要守后者需读 router 全局注册表，
// 而 RegisterAll 会把它置 nil（router.go:124）且同包 rbac_test 已消费过一次，
// 据此断言会变成执行顺序依赖的假绿；为它新增导出访问器又属于为测试扩大导出面。
// 这条接线的守卫是已知缺口，不假称已守住。
func TestWebAuthnLoginBeginBlocksRateLimitedIPBeforeHandler(t *testing.T) {
	engine := newWebAuthnBeginTestEngine(t, false)

	// 基线：未封禁时请求应穿过限流抵达 handler，因未配置而得 400。
	if rec := postBegin(engine); rec.Code != http.StatusBadRequest {
		t.Fatalf("baseline status = %d, want %d (unconfigured handler); body=%s",
			rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	// httptest.NewRequest 的 RemoteAddr 固定为 192.0.2.1:1234，
	// gin 未设可信代理时 ClientIP() 取的就是它。
	const clientIP = "192.0.2.1"
	middleware.ClearLoginFailures(clientIP)
	for i := 0; i < 6; i++ { // 默认阈值 5 次失败即封禁
		middleware.RecordLoginFailure(clientIP, time.Now())
	}
	t.Cleanup(func() { middleware.ClearLoginFailures(clientIP) })

	rec := postBegin(engine)
	if rec.Code == http.StatusBadRequest {
		t.Fatal("blocked IP still reached the handler; LoginRateLimit is not enforced on /login/begin")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status for blocked IP = %d, want %d; body=%s",
			rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
}
