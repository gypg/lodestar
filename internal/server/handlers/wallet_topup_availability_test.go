package handlers

/*
充值方式可用性必须由 /api/v1/wallet/balance 下发，不能让前端读 setting 列表推断。

背景（这是个真实缺陷，不是防御性测试）：付费终端用户是 `user` 角色，它**故意**没有
settings:read —— 那个列表会暴露 epay_key / stripe_api_key 等密钥。而前端原先用
`settings?.find(k === stripe_enabled)?.value === 'true'` 当 Stripe 充值入口的开关，
对 `user` 角色这个请求 403、settings 为 undefined、表达式恒 false，
于是**Stripe 充值按钮对所有非管理员永不渲染**：开门收钱时客户根本看不到入口。

守三件事，每件对应一条变异：
  - M1 删掉 getWallet 里的 stripe_configured → 键缺失，前端拿到 undefined，
    按钮又消失（回到缺陷本身）。
  - M2 把它写死成 true → 未配置 Stripe 时也渲染按钮，用户点了必然失败。
  - M3 给 userPermissions 加上 settings:read → 前端"读 setting 推断"看似又能用了，
    但那等于把密钥列表发给每个付费客户。这条守的是本修复的**前提**。

断言落在**响应体里的键与值**上，不只看状态码：只看 200 的话，一个完全不返回
stripe_configured 的实现照样绿。
*/

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/server/auth"
)

// setupTopupAvailabilityTest 建一个内存库 + 一个用户，并把 Stripe 相关 setting
// 播进缓存（明文；DecryptSettingValue 对不带 "enc:" 前缀的值原样透传，
// 所以测试里不需要初始化加密密钥）。
func setupTopupAvailabilityTest(t *testing.T) uint {
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
	if err := db.GetDB().AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	u := model.User{Username: "topup-" + t.Name(), Password: "x", Quota: 12.5}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u.ID
}

// seedStripeSettings 把三个 Stripe setting 写进缓存。
// stripeClient() 要求 enabled 且 api key、webhook secret 都非空。
func seedStripeSettings(t *testing.T, enabled bool, apiKey, webhookSecret string) {
	t.Helper()
	c := setting.GetCache()
	flag := "false"
	if enabled {
		flag = "true"
	}
	c.Set(model.SettingKeyStripeEnabled, flag)
	c.Set(model.SettingKeyStripeAPIKey, apiKey)
	c.Set(model.SettingKeyStripeWebhookSecret, webhookSecret)
	t.Cleanup(func() {
		c.Set(model.SettingKeyStripeEnabled, "false")
		c.Set(model.SettingKeyStripeAPIKey, "")
		c.Set(model.SettingKeyStripeWebhookSecret, "")
	})
}

// callWalletBalance 照生产挂 getWallet，但不挂 Auth（直接注入 user_id），
// 因为这里要验的是响应体内容，不是鉴权。
func callWalletBalance(t *testing.T, uid uint) map[string]any {
	t.Helper()
	engine := gin.New()
	engine.GET("/api/v1/wallet/balance", func(c *gin.Context) {
		c.Set("user_id", int(uid))
		getWallet(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/wallet/balance", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	if envelope.Data == nil {
		t.Fatalf("response has no data object; body = %s", rec.Body.String())
	}
	return envelope.Data
}

// M1：Stripe 配置齐全时，响应必须带 stripe_configured=true。
// 删掉 getWallet 里那一行 → 键缺失 → 这里红。
func TestWalletBalanceReportsStripeConfiguredWhenReady(t *testing.T) {
	uid := setupTopupAvailabilityTest(t)
	seedStripeSettings(t, true, "sk_test_wiring", "whsec_test_wiring")

	data := callWalletBalance(t, uid)

	got, ok := data["stripe_configured"]
	if !ok {
		t.Fatalf("response is missing stripe_configured; the frontend cannot gate the "+
			"Stripe top-up button without it (data = %v)", data)
	}
	if got != true {
		t.Fatalf("stripe_configured = %v, want true (enabled + key + webhook secret all set)", got)
	}
}

// M2 反向守卫：Stripe 未启用时必须是 false。
// 把值写死成 true 的实现会在这里红。
func TestWalletBalanceReportsStripeNotConfiguredWhenDisabled(t *testing.T) {
	uid := setupTopupAvailabilityTest(t)
	// 开关关掉，但密钥仍在 —— 单看"密钥非空"会误判成可用。
	seedStripeSettings(t, false, "sk_test_wiring", "whsec_test_wiring")

	data := callWalletBalance(t, uid)

	got, ok := data["stripe_configured"]
	if !ok {
		t.Fatalf("response is missing stripe_configured (data = %v)", data)
	}
	if got != false {
		t.Fatalf("stripe_configured = %v, want false when stripe_enabled is off", got)
	}
}

// 缺密钥也算未配置：开关开着但 webhook secret 空，点下去必然失败，不该渲染入口。
func TestWalletBalanceReportsStripeNotConfiguredWhenSecretsMissing(t *testing.T) {
	uid := setupTopupAvailabilityTest(t)
	seedStripeSettings(t, true, "sk_test_wiring", "")

	data := callWalletBalance(t, uid)

	if got := data["stripe_configured"]; got != false {
		t.Fatalf("stripe_configured = %v, want false when webhook secret is empty", got)
	}
}

// epay 的同一条契约（既有行为，一并钉住 —— 它和 stripe 是同一个坑）。
func TestWalletBalanceReportsEpayAvailability(t *testing.T) {
	uid := setupTopupAvailabilityTest(t)

	data := callWalletBalance(t, uid)

	if _, ok := data["epay_configured"]; !ok {
		t.Fatalf("response is missing epay_configured (data = %v)", data)
	}
}

// M3：本修复的前提 —— `user` 角色必须**没有** settings:read。
// 若有人给 userPermissions 加上它，前端"读 setting 列表推断"会显得又能用了，
// 而代价是把 stripe_api_key / epay_key 发给每一个付费客户。
func TestEndCustomerRoleCannotReadSettings(t *testing.T) {
	if auth.HasPermission(model.UserRoleUser, auth.PermSettingsRead) {
		t.Fatal("user role must NOT have settings:read — that list carries stripe_api_key, " +
			"epay_key and smtp_pass; top-up availability is delivered via " +
			"/api/v1/wallet/balance instead")
	}
	// 对照组：管理员必须有，否则上面那条断言会因为权限表整体坏掉而假绿。
	if !auth.HasPermission(model.UserRoleAdmin, auth.PermSettingsRead) {
		t.Fatal("admin role lost settings:read — permission table is broken, " +
			"which would make the assertion above vacuously true")
	}
}
