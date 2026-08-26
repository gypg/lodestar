package handlers

/*
WO-017 T5 — adminGrant 的入参校验与审计接线。

这里守三件事，每一件都对应一条变异：
  - reason 必填（M6）：无理由的余额改动等于无痕。
  - ActorID 必须是**操作的管理员**，不是受益人（M5）：搞混则流水看起来像用户自己给
    自己加了钱，审计彻底失效。
  - 加款与扣款都留流水，delta 保留符号（M9）。

断言落在**落库的流水行**上，不只看状态码 —— 只看 200 的话，一个"改了余额但不写流水"
的实现照样绿。
*/

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/server/middleware"
)

const grantAdminID = 9001

func setupGrantTest(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()),
	)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(&model.User{}, &model.QuotaLedger{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}

func grantUser(t *testing.T, label string, quota float64) uint {
	t.Helper()
	u := model.User{Username: "grant-" + label + "-" + t.Name(), Password: "x", Quota: quota}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u.ID
}

// callGrant 以 grantAdminID 的身份打一次 /wallet/grant，返回响应。
func callGrant(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/wallet/grant", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	// 生产里由 middleware.Auth() 写入；这里直接装上操作者身份。
	c.Set("user_id", grantAdminID)
	adminGrant(c)
	return rec
}

func grantLedger(t *testing.T, userID uint) []model.QuotaLedger {
	t.Helper()
	var rows []model.QuotaLedger
	if err := db.GetDB().Where("user_id = ?", userID).Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	return rows
}

func grantQuota(t *testing.T, userID uint) float64 {
	t.Helper()
	var u model.User
	if err := db.GetDB().Select("quota").First(&u, userID).Error; err != nil {
		t.Fatalf("read quota: %v", err)
	}
	return u.Quota
}

// 加款：余额入账、流水留痕、ActorID 是管理员而不是受益人。
func TestAdminGrantRecordsActorAndReason(t *testing.T) {
	setupGrantTest(t)
	uid := grantUser(t, "credit", 10)

	rec := callGrant(t, fmt.Sprintf(`{"user_id":%d,"amount":25,"reason":"客服补偿工单 #42"}`, uid))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	if q := grantQuota(t, uid); math.Abs(q-35) > 1e-9 {
		t.Fatalf("quota = %.17g, want 35", q)
	}

	rows := grantLedger(t, uid)
	if len(rows) != 1 {
		t.Fatalf("流水行数 = %d, want 1 —— 管理员改了余额却无痕", len(rows))
	}
	got := rows[0]
	if math.Abs(got.Delta-25) > 1e-9 {
		t.Errorf("delta = %.17g, want 25", got.Delta)
	}
	if got.Kind != model.LedgerKindAdminAdjust {
		t.Errorf("kind = %q, want %q", got.Kind, model.LedgerKindAdminAdjust)
	}
	if got.ActorID != grantAdminID {
		t.Errorf("actor_id = %d, want %d（操作的管理员，不是受益人 %d）", got.ActorID, grantAdminID, uid)
	}
	if got.UserID != uid {
		t.Errorf("user_id = %d, want %d（受益人）", got.UserID, uid)
	}
	if got.Reason != "客服补偿工单 #42" {
		t.Errorf("reason = %q, want %q", got.Reason, "客服补偿工单 #42")
	}
}

// 扣款（纠错平账）：delta 必须是负数，且允许把余额压到负。
// 取绝对值的实现会在这里红 —— 那样一次退款会变成再加一笔钱。
func TestAdminGrantNegativeAmountKeepsSign(t *testing.T) {
	setupGrantTest(t)
	uid := grantUser(t, "debit", 5)

	rec := callGrant(t, fmt.Sprintf(`{"user_id":%d,"amount":-12.5,"reason":"退款平账"}`, uid))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	if q := grantQuota(t, uid); math.Abs(q-(-7.5)) > 1e-9 {
		t.Fatalf("quota = %.17g, want -7.5（纠错必须允许余额为负，否则已交付的成本被丢掉）", q)
	}
	rows := grantLedger(t, uid)
	if len(rows) != 1 {
		t.Fatalf("流水行数 = %d, want 1", len(rows))
	}
	if math.Abs(rows[0].Delta-(-12.5)) > 1e-9 {
		t.Fatalf("delta = %.17g, want -12.5（符号丢了，扣款会变成加款）", rows[0].Delta)
	}
}

// reason 必填。空串、纯空白、字段缺失都要 400，且余额与流水分毫不动。
func TestAdminGrantRequiresReason(t *testing.T) {
	setupGrantTest(t)

	cases := map[string]string{
		"缺字段":  `{"user_id":%d,"amount":10}`,
		"空串":   `{"user_id":%d,"amount":10,"reason":""}`,
		"纯空白":  `{"user_id":%d,"amount":10,"reason":"   "}`,
		"制表换行": `{"user_id":%d,"amount":10,"reason":"\t\n "}`,
	}
	for label, tmpl := range cases {
		uid := grantUser(t, "noreason-"+label, 10)
		rec := callGrant(t, fmt.Sprintf(tmpl, uid))

		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s：status = %d, want 400; body=%s", label, rec.Code, rec.Body.String())
		}
		if q := grantQuota(t, uid); math.Abs(q-10) > 1e-9 {
			t.Errorf("%s：quota = %.17g, want 10（校验失败不得改余额）", label, q)
		}
		if rows := grantLedger(t, uid); len(rows) != 0 {
			t.Errorf("%s：流水行数 = %d, want 0", label, len(rows))
		}
	}
}

