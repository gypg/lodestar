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

var codes sync.Map // email -> codeEntry

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
	e := normalize(email)
	if e == "" || !strings.Contains(e, "@") {
		return errors.New("邮箱格式无效")
	}
	code := gen6()
	codes.Store(e, codeEntry{code: code, exp: time.Now().Add(10 * time.Minute)})
	return sendMail(e, "Lodestar 邮箱验证码", "你的验证码是："+code+"，10 分钟内有效。如非本人操作请忽略。")
}

// Verify checks (and consumes) the code for an email.
func Verify(email, code string) bool {
	e := normalize(email)
	v, ok := codes.Load(e)
	if !ok {
		return false
	}
	entry := v.(codeEntry)
	if time.Now().After(entry.exp) {
		codes.Delete(e)
		return false
	}
	if entry.code != strings.TrimSpace(code) {
		return false
	}
	codes.Delete(e)
	return true
}

// SendTest sends a test email to verify SMTP config.
func SendTest(to string) error {
	return sendMail(to, "Lodestar 测试邮件", "这是一封来自 Lodestar 的测试邮件，说明 SMTP 配置正常。")
}
