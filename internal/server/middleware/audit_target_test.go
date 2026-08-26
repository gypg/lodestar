package middleware

/*
WO-017 T5 附带 — 审计行的 target 必须能回答"这次操作动的是谁/什么"。

流水表记钱的账（谁、给谁、多少、为什么），审计表记人的账（哪个管理员、什么时候、
动了哪个接口）。审计行的 target 为空时，`wallet.grant` 这条记录只剩"有人调了这个
接口"，连受益人都查不出来 —— 那这条审计等于没记。
*/

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBuildAuditTargetCapturesGrantBeneficiary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	// adminGrant 的请求体：{user_id, amount, reason}。
	body := map[string]any{"user_id": float64(42), "amount": 25.0, "reason": "客服补偿"}
	got := buildAuditTarget(c, "/api/v1/wallet/grant", body, "admin")

	if got != "user_id=42" {
		t.Fatalf("target = %q, want %q（空串意味着审计行记不住给了谁）", got, "user_id=42")
	}
}

// 人类可读的标识优先于数字 ID —— 新加的 user_id 键不得抢在 username 前面，
// 否则既有路由的 target 会从用户名退化成数字。
func TestBuildAuditTargetPrefersHumanReadableKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	cases := []struct {
		label string
		body  map[string]any
		want  string
	}{
		{"username 优先于 user_id", map[string]any{"username": "alice", "user_id": float64(7)}, "alice"},
		{"name 优先于 user_id", map[string]any{"name": "my-channel", "user_id": float64(7)}, "my-channel"},
		{"只有 user_id 时才用它", map[string]any{"user_id": float64(7)}, "user_id=7"},
		{"什么都没有则为空", map[string]any{"amount": 1.0}, ""},
	}
	for _, tc := range cases {
		if got := buildAuditTarget(c, "/api/v1/some/route", tc.body, ""); got != tc.want {
			t.Errorf("%s：target = %q, want %q", tc.label, got, tc.want)
		}
	}
}

// 路由参数 :id 仍然最优先 —— 它是 URL 里的操作对象，比 body 更可靠。
func TestBuildAuditTargetPrefersPathParam(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Params = gin.Params{{Key: "id", Value: "99"}}

	if got := buildAuditTarget(c, "/api/v1/user/:id", map[string]any{"user_id": float64(7)}, ""); got != "id=99" {
		t.Fatalf("target = %q, want %q", got, "id=99")
	}
}

// grant 路由必须在审计白名单里 —— 不在的话中间件根本不会为它建审计行，
// 上面那条 target 测试就成了空谈。
func TestGrantRouteIsInAuditWhitelist(t *testing.T) {
	if !ShouldAuditManagementWrite(http.MethodPost, "/api/v1/wallet/grant") {
		t.Fatal("POST /api/v1/wallet/grant 不在审计白名单里")
	}
	// 反向对照：随便一个未登记的写路由不该被审计，否则白名单形同虚设。
	if ShouldAuditManagementWrite(http.MethodPost, "/api/v1/not/audited") {
		t.Fatal("未登记的路由被判为需要审计 —— 白名单没起作用")
	}
}
