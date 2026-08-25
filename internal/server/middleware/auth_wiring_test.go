package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/op/setting"
)

// ---------------------------------------------------------------------------
// WO-011 — 余额检查接线断言（auth.go:171 的 billing.HasBalanceForKey 调用）
//
// 目标：防止重构时误删"请求进入前先检查余额"这条闸门。删除 auth.go:171 的
// HasBalanceForKey 检查后，余额=0 的 key 不再被 402 拒绝，本测试必须红。
//
// 策略：走 APIKeyAuth 中间件的真实 HTTP 路径，用余额=0 的 key 发请求：
//   - commercial_mode 开 + 余额 0 → 应被拒绝（402 PaymentRequired）
//   - 删掉 auth.go:171 的检查 → 请求放行到 handler → 状态码不再 402 → 红
//
// 同时用余额充足的 key 验证正常放行（handler 返回 200），锁定闸门"该放行时放行"。
// ---------------------------------------------------------------------------

// initAuthWiringDB 搭建内存 SQLite + 缓存 + 开启 commercial_mode，创建带指定余额的
// 用户和其名下 API key。返回 key 的明文值（用于构造 Authorization 头）。
func initAuthWiringDB(t *testing.T, quota float64) string {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := setting.SetString(model.SettingKeyCommercialMode, "true"); err != nil {
		t.Fatalf("enable commercial mode: %v", err)
	}
	u := model.User{Username: "auth-wire-" + t.Name(), Password: "x", Quota: quota}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	keyVal := "sk-lodestar-authwire-" + t.Name()
	key := model.APIKey{UserID: u.ID, Name: "wire-key", APIKey: keyVal}
	if err := apikey.Create(&key, context.Background()); err != nil {
		t.Fatalf("create key: %v", err)
	}
	return keyVal
}

func newAuthWiringEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(APIKeyAuth())
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	return r
}

func doAuthWireRequest(r *gin.Engine, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestAuthWiring_insufficientBalance_rejectsRequest
// 余额=0 的用户 key：commercial_mode 开 → 必须被 402 拒绝。若 auth.go:171 的
// HasBalanceForKey 检查被删 → 请求放行到 handler 返回 200 → 红。
func TestAuthWiring_insufficientBalance_rejectsRequest(t *testing.T) {
	key := initAuthWiringDB(t, 0.0) // 余额 0
	r := newAuthWiringEngine()

	w := doAuthWireRequest(r, key)
	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("zero-balance key status = %d, want %d (402) — HasBalanceForKey gate (auth.go:171) likely removed", w.Code, http.StatusPaymentRequired)
	}
}

// TestAuthWiring_sufficientBalance_allowsRequest
// 余额充足的用户 key：应放行到 handler → 200。锁定闸门不误伤正常请求。
func TestAuthWiring_sufficientBalance_allowsRequest(t *testing.T) {
	key := initAuthWiringDB(t, 10.0) // 余额充足
	r := newAuthWiringEngine()

	w := doAuthWireRequest(r, key)
	if w.Code != http.StatusOK {
		t.Fatalf("funded-key status = %d, want %d (200) — balance gate rejecting funded requests", w.Code, http.StatusOK)
	}
}

// TestAuthWiring_billingOff_allowsZeroBalance
// commercial_mode 关闭（自用）时，余额=0 的 key 也必须放行（fail-open）。锁定
// "开关"与"接线"是两回事：改开关不能掩盖接线被删——开关关闭时请求能过是预期的，
// 但这条同时证明：即使商业模式没开，APIKeyAuth 仍会走到 HasBalanceForKey 后再放行。
func TestAuthWiring_billingOff_allowsZeroBalance(t *testing.T) {
	key := initAuthWiringDB(t, 0.0)
	_ = setting.SetString(model.SettingKeyCommercialMode, "false")
	r := newAuthWiringEngine()

	w := doAuthWireRequest(r, key)
	if w.Code != http.StatusOK {
		t.Fatalf("billing-off zero-balance status = %d, want %d (200 fail-open)", w.Code, http.StatusOK)
	}
}

