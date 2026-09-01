package user

/*
WO-026 阶段 B：忘记密码（客户侧自助重置）。

三条工单硬要求在这里落死：
  - T-B4 枚举防护：邮箱不存在时 RequestPasswordReset 必须与存在时返回完全相同的结果
    （nil error），否则这个端点就是"哪些邮箱注册过"的探测器。
  - 码一次性：email.Verify 消费即删，ResetPassword 依赖它；本测试钉住"同一码第二次必败"。
  - 密码强度：复用注册路径的最小长度 12（minInitialAdminPasswordLength），弱密码必须被拒。

email 包的码带 namespace 前缀（reg:/reset:）：注册验证码绝不能当重置码用，
反之亦然 —— 否则一条泄漏的注册码就成了改密凭据。
*/

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/email"
)

func setupPasswordResetTest(t *testing.T) (real, absent string) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	u := model.User{
		Username: "resetee-" + t.Name(),
		Password: "password123456",
		Role:     model.UserRoleUser,
		Email:    "resetee@example.com",
		Quota:    1,
	}
	if err := u.HashPassword(); err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	// Verify() 要求 Ready()（adminCache 非空）。把测试用户塞进缓存即可，
	// 顺带让 ResetPassword 的 adminCache 同步分支（管理员改密场景）被真实覆盖。
	SetCache(u)
	return "resetee@example.com", "ghost@example.com"
}

// TestRequestPasswordResetEnumerationSafe 钉死 T-B4：存在的邮箱与不存在的邮箱，
// RequestPasswordReset 的**可观测返回**必须一致（都是 nil）。
// 邮件发不出（SMTP 未配置）也一样静默 —— 否则错误差异照样能探测邮箱存在性。
func TestRequestPasswordResetEnumerationSafe(t *testing.T) {
	realEmail, absentEmail := setupPasswordResetTest(t)

	if err := RequestPasswordReset(realEmail, context.Background()); err != nil {
		t.Fatalf("existing email: RequestPasswordReset returned error %v — that difference is "+
			"the enumeration oracle (absent email must behave identically)", err)
	}
	if err := RequestPasswordReset(absentEmail, context.Background()); err != nil {
		t.Fatalf("absent email: RequestPasswordReset returned error %v — must be silent, identical "+
			"to the existing-email path (T-B4)", err)
	}
}

// TestRequestPasswordResetInvalidEmail 钉死输入校验：连"邮箱格式无效"都不许报 ——
// 报了就给攻击者一个免 SMTP 的快速探针（合法格式 vs 非法格式的响应差异）。
// 格式无效的处理与不存在一致：静默成功。
func TestRequestPasswordResetInvalidEmailSilent(t *testing.T) {
	_, _ = setupPasswordResetTest(t)
	if err := RequestPasswordReset("not-an-email", context.Background()); err != nil {
		t.Fatalf("invalid-format email must be silently accepted like an absent one (no format "+
			"oracle), got error: %v", err)
	}
}

// TestResetPasswordFullFlow 钉死 T-B1 的 op 层部分：请求 → 校验码 → 改密成功 →
// 新密码能登录、旧密码不能。
func TestResetPasswordFullFlow(t *testing.T) {
	realEmail, _ := setupPasswordResetTest(t)
	ctx := context.Background()

	// 直接种一枚码（绕过 SMTP 发送；发送链路由 handler 层测）。
	email.SeedCodeForTest(t, email.NamespaceReset, realEmail, "654321")

	if err := ResetPassword(realEmail, "654321", "newPassword888", ctx); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}

	// 新密码能登录（bcrypt 校验通过）。
	u, err := Verify("resetee-"+t.Name(), "newPassword888")
	if err != nil {
		t.Fatalf("login with new password failed: %v", err)
	}
	if u.Email != realEmail {
		t.Fatalf("verified user email = %q, want %q", u.Email, realEmail)
	}
	// 旧密码不能登录。
	if _, err := Verify("resetee-"+t.Name(), "password123456"); err == nil {
		t.Fatal("login with OLD password succeeded — reset did not take effect")
	}
}

// TestResetPasswordCodeSingleUse 钉死 T-B2：同一个码第二次使用必须失败。
func TestResetPasswordCodeSingleUse(t *testing.T) {
	realEmail, _ := setupPasswordResetTest(t)
	ctx := context.Background()
	email.SeedCodeForTest(t, email.NamespaceReset, realEmail, "111222")

	if err := ResetPassword(realEmail, "111222", "newPassword888", ctx); err != nil {
		t.Fatalf("first use: %v", err)
	}
	// 第二次：码已被消费。
	err := ResetPassword(realEmail, "111222", "anotherPass999", ctx)
	if err == nil {
		t.Fatal("second use of the same code must fail (one-time semantics)")
	}
}

// TestResetPasswordWeakPasswordRejected 钉死强度校验：少于 12 字符的新密码必须被拒，
// 且**码不被消费**（拒在改密之前，不给攻击者烧码探测的机会）。
func TestResetPasswordWeakPasswordRejected(t *testing.T) {
	realEmail, _ := setupPasswordResetTest(t)
	ctx := context.Background()
	email.SeedCodeForTest(t, email.NamespaceReset, realEmail, "333444")

	if err := ResetPassword(realEmail, "333444", "short", ctx); err == nil {
		t.Fatal("weak password (short) must be rejected")
	}
	// 码仍有效：强度校验失败不应烧码（合法用户只是输错了新密码）。
	if err := ResetPassword(realEmail, "333444", "strongPassword123", ctx); err != nil {
		t.Fatalf("same code should still work after a weak-password rejection: %v", err)
	}
}

// TestResetPasswordWrongCodeRejected 错码必须失败。
func TestResetPasswordWrongCodeRejected(t *testing.T) {
	realEmail, _ := setupPasswordResetTest(t)
	email.SeedCodeForTest(t, email.NamespaceReset, realEmail, "777888")

	if err := ResetPassword(realEmail, "000000", "strongPassword123", context.Background()); err == nil {
		t.Fatal("wrong code must be rejected")
	}
}

// TestResetPasswordExpiredCodeRejected 钉死 T-B3：过期的码必须失败。
func TestResetPasswordExpiredCodeRejected(t *testing.T) {
	realEmail, _ := setupPasswordResetTest(t)
	email.SeedExpiredCodeForTest(t, email.NamespaceReset, realEmail, "555666")

	if err := ResetPassword(realEmail, "555666", "strongPassword123", context.Background()); err == nil {
		t.Fatal("expired code must be rejected (T-B3)")
	}
}

// TestResetPasswordNamespaceIsolation 钉死 namespace 隔离：注册码（reg:）绝不能用于改密。
func TestResetPasswordNamespaceIsolation(t *testing.T) {
	realEmail, _ := setupPasswordResetTest(t)
	email.SeedCodeForTest(t, email.NamespaceRegister, realEmail, "999000")

	if err := ResetPassword(realEmail, "999000", "strongPassword123", context.Background()); err == nil {
		t.Fatal("a register-namespace code must NOT authorize a password reset")
	}
}
