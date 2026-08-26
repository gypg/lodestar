package payment

/*
WO-017 M10 — epay 回调必须**经过漏斗**给用户入账。

为什么要有这条测试：门 A（ledger_gate_test.go）是静态扫描，只认字面量写法，能被间接
调用绕过。把这里的 user.MutateQuota 换回裸 `gorm.Expr("quota + ?")` 时，门 A 抓到的是
"payment.go 出现了未白名单的 quota 写入"；但如果换成别的绕过写法（Raw SQL、变量列名），
门 A 就哑了。只有断言"回调走完之后 quota_ledgers 里确实多了一行、且余额也动了"的行为
测试才守得住 —— 调用点变异只有接线测试杀得掉（见 session-2026-08-25-burst-overdraft）。

测试驱动的是真实的 HandleEpayNotify 入口，签名用 epay.GenerateParams 真算，
不是直接调内部函数 —— 后者会把"回调是否真的走到入账"这一段跳过去。
*/

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	epay "github.com/Calcium-Ion/go-epay/epay"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
)

const (
	testEpayPID = "1001"
	testEpayKey = "test-epay-key"
)

func initEpayTestDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()),
	)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(
		&model.User{}, &model.Setting{}, &model.PaymentOrder{}, &model.QuotaLedger{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("refresh settings: %v", err)
	}
	for k, v := range map[model.SettingKey]string{
		model.SettingKeyEpayEnabled: "true",
		model.SettingKeyPayAddress:  "https://pay.example.com/submit.php",
		model.SettingKeyEpayPID:     testEpayPID,
		model.SettingKeyEpayKey:     testEpayKey,
	} {
		if err := setting.SetString(k, v); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
	}
	if client() == nil {
		t.Fatal("epay client 未配置成功 —— 后面的回调会在 client()==nil 处早退，测试就成了空过")
	}
}

// signedNotify 构造一份**签名合法**的成功回调参数。
func signedNotify(tradeNo, money string) map[string]string {
	params := map[string]string{
		"pid":          testEpayPID,
		"trade_no":     "gw-" + tradeNo,
		"out_trade_no": tradeNo,
		"type":         "alipay",
		"name":         "Lodestar credit",
		"money":        money,
		"trade_status": epay.StatusTradeSuccess,
	}
	return epay.GenerateParams(params, testEpayKey)
}

func seedPendingOrder(t *testing.T, userID uint, tradeNo string, amountUSD float64) {
	t.Helper()
	order := model.PaymentOrder{
		UserID:    userID,
		AmountUSD: amountUSD,
		Money:     amountUSD * 7,
		TradeNo:   tradeNo,
		Method:    "alipay",
		Provider:  "epay",
		Status:    "pending",
	}
	if err := db.GetDB().Create(&order).Error; err != nil {
		t.Fatalf("seed order: %v", err)
	}
}

func seedEpayUser(t *testing.T, label string, quota float64) uint {
	t.Helper()
	u := model.User{Username: "epay-" + label + "-" + t.Name(), Password: "x", Quota: quota}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u.ID
}

func readLedger(t *testing.T, userID uint) []model.QuotaLedger {
	t.Helper()
	var rows []model.QuotaLedger
	if err := db.GetDB().Where("user_id = ?", userID).Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	return rows
}

func readQuota(t *testing.T, userID uint) float64 {
	t.Helper()
	var u model.User
	if err := db.GetDB().Select("quota").First(&u, userID).Error; err != nil {
		t.Fatalf("read quota: %v", err)
	}
	return u.Quota
}

