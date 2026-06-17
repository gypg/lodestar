package webauthn

import (
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/lingyuins/octopus/internal/model"
	stg "github.com/lingyuins/octopus/internal/op/setting"
)

func setSetting(t *testing.T, key model.SettingKey, value string) {
	t.Helper()
	stg.GetCache().Set(key, value)
}

func TestSplitOrigins(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"https://a.com", []string{"https://a.com"}},
		{"https://a.com, https://b.com", []string{"https://a.com", "https://b.com"}},
		{" https://a.com \n, https://b.com ", []string{"https://a.com", "https://b.com"}},
	}
	for _, c := range cases {
		got := splitOrigins(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitOrigins(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitOrigins(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestNew_NotConfigured(t *testing.T) {
	setSetting(t, model.SettingKeyWebAuthnRPID, "")
	setSetting(t, model.SettingKeyWebAuthnOrigins, "")
	if _, err := New(); !errorIs(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}

func TestNew_Configured(t *testing.T) {
	setSetting(t, model.SettingKeyWebAuthnRPID, "example.com")
	setSetting(t, model.SettingKeyWebAuthnRPName, "Octopus")
	setSetting(t, model.SettingKeyWebAuthnOrigins, "https://example.com")
	w, err := New()
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	if w == nil {
		t.Fatal("New returned nil webauthn")
	}
}

func TestSessionStore_TakeConsumesAndValidatesKind(t *testing.T) {
	// takeSession 是一次性消费：任何一次取用都会使 token 失效（防重放）。
	tokWrong, err := saveSession(&pendingSession{data: &webauthn.SessionData{}, userID: 1, kind: "registration", expiry: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatalf("saveSession: %v", err)
	}
	if takeSession(tokWrong, "login") != nil {
		t.Error("takeSession with wrong kind should return nil")
	}
	// 错误 kind 的取用已消费该 token，再次取用应为 nil。
	if takeSession(tokWrong, "registration") != nil {
		t.Error("session should be consumed after a take")
	}

	// 正确 kind 的一次性消费。
	tokOK, _ := saveSession(&pendingSession{data: &webauthn.SessionData{}, userID: 2, kind: "login", expiry: time.Now().Add(time.Minute)})
	if s := takeSession(tokOK, "login"); s == nil {
		t.Error("takeSession with correct kind should return the session")
	} else if s.userID != 2 {
		t.Errorf("unexpected userID %d", s.userID)
	}
	if takeSession(tokOK, "login") != nil {
		t.Error("session should be consumed after a successful take")
	}
}

func TestSessionStore_Expired(t *testing.T) {
	tok, _ := saveSession(&pendingSession{data: &webauthn.SessionData{}, kind: "login", expiry: time.Now().Add(-time.Second)})
	if takeSession(tok, "login") != nil {
		t.Error("expired session should not be returned")
	}
}

func TestParseUserHandle_RoundTrip(t *testing.T) {
	for _, id := range []uint{1, 42, 999999} {
		handle := []byte(itoa(id))
		got, err := parseUserHandle(handle)
		if err != nil {
			t.Fatalf("parseUserHandle(%d): %v", id, err)
		}
		if got != id {
			t.Errorf("parseUserHandle round-trip %d -> %d", id, got)
		}
	}
	if _, err := parseUserHandle([]byte("not-a-number")); err == nil {
		t.Error("parseUserHandle should error on non-numeric input")
	}
}

func TestCredentialIDHex_Deterministic(t *testing.T) {
	a := credentialIDHex([]byte{1, 2, 3})
	b := credentialIDHex([]byte{1, 2, 3})
	c := credentialIDHex([]byte{3, 2, 1})
	if a != b {
		t.Error("credentialIDHex should be deterministic")
	}
	if a == c {
		t.Error("credentialIDHex should differ for different inputs")
	}
}

func itoa(id uint) string {
	// mirror WebAuthnID encoding (decimal) without importing strconv twice
	if id == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for id > 0 {
		i--
		buf[i] = byte('0' + id%10)
		id /= 10
	}
	return string(buf[i:])
}

func errorIs(err, target error) bool {
	return err == target
}