// amount == 0 拒掉：漏斗把 0 当 no-op，返回 200 会让管理员以为调整生效了。
func TestAdminGrantRejectsZeroAmount(t *testing.T) {
	setupGrantTest(t)
	uid := grantUser(t, "zero", 10)

	rec := callGrant(t, fmt.Sprintf(`{"user_id":%d,"amount":0,"reason":"手滑"}`, uid))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400（零额调整是无痕的成功，必须拒）; body=%s", rec.Code, rec.Body.String())
	}
	if rows := grantLedger(t, uid); len(rows) != 0 {
		t.Fatalf("流水行数 = %d, want 0", len(rows))
	}
}

// 非有限值：JSON decoder 会先挡掉 NaN 字面量（400），漏斗是第二道。
// 两端都堵，见 float-config-nonfinite-parse。
func TestAdminGrantRejectsNonFiniteAmount(t *testing.T) {
	setupGrantTest(t)
	uid := grantUser(t, "nonfinite", 10)

	for _, raw := range []string{"NaN", "Infinity", "-Infinity", "1e999"} {
		rec := callGrant(t, fmt.Sprintf(`{"user_id":%d,"amount":%s,"reason":"探测"}`, uid, raw))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("amount=%s：status = %d, want 400; body=%s", raw, rec.Code, rec.Body.String())
		}
		if q := grantQuota(t, uid); math.Abs(q-10) > 1e-9 {
			t.Fatalf("amount=%s 把余额毒化成 %.17g —— NaN 一旦落库，之后每个 remaining>0 都是 false，账户永久锁死", raw, q)
		}
		if rows := grantLedger(t, uid); len(rows) != 0 {
			t.Errorf("amount=%s：流水行数 = %d, want 0", raw, len(rows))
		}
	}
}

// user_id 缺失 → 400；用户不存在 → 404，且不留流水。
func TestAdminGrantValidatesTarget(t *testing.T) {
	setupGrantTest(t)

	rec := callGrant(t, `{"amount":10,"reason":"没填用户"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("缺 user_id：status = %d, want 400", rec.Code)
	}

	rec = callGrant(t, `{"user_id":424242,"amount":10,"reason":"查无此人"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("不存在的用户：status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	var n int64
	if err := db.GetDB().Model(&model.QuotaLedger{}).Count(&n).Error; err != nil {
		t.Fatalf("count ledger: %v", err)
	}
	if n != 0 {
		t.Errorf("给不存在的用户留下了 %d 行流水, want 0", n)
	}
}

// 审计中间件的白名单必须覆盖这个路由 —— 流水表记钱的账，审计表记人的账。
// 两张表都要有，缺了审计表就查不出"哪个管理员的会话动的手"。
func TestAdminGrantRouteIsAudited(t *testing.T) {
	if !middleware.ShouldAuditManagementWrite(http.MethodPost, "/api/v1/wallet/grant") {
		t.Fatal("POST /api/v1/wallet/grant 不在审计白名单里 —— 管理员调整余额不会进审计日志")
	}
}

// 响应体是标准 envelope，且不回显余额（避免把别人的账户状态漏给调用方）。
func TestAdminGrantResponseShape(t *testing.T) {
	setupGrantTest(t)
	uid := grantUser(t, "shape", 0)

	rec := callGrant(t, fmt.Sprintf(`{"user_id":%d,"amount":1,"reason":"ok"}`, uid))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("响应不是合法 JSON: %v; body=%s", err, rec.Body.String())
	}
	if code, ok := payload["code"].(float64); !ok || int(code) != http.StatusOK {
		t.Errorf("响应 code = %v, want 200; body=%s", payload["code"], rec.Body.String())
	}
	// 不回显余额：调整结果要去 /wallet/reconcile 或用户详情看，
	// 这个接口不该把目标账户的状态顺手漏出来。
	if _, leaked := payload["quota"]; leaked {
		t.Errorf("响应回显了余额: %s", rec.Body.String())
	}
}