// 成功回调：余额入账 **且** 流水留痕，两者都必须发生。
func TestEpayNotifyCreditsThroughLedgerFunnel(t *testing.T) {
	initEpayTestDB(t)
	ctx := context.Background()

	uid := seedEpayUser(t, "ok", 5)
	const tradeNo = "ls-epay-1"
	seedPendingOrder(t, uid, tradeNo, 20)

	if ok := HandleEpayNotify(signedNotify(tradeNo, "140.00"), ctx); !ok {
		t.Fatal("HandleEpayNotify = false, want true（签名合法的成功回调必须被 ack）")
	}

	if q := readQuota(t, uid); math.Abs(q-25) > 1e-9 {
		t.Fatalf("quota = %.17g, want 25（5 + 订单 20）", q)
	}

	rows := readLedger(t, uid)
	if len(rows) != 1 {
		t.Fatalf("流水行数 = %d, want 1 —— 入账绕过了漏斗，钱到账但无痕", len(rows))
	}
	got := rows[0]
	if math.Abs(got.Delta-20) > 1e-9 {
		t.Errorf("流水 delta = %.17g, want 20（取自 DB 订单，不是回调里的 money 140）", got.Delta)
	}
	if got.Kind != model.LedgerKindTopupEpay {
		t.Errorf("kind = %q, want %q", got.Kind, model.LedgerKindTopupEpay)
	}
	if got.RefType != model.LedgerRefPaymentOrder {
		t.Errorf("ref_type = %q, want %q", got.RefType, model.LedgerRefPaymentOrder)
	}
	if got.RefID != tradeNo {
		t.Errorf("ref_id = %q, want %q（对不上单据就查不回这笔钱的来源）", got.RefID, tradeNo)
	}
	if got.ActorID != 0 {
		t.Errorf("actor_id = %d, want 0（网关回调没有人工操作者）", got.ActorID)
	}
}

// 重复回调（网关重试）：第二次既不能再入账，也不能再写一行流水。
// 只断言余额的话，一个"余额幂等但流水重复"的实现照样绿，而那会让对账凭空多出一笔。
func TestEpayNotifyIsIdempotentForBalanceAndLedger(t *testing.T) {
	initEpayTestDB(t)
	ctx := context.Background()

	uid := seedEpayUser(t, "dup", 0)
	const tradeNo = "ls-epay-dup"
	seedPendingOrder(t, uid, tradeNo, 30)

	for i := 0; i < 3; i++ {
		if ok := HandleEpayNotify(signedNotify(tradeNo, "210.00"), ctx); !ok {
			t.Fatalf("第 %d 次回调 = false, want true", i+1)
		}
	}

	if q := readQuota(t, uid); math.Abs(q-30) > 1e-9 {
		t.Fatalf("quota = %.17g, want 30（重复回调不得重复入账）", q)
	}
	if rows := readLedger(t, uid); len(rows) != 1 {
		t.Fatalf("流水行数 = %d, want 1（重复回调写了重复流水，对账会凭空多出金额）", len(rows))
	}
}

// 签名错误：既不入账也不留流水，且不 ack。
// 这是上面两条的反向对照 —— 若 HandleEpayNotify 恒真恒入账，这条会红。
func TestEpayNotifyRejectsBadSignature(t *testing.T) {
	initEpayTestDB(t)
	ctx := context.Background()

	uid := seedEpayUser(t, "badsig", 5)
	const tradeNo = "ls-epay-bad"
	seedPendingOrder(t, uid, tradeNo, 20)

	params := signedNotify(tradeNo, "140.00")
	params["sign"] = "deadbeefdeadbeefdeadbeefdeadbeef"

	if ok := HandleEpayNotify(params, ctx); ok {
		t.Fatal("HandleEpayNotify = true 对签名错误的回调，want false")
	}
	if q := readQuota(t, uid); math.Abs(q-5) > 1e-9 {
		t.Fatalf("quota = %.17g, want 5（签名错误不得入账）", q)
	}
	if rows := readLedger(t, uid); len(rows) != 0 {
		t.Fatalf("流水行数 = %d, want 0", len(rows))
	}

	// 订单必须仍是 pending —— 否则真回调到达时会被当成"已处理"而永久丢单。
	var status string
	if err := db.GetDB().Model(&model.PaymentOrder{}).
		Where("trade_no = ?", tradeNo).Select("status").Scan(&status).Error; err != nil {
		t.Fatalf("read order: %v", err)
	}
	if status != "pending" {
		t.Fatalf("订单状态 = %q, want pending", status)
	}
}
