package server

/*
WO-026 阶段 A：/api/v1/wallet/ledger 生产路由链验证。

handlers 包内不能用 router.RegisterAll（它把全局 registeredRouters 置 nil，
同包 rbac_test 已消费过一次；见 webauthn_ratelimit_route_test.go 头注释与
site_import_limit_test.go:46 的警告）。所以这条测试放在 internal/server 包，
用本包共享的 getProductionEngine —— RegisterAll 经 sync.Once 只跑一次，
engine 上挂的就是 wallet.go init() 注册出来的**生产**路由链。

它守的是 M-A2 那条变异：/ledger 路由绝不能被误加 RequirePermission 门。
user 角色（付费终端客户）只持 apikeys/stats/subscriptions 三类权限，
一旦挂门，客户看自己的流水就是 403 —— 那正是本工单要修的问题的反向重演
（Stripe 按钮 / 钱包导航 / 代理池列表，同类事故的第四次）。
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
	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/user"
	serverauth "github.com/gypg/lodestar/internal/server/auth"
)

// TestWalletLedgerRouteServesEndCustomer 钉死：生产路由链上的 GET /api/v1/wallet/ledger
// 对 user 角色返回 200（Auth() 即可，无 RequirePermission 门），且只返回本人流水。
func TestWalletLedgerRouteServesEndCustomer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	conf.AppConfig.Auth.JWTSecret = "test-jwt-secret-wo026-route"

	if err := op.UserInit(); err != nil {
		t.Fatalf("user init: %v", err)
	}
	customer := model.User{Username: "customer-" + t.Name(), Password: "x", Role: model.UserRoleUser, Quota: 3}
	if err := db.GetDB().Create(&customer).Error; err != nil {
		t.Fatalf("create customer: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}

	// 造一笔客户自己的流水 + 一笔别人的（隔离断言需要第二个人）。
	if err := user.MutateQuota(nil, customer.ID, 5, user.LedgerEntry{
		Kind: model.LedgerKindRedeem, RefType: "test", RefID: "mine",
	}, context.Background()); err != nil {
		t.Fatalf("mutate customer: %v", err)
	}
	other := model.User{Username: "other-" + t.Name(), Password: "x", Role: model.UserRoleUser, Quota: 3}
	if err := db.GetDB().Create(&other).Error; err != nil {
		t.Fatalf("create other: %v", err)
	}
	if err := user.MutateQuota(nil, other.ID, 9, user.LedgerEntry{
		Kind: model.LedgerKindRedeem, RefType: "test", RefID: "theirs",
	}, context.Background()); err != nil {
		t.Fatalf("mutate other: %v", err)
	}

	engine := getProductionEngine(t)

	token, _, err := serverauth.GenerateJWTToken(60, customer.ID, model.UserRoleUser)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/ledger?page=1&page_size=20", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	// 核心断言：user 角色必须 200。M-A2（路由被误加 RequirePermission 门）会在这里 403。
	if rec.Code != http.StatusOK {
		t.Fatalf("end-customer (user role) GET /wallet/ledger must return 200 via production routes, got %d "+
			"(403 would mean a RequirePermission gate was added to the route — the M-A2 variant; "+
			"404 would mean RegisterAll was consumed elsewhere); body=%s",
			rec.Code, rec.Body.String())
	}

	var envelope struct {
		Data struct {
			Entries []map[string]any `json:"entries"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	if len(envelope.Data.Entries) != 1 {
		t.Fatalf("expected exactly the customer's 1 entry (op-layer isolation), got %d; body=%s",
			len(envelope.Data.Entries), rec.Body.String())
	}
	if got := envelope.Data.Entries[0]["ref_id"]; got != "mine" {
		t.Fatalf("entry ref_id = %v, want \"mine\" — a cross-user leak would show \"theirs\"", got)
	}
}
