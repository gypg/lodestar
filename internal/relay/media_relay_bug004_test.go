package relay

/*
WO-013 — BUG-004 媒体双倍扣款 + multipart nil-body 计费测试。

BUG-004: recordMediaRelayLog 末尾调用 ChargeKeyWithExpr，它内部会对
resolvedModel 重新执行 ComputeExprCost（不带 body），用新算出的值覆盖传入的
mediaCost —— 日志记 $5 钱包扣 $10。修复后媒体路径直接调 billing.ChargeKey，
底层扣款只发生一次、金额恰为 mediaCost（余额断言 + 调用计数双保险）。

BUG-004b: multipart 端点（images/edits、images/variations、audio/transcriptions）
bodyBytes==nil，param('size') 等取不到字段。修复后 MediaHandler 把 multipart
form 字段序列化成 JSON body（multipartFormToJSONBody），cost 路径能看到字段。
*/

import (
	"context"
	"fmt"
	"math"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/apikey"
	billing "github.com/gypg/lodestar/internal/op/billing"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/user"
)

// TestChargeKey_NoDuplication_MediaPath
// 场景：requestModel 与 resolvedModel 都配了 param('size') 表达式，请求体带
// 1024x1024。日志路径按 requestModel 算得 5.0；若接回 ChargeKeyWithExpr
// （BUG-004），它会按 resolvedModel 无 body 重算 → param('size')=nil → else
// 分支 10.0 覆盖 mediaCost → 余额扣 10.0。断言余额恰好扣 5.0 + 调用恰一次，
// 双保险防复发。
func TestChargeKey_NoDuplication_MediaPath(t *testing.T) {
	uid, kid := initBug004BillingEnv(t)

	const requestModel = "test-image-model"
	const resolvedModel = "gpt-image-1"
	expr := `param('size') == '1024x1024' ? 5.0 : 10.0`
	if err := setting.SetString(model.SettingKeyBillingExpr,
		fmt.Sprintf(`{"%s":%q,"%s":%q}`, requestModel, expr, resolvedModel, expr)); err != nil {
		t.Fatalf("set billing expr: %v", err)
	}

	var chargeCalls []float64
	billing.CallRecorder = func(_ int, _ string, _, _ int, cost float64) {
		chargeCalls = append(chargeCalls, cost)
	}
	t.Cleanup(func() { billing.CallRecorder = nil })

	body := []byte(`{"size":"1024x1024","prompt":"a cat"}`)
	rl := recordMediaRelayLogForTest(requestModel, body, withAPIKeyID(kid), withResolvedModel(resolvedModel))

	if math.Abs(rl.RelayLogCost-5.0) > 1e-9 {
		t.Errorf("relayLog.Cost: want 5.0, got %.9f", rl.RelayLogCost)
	}
	if len(chargeCalls) != 1 {
		t.Fatalf("ChargeKey must be called exactly once (BUG-004 double-charge), got %d calls: %v", len(chargeCalls), chargeCalls)
	}
	if math.Abs(chargeCalls[0]-5.0) > 1e-9 {
		t.Errorf("ChargeKey cost: want 5.0 (mediaCost passthrough), got %.9f", chargeCalls[0])
	}

	rem, _, err := user.GetQuota(uid, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(rem-995.0) > 1e-6 {
		t.Errorf("balance: want 995.0 after a single 5.0 charge, got %.9f (BUG-004 re-charge would be 990)", rem)
	}
}

// TestMultipartBody_ParamExtraction
// 场景：multipart 端点（cfg.MultipartInput=true）bodyBytes==nil，但
// c.Request.MultipartForm 含 size 字段。extractBodyForBilling 必须把 form
// 序列化成 JSON body，使表达式命中 then 分支 → 8.0。
// 删除提取接线（extractBodyForBilling 原样返回 nil）→ param 取不到 size →
// else 分支 5.0 → 红。可证伪。
func TestMultipartBody_ParamExtraction(t *testing.T) {
	uid, kid := initBug004BillingEnv(t)

	const requestModel = "test-image-model"
	// then 分支是 8.0：只有 size 真正被提取出来才会命中；param=nil 会落 else 5.0。
	expr := `param('size') == '1792x1024' ? 8.0 : 5.0`
	if err := setting.SetString(model.SettingKeyBillingExpr,
		fmt.Sprintf(`{"%s":%q}`, requestModel, expr)); err != nil {
		t.Fatalf("set billing expr: %v", err)
	}

	// 构造 multipart 请求：bodyBytes 为 nil（与 extractModelFromMultipart 一致），
	// 但 MultipartForm 已解析含 size 字段。
	c, _ := gin.CreateTestContext(nil)
	c.Request = &http.Request{
		Method: http.MethodPost,
		Header: http.Header{"Content-Type": {`multipart/form-data; boundary=x`}},
	}
	c.Request.MultipartForm = &multipart.Form{
		Value: map[string][]string{
			"model": {requestModel},
			"size":  {"1792x1024"},
		},
	}
	cfg := getMediaEndpointConfig(MediaEndpointImageEdit)
	if !cfg.MultipartInput {
		t.Fatal("precondition: image edits endpoint must be multipart input")
	}

	jsonBody := extractBodyForBilling(c, cfg, nil)
	if jsonBody == nil {
		t.Fatal("extractBodyForBilling returned nil body for multipart form with fields")
	}

	var chargeCalls []float64
	billing.CallRecorder = func(_ int, _ string, _, _ int, cost float64) {
		chargeCalls = append(chargeCalls, cost)
	}
	t.Cleanup(func() { billing.CallRecorder = nil })

	// 走 recordMediaRelayLog 全流程（bodyBytes 传提取后的 JSON）。
	rl := recordMediaRelayLogForTest(requestModel, jsonBody, withAPIKeyID(kid))

	if math.Abs(rl.RelayLogCost-8.0) > 1e-9 {
		t.Errorf("relayLog.Cost: want 8.0 (then branch via extracted size), got %.9f", rl.RelayLogCost)
	}
	if len(chargeCalls) != 1 {
		t.Fatalf("ChargeKey calls: want exactly 1, got %d: %v", len(chargeCalls), chargeCalls)
	}
	if math.Abs(chargeCalls[0]-8.0) > 1e-9 {
		t.Errorf("ChargeKey cost: want 8.0, got %.9f", chargeCalls[0])
	}

	rem, _, err := user.GetQuota(uid, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(rem-992.0) > 1e-6 {
		t.Errorf("balance: want 992.0 after single 8.0 charge, got %.9f", rem)
	}
}

// TestMultipartBody_NilForm_NoCharge
// 场景：MultipartForm 为 nil（纯空请求）→ multipartFormToJSONBody 不 panic；
// bodyBytes 保持 nil、模型无表达式 → cost 0，余额不动，ChargeKey 仍触达一次
// （金额 0，不扣钱）。
func TestMultipartBody_NilForm_NoCharge(t *testing.T) {
	uid, kid := initBug004BillingEnv(t)

	// 不配表达式（无表达式 fallback → cost 0）。
	if err := setting.SetString(model.SettingKeyBillingExpr, ""); err != nil {
		t.Fatalf("clear billing expr: %v", err)
	}

	// multipart 端点但 MultipartForm 为 nil（纯空请求）：extractBodyForBilling
	// 必须返回原样 nil（不 panic），且不会误提取。
	c, _ := gin.CreateTestContext(nil)
	c.Request = &http.Request{Method: http.MethodPost}
	cfg := getMediaEndpointConfig(MediaEndpointImageEdit)
	if got := extractBodyForBilling(c, cfg, nil); got != nil {
		t.Fatalf("extractBodyForBilling with nil MultipartForm: want nil body, got %q", got)
	}
	// 底层序列化器对 nil form 同样安全（不 panic）。
	if got := multipartFormToJSONBody(nil); got != nil {
		t.Fatalf("multipartFormToJSONBody(nil): want nil body (no panic), got %q", got)
	}

	var chargeCalls []float64
	billing.CallRecorder = func(_ int, _ string, _, _ int, cost float64) {
		chargeCalls = append(chargeCalls, cost)
	}
	t.Cleanup(func() { billing.CallRecorder = nil })

	rl := recordMediaRelayLogForTest("no-expr-model", nil, withAPIKeyID(kid))

	if math.Abs(rl.RelayLogCost-0.0) > 1e-9 {
		t.Errorf("relayLog.Cost: want 0.0 (no expr), got %.9f", rl.RelayLogCost)
	}
	if len(chargeCalls) != 1 {
		t.Fatalf("ChargeKey calls: want exactly 1 (zero-value charge still reaches call site), got %d: %v", len(chargeCalls), chargeCalls)
	}
	if math.Abs(chargeCalls[0]-0.0) > 1e-9 {
		t.Errorf("ChargeKey cost: want 0.0 (no charge), got %.9f", chargeCalls[0])
	}

	rem, _, err := user.GetQuota(uid, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(rem-1000.0) > 1e-6 {
		t.Errorf("balance: want 1000.0 (unchanged), got %.9f", rem)
	}
}

// ---- 测试辅助 ----

// initBug004BillingEnv 搭起 DB + 缓存 + commercial_mode 开（让 ChargeKey 真扣费，
// 只有真扣费路径才能暴露"调了两次/金额错"）+ User/APIKey 表。
// 返回 (userID, keyID)。
func initBug004BillingEnv(t *testing.T) (uint, int) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := db.GetDB().AutoMigrate(&model.Setting{}, &model.User{}, &model.APIKey{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := setting.RefreshCache(nil); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	if err := setting.SetString(model.SettingKeyCommercialMode, "true"); err != nil {
		t.Fatalf("enable commercial mode: %v", err)
	}
	u := model.User{Username: "bug004-user", Password: "x", Quota: 1000.0}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	key := model.APIKey{UserID: u.ID, APIKey: "sk-bug004-" + t.Name()}
	if err := apikey.Create(&key, context.Background()); err != nil {
		t.Fatalf("create api key: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return u.ID, key.ID
}

// withAPIKeyID 覆盖 recordMediaRelayLogForTest 的默认 apiKeyID（99001），
// 让扣费落在本测试创建的 owned key 上。
func withAPIKeyID(id int) mediaRelayOpt {
	return func(_ *mediaRelayCostResult, apiKeyID *int, _ *string) {
		*apiKeyID = id
	}
}
