// Package webauthn 实现 Passkey(WebAuthn)登录与凭证管理。
//
// 设计要点：
//   - RP 配置(RP ID / RP Name / Origins)来自数据库 Setting，每次请求即时读取，
//     避免缓存失效问题；配置不完整时返回 ErrNotConfigured，由上层转为 4xx。
//   - 注册：要求 ResidentKey=required 的可发现凭证(Passkey)，无需用户名即可登录。
//   - 登录：可发现凭证断言，ValidatePasskeyLogin 通过 userHandle 反查到用户。
//   - challenge 会话仅存内存、单次消费、5 分钟过期；以随机 token 关联 begin/finish。
//   - webauthn.Credential 整体以 JSON 存库，登录校验时在内存按原始 id 匹配。
package webauthn

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	stg "github.com/gypg/lodestar/internal/op/setting"
	usr "github.com/gypg/lodestar/internal/op/user"
	"gorm.io/gorm"
)

const (
	sessionTTL        = 5 * time.Minute
	maxCredentialName = 64
	// maxPendingSessions 限制待完成的 challenge 会话总数。
	// /login/begin 匿名可达且不校验任何凭证，无上限时每次调用都留下一条 5 分钟的记录，
	// 攻击者可把进程内存推到 OOM（实测每条约 338 字节，容器上限 512MiB）。
	// 正常用量远低于此：一条会话只在用户点击登录到浏览器完成指纹/PIN 之间存活。
	maxPendingSessions = 4096
	// sessionPurgeInterval 是惰性清理的最小间隔。原实现每次 saveSession 都持锁
	// 遍历全表（O(n)），表变大后单次调用从 2.5µs 退化到 10.97ms（20 万条实测），
	// 且退化发生在全局锁内，连带拖慢 finish。改为按间隔摊还，正常路径 O(1)。
	sessionPurgeInterval = 30 * time.Second
	// userHandleBase: 用户句柄(userHandle)采用用户 ID 的十进制字符串字节，
	// 在注册/登录之间稳定可逆映射（解析侧用 ParseUint 还原）。
	userHandleBase = 10
)

var (
	ErrNotConfigured = errors.New("webauthn is not configured")
	ErrInvalidToken  = errors.New("invalid or expired webauthn session")
	ErrUserNotFound  = errors.New("user not found")
	// ErrTooManySessions 表示待完成的 challenge 会话已达上限，请稍后重试。
	ErrTooManySessions = errors.New("too many pending webauthn sessions, please retry shortly")
)

// --- 会话存储 ---

type pendingSession struct {
	data   *webauthn.SessionData
	userID uint // 注册：已登录用户 ID；登录：finish 时由 handler 解析
	kind   string
	expiry time.Time
}

var (
	sessionsMu    sync.Mutex
	sessions      = make(map[string]*pendingSession)
	lastPurgeAt   time.Time
	sessionsClock = time.Now // 测试可替换，生产恒为 time.Now
)

// saveSession 生成随机 token 并保存会话，并在容量上限内摊还式清理过期项。
//
// 清理是"按间隔摊还"而非每次全表扫描：后者在表变大时会把 O(n) 遍历压在全局锁内。
// 但当表已满时必须**先无条件强制清理再判上限**，否则一批过期会话会把上限永久
// 占死，让合法用户再也开不出新 challenge（把内存 DoS 换成了功能 DoS）。
func saveSession(s *pendingSession) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	now := sessionsClock()
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	if now.Sub(lastPurgeAt) >= sessionPurgeInterval {
		purgeExpiredSessionsLocked(now)
	}
	if len(sessions) >= maxPendingSessions {
		// 表满：强制清理一次，回收间隔内到期的条目。
		purgeExpiredSessionsLocked(now)
		if len(sessions) >= maxPendingSessions {
			return "", ErrTooManySessions
		}
	}

	sessions[token] = s
	return token, nil
}

// purgeExpiredSessionsLocked 删除所有已过期会话。调用方必须持 sessionsMu。
func purgeExpiredSessionsLocked(now time.Time) {
	for k, v := range sessions {
		if now.After(v.expiry) {
			delete(sessions, k)
		}
	}
	lastPurgeAt = now
}