// ---------------------------------------------------------------------------
// 并发闸门接线断言（auth.go 的 billing.AcquireForKey + defer release）
//
// 上面三条只能证明"余额闸门在链上"。它们证明不了本次修复的东西：纯谓词闸门在并发
// 下形同不存在 —— 一次打进来的 N 条请求全都在第一条结算之前通过了 `remaining > 0`。
// 实测（改之前，上游带真实延迟）：余额 $0.005 的用户并发 20 条，20 条全被服务，
// 结算完 -$0.205。
//
// 所以这里要复现"请求互相重叠"：handler 阻塞住不返回，第一条还没结算时后面的就打
// 进来。单元测试（internal/op/billing/inflight_test.go）只覆盖判定规则本身，覆盖
// 不到"槽位到底有没有在中间件里被 defer 还回去"——那只能走真实 HTTP 链路。
// ---------------------------------------------------------------------------

// newBlockingAuthWiringEngine 的 handler 会先报告"我进来了"，再挂住等 hold 关闭，
// 借此让请求互相重叠。带 3 秒兜底超时：闸门被改坏时后续请求会真的进到 handler，
// 有超时才会得到一条清晰的断言失败，而不是把测试挂死。
func newBlockingAuthWiringEngine(entered chan<- struct{}, hold <-chan struct{}) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(APIKeyAuth())
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		entered <- struct{}{}
		select {
		case <-hold:
		case <-time.After(3 * time.Second):
		}
		c.String(http.StatusOK, "ok")
	})
	return r
}

// TestAuthWiring_concurrentBurstOnThinBalance_servesOne
// 余额 $0.005（连一次请求都不一定盖得住）的 key：一条请求正挂在 handler 里没结算时，
// 再并发打 19 条，19 条必须全被 402 拦下。
//
// 故意不改 max_expected_request_cost，用出厂默认值跑 —— 顺带钉死"默认值不是 0"，
// 否则生产上这道闸门是关着的，测试却在自己设的值上绿。
func TestAuthWiring_concurrentBurstOnThinBalance_servesOne(t *testing.T) {
	const burst = 19
	key := initAuthWiringDB(t, 0.005)

	if got, err := setting.GetString(model.SettingKeyMaxExpectedRequestCost); err != nil || got != "0.5" {
		t.Fatalf("出厂默认假定成本 = %q err=%v, want \"0.5\"（默认值若为 0 或读不到，生产的并发闸门就是关着的）", got, err)
	}

	entered := make(chan struct{}, burst+1)
	hold := make(chan struct{})
	r := newBlockingAuthWiringEngine(entered, hold)

	// 第一条：放行，并且卡在 handler 里持有槽位。
	firstCode := make(chan int, 1)
	go func() { firstCode <- doAuthWireRequest(r, key).Code }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("第一条请求没进到 handler —— 有钱的账户被闸门误伤了")
	}

	// 第一条尚未结算，此刻并发打 burst 条。
	var wg sync.WaitGroup
	codes := make([]int, burst)
	for i := 0; i < burst; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = doAuthWireRequest(r, key).Code
		}(i)
	}
	wg.Wait()

	refused := 0
	for _, c := range codes {
		if c == http.StatusPaymentRequired {
			refused++
		}
	}
	if refused != burst {
		t.Fatalf("一条在途未结算时并发 %d 条: 被 402 拦下 %d 条, want %d —— 其余被服务了, 透支敞口 = 并发数 × 单次成本（这正是修复前的实测行为: 20/20 全服务, 停在 -$0.205）",
			burst, refused, burst)
	}

	close(hold)
	if code := <-firstCode; code != http.StatusOK {
		t.Fatalf("第一条请求 = %d, want 200", code)
	}

	// 槽位必须被 defer release 还回去：还不回来的话，这个还有钱的账户会被自己上一条
	// 请求永久挡在门外。
	if w := doAuthWireRequest(r, key); w.Code != http.StatusOK {
		t.Fatalf("上一条完成后再发一条 = %d, want 200（auth.go 的 defer release 没生效，账户被自己挡死）", w.Code)
	}
}

// TestAuthWiring_slotReleasedAfterEachRequest
// 薄余额（只够一条在途）连发三条顺序请求，三条都必须 200。删掉 auth.go 的
// defer release 之后，第二条就会 402 —— 顺序请求本来永远不该受并发闸门影响。
func TestAuthWiring_slotReleasedAfterEachRequest(t *testing.T) {
	key := initAuthWiringDB(t, 0.005)
	r := newAuthWiringEngine()

	for i := 1; i <= 3; i++ {
		w := doAuthWireRequest(r, key)
		if w.Code != http.StatusOK {
			t.Fatalf("第 %d 条顺序请求 = %d, want 200（前一条的在途槽位没还回来）", i, w.Code)
		}
	}
}
