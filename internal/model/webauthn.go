package model

import "time"

// WebAuthnCredential 存储用户绑定的 Passkey(WebAuthn)凭证。
//
// 一个用户可绑定多个凭证；凭证的核心数据（公钥、计数器、transports 等）以
// github.com/go-webauthn/webauthn/webauthn.Credential 的 JSON 序列化形式整体存入
// Credential 字段，登录校验时由 go-webauthn 在内存中按原始 credential id 匹配，
// 因此 DB 层无需对原始 id 建唯一索引——改用其 SHA-256 摘要做唯一约束，规避各方言
// 对长字符串唯一索引的长度限制（尤其 MySQL utf8mb4）。
type WebAuthnCredential struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	UserID          uint       `gorm:"index;not null" json:"user_id"`
	CredentialIDHex string     `gorm:"uniqueIndex;size:64;not null" json:"-"` // 原始 credential id 的 SHA-256 十六进制摘要，用于唯一约束
	Credential      string     `gorm:"type:text;not null" json:"-"`           // webauthn.Credential 的 JSON
	Name            string     `json:"name"`                                  // 用户自定义标签（如「MacBook」）
	CreatedAt       time.Time  `json:"created_at"`
	LastUsedAt      *time.Time `json:"last_used_at"`
}

func (WebAuthnCredential) TableName() string { return "webauthn_credentials" }