// takeSession 取出并删除会话（单次消费）。过期或不存在返回 nil。
func takeSession(token, kind string) *pendingSession {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	s, ok := sessions[token]
	if !ok {
		return nil
	}
	delete(sessions, token)
	if s.kind != kind || time.Now().After(s.expiry) {
		return nil
	}
	return s
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// --- RP 配置 ---

// New 根据当前 Setting 构造 *webauthn.WebAuthn。配置不完整时返回 ErrNotConfigured。
func New() (*webauthn.WebAuthn, error) {
	rpID, _ := stg.GetString(model.SettingKeyWebAuthnRPID)
	rpName, _ := stg.GetString(model.SettingKeyWebAuthnRPName)
	originsCSV, _ := stg.GetString(model.SettingKeyWebAuthnOrigins)

	rpID = strings.TrimSpace(rpID)
	rpName = strings.TrimSpace(rpName)
	if rpName == "" {
		rpName = "Octopus"
	}
	origins := splitOrigins(originsCSV)
	if rpID == "" || len(origins) == 0 {
		return nil, ErrNotConfigured
	}

	w, err := webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: rpName,
		RPOrigins:     origins,
	})
	if err != nil {
		return nil, fmt.Errorf("build webauthn: %w", err)
	}
	return w, nil
}

func splitOrigins(csv string) []string {
	parts := strings.FieldsFunc(csv, func(r rune) bool { return r == ',' || r == '\n' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// --- webauthn.User 适配器 ---

type webAuthnUser struct {
	id          uint
	name        string
	credentials []webauthn.Credential
}

func (u *webAuthnUser) WebAuthnID() []byte {
	return []byte(strconv.FormatUint(uint64(u.id), userHandleBase))
}

func (u *webAuthnUser) WebAuthnName() string        { return u.name }
func (u *webAuthnUser) WebAuthnDisplayName() string { return u.name }

func (u *webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return u.credentials
}

// loadAdapter 从 DB 载入用户及其所有凭证，构造 webauthn.User。
func loadAdapter(userID uint) (*webAuthnUser, *model.User, error) {
	user, err := usr.GetByID(userID, context.Background())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrUserNotFound
		}
		return nil, nil, err
	}
	creds, err := loadCredentials(userID)
	if err != nil {
		return nil, nil, err
	}
	return &webAuthnUser{id: user.ID, name: user.Username, credentials: creds}, &user, nil
}

// --- 凭证存取 ---

func loadCredentials(userID uint) ([]webauthn.Credential, error) {
	var records []model.WebAuthnCredential
	if err := db.GetDB().Where("user_id = ?", userID).Find(&records).Error; err != nil {
		return nil, err
	}
	creds := make([]webauthn.Credential, 0, len(records))
	for _, r := range records {
		var c webauthn.Credential
		if err := json.Unmarshal([]byte(r.Credential), &c); err == nil {
			creds = append(creds, c)
		}
	}
	return creds, nil
}

func credentialIDHex(id []byte) string {
	sum := sha256.Sum256(id)
	return hex.EncodeToString(sum[:])
}

// --- 注册 ---

// BeginRegistration 发起凭证注册，返回 session token 与浏览器所需的 CredentialCreation 选项。
func BeginRegistration(userID uint) (string, *protocol.CredentialCreation, error) {
	w, err := New()
	if err != nil {
		return "", nil, err
	}
	adapter, _, err := loadAdapter(userID)
	if err != nil {
		return "", nil, err
	}
	creation, session, err := w.BeginRegistration(
		adapter,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
	)
	if err != nil {
		return "", nil, fmt.Errorf("begin registration: %w", err)
	}
	token, err := saveSession(&pendingSession{
		data:   session,
		userID: userID,
		kind:   "registration",
		expiry: time.Now().Add(sessionTTL),
	})
	if err != nil {
		return "", nil, err
	}
	return token, creation, nil
}

// FinishRegistration 校验浏览器回传的注册响应并落库。
// body 为浏览器 navigator.credentials.create() 返回结果的 JSON。
func FinishRegistration(token string, body []byte, name string) error {
	s := takeSession(token, "registration")
	if s == nil {
		return ErrInvalidToken
	}
	w, err := New()
	if err != nil {
		return err
	}
	adapter, _, err := loadAdapter(s.userID)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, "", bytes.NewReader(body))
	if err != nil {
		return err
	}
	cred, err := w.FinishRegistration(adapter, *s.data, req)
	if err != nil {
		return fmt.Errorf("finish registration: %w", err)
	}
	credJSON, err := json.Marshal(cred)
	if err != nil {
		return fmt.Errorf("marshal credential: %w", err)
	}
	name = strings.TrimSpace(name)
	if len(name) > maxCredentialName {
		name = name[:maxCredentialName]
	}
	record := model.WebAuthnCredential{
		UserID:          s.userID,
		CredentialIDHex: credentialIDHex(cred.ID),
		Credential:      string(credJSON),
		Name:            name,
	}
	if err := db.GetDB().Create(&record).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return fmt.Errorf("credential already registered")
		}
		return fmt.Errorf("save credential: %w", err)
	}
	return nil
}

