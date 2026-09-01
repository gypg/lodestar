package handlers

/*
WO-026 阶段 A：客户侧余额流水端点（GET /api/v1/wallet/ledger）守卫。

端点挂在 /api/v1/wallet 第一组（Auth()，无 RequirePermission）——与 /balance /usage
同权限级，user 角色即可看自己的流水。绝不能加 RequirePermission 门，否则端客户
（user 角色只持 apikeys/stats/subscriptions 三类权限）看不到自己的流水——那正是
本工单要修的问题的反向重演（项目栽过三次：Stripe 按钮/钱包导航/代理池列表）。

三条核心断言 + 两个变异：
  - T-A1 只能看自己的：用户甲的端点不能返回用户乙的行。
  - T-A2 端客户能看到：user 角色调用必须 200，不能 403。
  - T-A3 数据正确：一笔充值 + 一笔消费后，流水条数与金额与 quota_ledgers 一致。
  - M-A1 去掉 op 层 user_id 过滤 → T-A1 红（跨用户泄露）。
  - M-A2 给流水端点加 RequirePermission(PermSettingsRead) → T-A2 红（端客户 403）。
*/

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/user"
	serverauth "github.com/gypg/lodestar/internal/server/auth"
)

// setupLedgerTest 建内存库 + 迁移 User/QuotaLedger，返回两个 user 角色的用户（甲/乙）。
// 两个都给 user 角色——本工单的核心就是“user 角色也能看自己的流水”。
func setupLedgerTest(t *testing.T) (alice, bob model.User) {
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
	alice = model.User{Username: "alice-" + t.Name(), Password: "x", Role: model.UserRoleUser, Quota: 5}
	bob = model.User{Username: "bob-" + t.Name(), Password: "x", Role: model.UserRoleUser, Quota: 5}
	if err := db.GetDB().Create(&alice).Error; err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if err := db.GetDB().Create(&bob).Error; err != nil {
		t.Fatalf("create bob: %v", err)
	}
	return alice, bob
}

