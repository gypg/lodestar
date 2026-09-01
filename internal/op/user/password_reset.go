package user

/*
WO-026 阶段 B — 客户侧忘记密码（自助重置）。

三步流程（业界标准，不自创）：提交邮箱 → 一次性码发到邮箱 → 邮箱+码+新密码改密。
码的机制全部复用 internal/op/email（crypto/rand 6 位、消费即删、15 分钟 TTL、
与注册码 namespace 隔离）——本文件只做三件事：

 1. 枚举防护：邮箱不存在时与存在时**可观测行为完全一致**（都静默成功、都不发邮件）。
    任何差异——错误、延迟特征、日志回显——都会让这个端点变成"哪些邮箱注册过"的探测器。
 2. 强度闸：新密码走与注册同一条 validatePasswordStrength（最小 12 字符）。
    校验放在码校验**之前**，弱密码拒绝不烧码（合法用户输错新密码还能用原码重试）。
 3. 改密走 ChangePassword 同款路径（bcrypt + adminCache 同步）。
*/

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/email"
)

// ErrInvalidResetCode 是验证码环节的统一错误文案。**不得**区分"码错误"与
// "邮箱不存在"——那两个分支在这里自然合流：码按 (namespace, 邮箱) 存取，
// 不存在的邮箱根本没有码可验，落到同一个 false。
var ErrInvalidResetCode = errors.New("验证码错误或已过期")

// RequestPasswordReset 为已注册邮箱发送密码重置码；邮箱不存在时静默成功。
//
// 返回值刻意不带任何"邮箱是否存在"的信息（调用方 handler 对一切输入都返回同一个
// 成功响应）。SMTP 未配置或发送失败同样静默——发送是尽力而为，失败记不上是运维
// 问题，不是调用方可观测的差异。码的有效期与一次性语义在 email 包内。
func RequestPasswordReset(addr string, ctx context.Context) error {
	e := strings.ToLower(strings.TrimSpace(addr))
	if e == "" || !strings.Contains(e, "@") {
		return nil // 与"不存在"同一路径：静默成功，无格式探针
	}
	var count int64
	if err := db.GetDB().WithContext(ctx).
		Model(&model.User{}).
		Where("email = ?", e).
		Count(&count).Error; err != nil {
		// DB 故障时也不能向调用方暴露差异——统一静默。真故障由日志/监控发现。
		return nil
	}
	if count == 0 {
		return nil // 枚举防护的核心分支：不存在 = 什么都不做，返回 nil
	}
	// 发送失败忽略：码已入库，用户可重新请求（新码覆盖旧码）；响应保持一致。
	_ = email.GenerateAndSendPasswordReset(e)
	return nil
}

// ResetPassword 用一次性码完成密码重置。
//
// 顺序有讲究：强度校验 → 码校验（消费即删）→ 改密。弱密码在烧码之前被拒，
// 合法用户输错新密码后原码仍可用；码一旦校验通过即被消费，重放无效（T-B2）。
// 同邮箱对应多个账号（历史数据）时取 id 最小的账号——写邮件地址的唯一性不是
// DB 约束，这个 corner 的完整治理超出本工单，已在回执说明。
func ResetPassword(addr, code, newPassword string, ctx context.Context) error {
	e := strings.ToLower(strings.TrimSpace(addr))

	// 1. 强度闸（不烧码）。
	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}

	// 2. 码校验（一次性）。不存在的邮箱没有码，与错码同文案。
	if !email.VerifyPasswordReset(e, code) {
		return ErrInvalidResetCode
	}

	// 3. 改密（ChangePassword 同款：bcrypt + adminCache 同步）。
	var u model.User
	if err := db.GetDB().WithContext(ctx).
		Where("email = ?", e).
		Order("id ASC").
		First(&u).Error; err != nil {
		return fmt.Errorf("user not found: %w", err)
	}
	u.Password = newPassword
	if err := u.HashPassword(); err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}
	if err := db.GetDB().WithContext(ctx).Model(&u).Update("password", u.Password).Error; err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}
	if adminCache.ID == u.ID {
		adminCache.Password = u.Password
	}
	return nil
}