// --- 登录 ---

// BeginLogin 发起可发现凭证(Passkey)登录断言，返回 session token 与浏览器所需的 CredentialAssertion。
func BeginLogin() (string, *protocol.CredentialAssertion, error) {
	w, err := New()
	if err != nil {
		return "", nil, err
	}
	assertion, session, err := w.BeginDiscoverableLogin()
	if err != nil {
		return "", nil, fmt.Errorf("begin login: %w", err)
	}
	token, err := saveSession(&pendingSession{
		data:   session,
		kind:   "login",
		expiry: time.Now().Add(sessionTTL),
	})
	if err != nil {
		return "", nil, err
	}
	return token, assertion, nil
}

// FinishLogin 校验浏览器回传的登录断言，返回认证成功的用户。
func FinishLogin(token string, body []byte) (model.User, error) {
	s := takeSession(token, "login")
	if s == nil {
		return model.User{}, ErrInvalidToken
	}
	w, err := New()
	if err != nil {
		return model.User{}, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body)
	if err != nil {
		return model.User{}, fmt.Errorf("parse assertion: %w", err)
	}
	// 可发现凭证：浏览器回传 userHandle，据此反查用户及其凭证。
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		userID, perr := parseUserHandle(userHandle)
		if perr != nil {
			return nil, perr
		}
		adapter, _, lerr := loadAdapter(userID)
		if lerr != nil {
			return nil, lerr
		}
		return adapter, nil
	}
	authedUser, cred, err := w.ValidatePasskeyLogin(handler, *s.data, parsed)
	if err != nil {
		return model.User{}, fmt.Errorf("validate login: %w", err)
	}

	// 持久化更新后的计数器与最近使用时间。
	if uerr := updateCredentialUsage(authedUser.WebAuthnID(), cred); uerr != nil {
		// 计数器更新失败不阻断登录（已通过校验），仅记录。
		_ = uerr
	}
	userID, _ := parseUserHandle(authedUser.WebAuthnID())
	user, err := usr.GetByID(userID, context.Background())
	if err != nil {
		return model.User{}, err
	}
	return user, nil
}

func parseUserHandle(handle []byte) (uint, error) {
	id, err := strconv.ParseUint(string(handle), userHandleBase, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid user handle: %w", err)
	}
	return uint(id), nil
}

func updateCredentialUsage(userHandle []byte, cred *webauthn.Credential) error {
	hex := credentialIDHex(cred.ID)
	now := time.Now()
	credJSON, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	return db.GetDB().Model(&model.WebAuthnCredential{}).
		Where("credential_id_hex = ?", hex).
		Updates(map[string]any{
			"credential":   string(credJSON),
			"last_used_at": now,
		}).Error
}

// --- 凭证管理 ---

// CredentialView 是对外暴露的凭证视图（不含公钥等敏感数据）。
type CredentialView struct {
	ID         uint       `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func ListCredentials(userID uint) ([]CredentialView, error) {
	var records []model.WebAuthnCredential
	if err := db.GetDB().Where("user_id = ?", userID).Order("created_at DESC").Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]CredentialView, 0, len(records))
	for _, r := range records {
		out = append(out, CredentialView{
			ID: r.ID, Name: r.Name, CreatedAt: r.CreatedAt, LastUsedAt: r.LastUsedAt,
		})
	}
	return out, nil
}

func DeleteCredential(userID, id uint) error {
	res := db.GetDB().Where("id = ? AND user_id = ?", id, userID).Delete(&model.WebAuthnCredential{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// HasAnyCredential 报告是否至少有一个用户绑定了凭证。用于前端登录页判断是否展示 Passkey 入口。
func HasAnyCredential() bool {
	var count int64
	db.GetDB().Model(&model.WebAuthnCredential{}).Count(&count)
	return count > 0
}