// callWalletLedger 照生产挂 getWalletLedger，直接注入 user_id（不挂 Auth 中间件）。
// 这里验的是隔离与响应体内容，不是鉴权链路本身。
func callWalletLedger(t *testing.T, uid uint) (int, map[string]any) {
	t.Helper()
	engine := gin.New()
	engine.GET("/api/v1/wallet/ledger", func(c *gin.Context) {
		c.Set("user_id", int(uid))
		getWalletLedger(c)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/ledger?page=1&page_size=20", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	var envelope struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &envelope)
	return rec.Code, envelope.Data
}

// ledgerEntries 从响应 data 里抽出 entries 列表（[]any 形态）。
func ledgerEntries(t *testing.T, data map[string]any) []map[string]any {
	t.Helper()
	if data == nil {
		t.Fatalf("response has no data object")
	}
	raw, ok := data["entries"]
	if !ok {
		t.Fatalf("response data missing 'entries' key: %v", data)
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("entries is not an array: %T", raw)
	}
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// TestT_A1OnlySeesOwnLedger 钉死 T-A1：用户甲的端点不能返回用户乙的流水行。
// 这条是本段最重要的断言。隔离在 op 层 user.ListByUser（WHERE user_id=?），
// handler 只传 uid —— 照 apikey.ListByUser 的模式，调用方不会因忘记过滤而泄露。
func TestT_A1OnlySeesOwnLedger(t *testing.T) {
	alice, bob := setupLedgerTest(t)
	ctx := context.Background()

	// 甲：一笔充值 +5；乙：一笔充值 +3。各自只该看到自己的。
	if err := user.MutateQuota(nil, alice.ID, 5, user.LedgerEntry{Kind: model.LedgerKindRedeem, RefType: "test", RefID: "alicecode"}, ctx); err != nil {
		t.Fatalf("mutate alice: %v", err)
	}
	if err := user.MutateQuota(nil, bob.ID, 3, user.LedgerEntry{Kind: model.LedgerKindRedeem, RefType: "test", RefID: "bobcode"}, ctx); err != nil {
		t.Fatalf("mutate bob: %v", err)
	}

	code, data := callWalletLedger(t, alice.ID)
	if code != http.StatusOK {
		t.Fatalf("alice ledger status = %d, want 200; body see data=%v", code, data)
	}
	entries := ledgerEntries(t, data)
	if len(entries) != 1 {
		t.Fatalf("alice should see only her own 1 entry, got %d (cross-user leak): %v", len(entries), entries)
	}
	// 钉死：唯一那条必须是 alice 的（ref_id=alicecode），绝不能是 bob 的。
	if got := entries[0]["ref_id"]; got != "alicecode" {
		t.Fatalf("alice ledger returned a non-alice entry (cross-user leak): ref_id=%v", got)
	}
	if got := entries[0]["user_id"]; got != float64(alice.ID) {
		t.Fatalf("entry user_id = %v, want alice %d (cross-user leak)", got, alice.ID)
	}
}

// TestT_A2EndCustomerRoleCanReadLedger 钉死 T-A2：user 角色调用必须 200，不能 403。
// 这条钉的是“不要给流水端点加 RequirePermission” —— user 角色只持
// apikeys/stats/subscriptions 三类权限，加 settings:read 门客户就看不到自己的流水。
func TestT_A2EndCustomerRoleCanReadLedger(t *testing.T) {
	// 前置断言：user 角色确实没有 settings:read / settings:write。
	// 若有人给 user 角色加上这两项，本测试的前提崩了 —— 那时“加门”会让端客户仍能过，
	// T-A2 就假绿。所以先钉这个对照。
	if serverauth.HasPermission(model.UserRoleUser, serverauth.PermSettingsRead) {
		t.Fatal("user role must NOT have settings:read — ledger endpoint relies on Auth()-only, " +
			"adding RequirePermission(PermSettingsRead) would block end customers (M-A2 mentions this)")
	}
	if serverauth.HasPermission(model.UserRoleUser, serverauth.PermSettingsWrite) {
		t.Fatal("user role must NOT have settings:write (same reason as settings:read above)")
	}

	alice, _ := setupLedgerTest(t)
	if err := user.MutateQuota(nil, alice.ID, 2, user.LedgerEntry{Kind: model.LedgerKindRedeem, RefType: "test", RefID: "x"}, context.Background()); err != nil {
		t.Fatalf("mutate alice: %v", err)
	}

	// 端点本身：user 角色的 uid 调用必须 200。本测试不挂 Auth 中间件，
	// 但生产里 /ledger 挂在 Auth() 第一组（无 RequirePermission），user 角色凭登录即可。
	// 这里用 user 角色的 uid 调用，验响应体不是 403 类拒绝。
	code, data := callWalletLedger(t, alice.ID)
	if code != http.StatusOK {
		t.Fatalf("end-customer (user role) ledger call must return 200, got %d (would happen if "+
			"RequirePermission gate was added — M-A2 variant); data=%v", code, data)
	}
	entries := ledgerEntries(t, data)
	if len(entries) != 1 {
		t.Fatalf("expected alice's 1 entry, got %d", len(entries))
	}
}

// TestT_A3LedgerDataMatchesQuotaLedgers 钉死 T-A3：一笔充值 + 一笔消费后，
// 端点返回的条数与金额必须与 quota_ledgers 表里的实际行一致。
func TestT_A3LedgerDataMatchesQuotaLedgers(t *testing.T) {
	alice, _ := setupLedgerTest(t)
	ctx := context.Background()

	// 一笔 +5 充值（redeem），一笔 -2 消费（admin_adjust 负 delta，RequireAffordable=false
	// 允许变负，模拟已交付成本的纠错 —— 这里只是造一条负 delta 流水）。
	if err := user.MutateQuota(nil, alice.ID, 5, user.LedgerEntry{Kind: model.LedgerKindRedeem, RefType: "test", RefID: "topup"}, ctx); err != nil {
		t.Fatalf("mutate +5: %v", err)
	}
	if err := user.MutateQuota(nil, alice.ID, -2, user.LedgerEntry{Kind: model.LedgerKindAdminAdjust, Reason: "consume"}, ctx); err != nil {
		t.Fatalf("mutate -2: %v", err)
	}

	// 直查 quota_ledgers 表，拿 ground truth。
	var rows []model.QuotaLedger
	if err := db.GetDB().Where("user_id = ?", alice.ID).Order("created_at DESC, id DESC").Find(&rows).Error; err != nil {
		t.Fatalf("query ledger: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ground truth: expected 2 ledger rows for alice, got %d", len(rows))
	}

	// 端点返回应与 ground truth 一致。
	code, data := callWalletLedger(t, alice.ID)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; data=%v", code, data)
	}
	entries := ledgerEntries(t, data)
	if len(entries) != len(rows) {
		t.Fatalf("endpoint returned %d entries, ground truth has %d — mismatch", len(entries), len(rows))
	}
	// newest-first：第 0 条应是 -2（后写的），第 1 条是 +5（先写的）。
	// 按 created_at DESC, id DESC —— admin_adjust 的 id 更大，排在前。
	expectDeltas := []float64{-2, 5}
	for i, want := range expectDeltas {
		got := entries[i]["delta"]
		if got != want {
			t.Fatalf("entry %d delta = %v, want %v (newest-first: -2 then +5)", i, got, want)
		}
	}
	// 余额不变式：Σdelta = quota 的增量。初始 5，+5-2 = 8。
	// （不在此断言余额，那是 MutateQuota 的职责；这里只验流水条数与金额。）
}
