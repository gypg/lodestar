package email

/*
Lodestar — SMTP 邮件 + 邮箱验证码。

配置驱动（管理员在后台填 SMTP 凭据，对齐易支付做法——构建无需凭据）。验证码存内存
（10 分钟 TTL，短时有效；单节点足够，重启失效可接受）。用 net/smtp（587 STARTTLS 兼容）。
*/

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
)

type codeEntry struct {
	code string
	exp  time.Time
}

var codes sync.Map // key -> codeEntry；key = namespace + 邮箱（见 Namespace*）

// 验证码 namespace。注册码与密码重置码必须隔离：一条泄漏的注册码绝不能当改密
// 凭据用（反之亦然）。key 形如 "reset:user@example.com"。
const (
	NamespaceRegister = "reg:"
	NamespaceReset    = "reset:"
)

// resetCodeTTL 是密码重置码的有效期。工单建议 ≤15 分钟（与 JWT 默认有效期一致）。
const resetCodeTTL = 15 * time.Minute

func get(k model.SettingKey) string {
	v, _ := setting.GetString(k)
	return strings.TrimSpace(v)
}

func cfg() (host, port, user, pass, from string, ok bool) {
	if e, _ := setting.GetBool(model.SettingKeySMTPEnabled); !e {
		return "", "", "", "", "", false
	}
	host = get(model.SettingKeySMTPHost)
	port = get(model.SettingKeySMTPPort)
	user = get(model.SettingKeySMTPUser)
	pass = get(model.SettingKeySMTPPass)
	from = get(model.SettingKeySMTPFrom)
	if host == "" || from == "" {
		return "", "", "", "", "", false
	}
	if port == "" {
		port = "587"
	}
	return host, port, user, pass, from, true
}

// Configured reports whether SMTP is ready (for the frontend).
func Configured() bool {
	_, _, _, _, _, ok := cfg()
	return ok
}

func sendMail(to, subject, body string) error {
	host, port, user, pass, from, ok := cfg()
	if !ok {
		return errors.New("SMTP 未配置")
	}
	msg := []byte("To: " + to + "\r\n" +
		"From: " + from + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n" +
		body)
	var auth smtp.Auth
	if user != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}
	return smtp.SendMail(host+":"+port, auth, from, []string{to}, msg)
}

func gen6() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "000000"
	}
	return fmt.Sprintf("%06d", n.Int64())
}

func normalize(email string) string { return strings.TrimSpace(strings.ToLower(email)) }

// GenerateAndSend creates a 6-digit code for the email and sends it.
func GenerateAndSend(email string) error {
	return generateAndSend(NamespaceRegister, email, "Lodestar 邮箱验证码",
		func(code string) string {
			return "你的验证码是：" + code + "，10 分钟内有效。如非本人操作请忽略。"
		}, 10*time.Minute)
}

// GenerateAndSendPasswordReset 为密码重置生成并发送验证码。
// 与注册码同机制（crypto/rand 6 位、消费即删）但 namespace 隔离，TTL 15 分钟。
func GenerateAndSendPasswordReset(email string) error {
	return generateAndSend(NamespaceReset, email, "Lodestar 密码重置",
		func(code string) string {
			return "你的密码重置验证码是：" + code + "，15 分钟内有效。如非本人操作请忽略此邮件" +
				"——你的密码不会被改动。"
		}, resetCodeTTL)
}

func generateAndSend(ns, email, subject string, body func(string) string, ttl time.Duration) error {
	e := normalize(email)
	if e == "" || !strings.Contains(e, "@") {
		return errors.New("邮箱格式无效")
	}
	code := gen6()
	codes.Store(ns+e, codeEntry{code: code, exp: time.Now().Add(ttl)})
	return sendMail(e, subject, body(code))
}

// Verify checks (and consumes) the code for an email.
func Verify(email, code string) bool {
	return verify(NamespaceRegister, email, code)
}

// VerifyPasswordReset 校验（并消费）密码重置码。一次性：成功即删。
func VerifyPasswordReset(email, code string) bool {
	return verify(NamespaceReset, email, code)
}

func verify(ns, email, code string) bool {
	e := normalize(email)
	v, ok := codes.Load(ns + e)
	if !ok {
		return false
	}
	entry := v.(codeEntry)
	if time.Now().After(entry.exp) {
		codes.Delete(ns + e)
		return false
	}
	if entry.code != strings.TrimSpace(code) {
		return false
	}
	codes.Delete(ns + e)
	return true
}

// SeedCodeForTest / SeedExpiredCodeForTest 是**仅供测试**的种子钩子：绕过 SMTP
// 直接种一枚码，让上层（op/user、handlers）能对"拿到码之后"的流程做确定性测试。
// 生产代码不得调用。与 generateAndSend 相同的 key 规则（ns+normalize(email)）。
func SeedCodeForTest(t interface {
	Helper()
	Cleanup(func())
}, ns, email, code string) {
	t.Helper()
	codes.Store(ns+normalize(email), codeEntry{code: code, exp: time.Now().Add(resetCodeTTL)})
}

func SeedExpiredCodeForTest(t interface {
	Helper()
	Cleanup(func())
}, ns, email, code string) {
	t.Helper()
	codes.Store(ns+normalize(email), codeEntry{code: code, exp: time.Now().Add(-time.Minute)})
}

// SendTest sends a test email to verify SMTP config.
func SendTest(to string) error {
	return sendMail(to, "Lodestar 测试邮件", "这是一封来自 Lodestar 的测试邮件，说明 SMTP 配置正常。")
}
