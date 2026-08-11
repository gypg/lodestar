package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/model"
)

// NewAPI / OneAPI / OneHub 的 /api/user/login 成功时 data 是用户对象，
// 会话凭据只在 Set-Cookie 里。此前 resolveManagedAccessToken 只看 body，
// 用户名密码账号必然报 login response missing access token（用户报告 #2）。
func TestResolveManagedAccessTokenFallsBackToLoginCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/user/login" {
			http.NotFound(w, r)
			return
		}
		// 真实 NewAPI 的形状：data 是用户对象，没有任何 token 字段。
		w.Header().Add("Set-Cookie", "session=MTc1NTAwMDAwMHxabXBn; Path=/; HttpOnly")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"id":11494,"username":"managed-user","role":1}}`))
	}))
	defer server.Close()

	siteRecord := &model.Site{BaseURL: server.URL, Platform: model.SitePlatformNewAPI}
	account := &model.SiteAccount{
		CredentialType: model.SiteCredentialTypeUsernamePassword,
		Username:       "managed-user",
		Password:       "managed-pass",
	}

	token, err := resolveManagedAccessToken(context.Background(), siteRecord, account)
	if err != nil {
		t.Fatalf("expected cookie-only login to succeed, got error: %v", err)
	}
	if !strings.Contains(token, "session=MTc1NTAwMDAwMHxabXBn") {
		t.Fatalf("expected the session cookie to be returned as the credential, got %q", token)
	}
	// 必须是 cookie 形状，否则下游 buildManagedAuthHeaders 会当成 Bearer 发。
	if !looksLikeCookieToken(token) {
		t.Fatalf("returned credential %q is not recognized as a cookie, downstream will send it as Bearer", token)
	}
}

// 登录成功后拿到的 cookie 必须真的以 Cookie 头发出去，而不是 Bearer。
func TestManagedCookieCredentialIsSentAsCookieHeader(t *testing.T) {
	observedCookie := ""
	observedAuth := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/login":
			w.Header().Add("Set-Cookie", "session=abc123; Path=/; HttpOnly")
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":7,"username":"u"}}`))
		case "/api/user/self":
			observedCookie = r.Header.Get("Cookie")
			observedAuth = r.Header.Get("Authorization")
			if observedCookie == "" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"unauthorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":7,"username":"u"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	siteRecord := &model.Site{BaseURL: server.URL, Platform: model.SitePlatformNewAPI}
	account := &model.SiteAccount{
		CredentialType: model.SiteCredentialTypeUsernamePassword,
		Username:       "u",
		Password:       "p",
	}

	token, err := resolveManagedAccessToken(context.Background(), siteRecord, account)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	if _, err := requestJSONWithManagedAccessToken(context.Background(), siteRecord, http.MethodGet, buildSiteURL(server.URL, "/api/user/self"), nil, token, account); err != nil {
		t.Fatalf("authenticated request with cookie credential failed: %v", err)
	}
	if !strings.Contains(observedCookie, "session=abc123") {
		t.Fatalf("expected Cookie header to carry the session, got Cookie=%q Authorization=%q", observedCookie, observedAuth)
	}
}

// 密码错误时仍必须报登录失败，且带上上游文案 —— cookie 回落不能把失败洗成成功。
func TestResolveManagedAccessTokenStillFailsOnBadPassword(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 即使失败响应也带 Set-Cookie（真实站点常有），不能被当成凭据。
		w.Header().Add("Set-Cookie", "session=deadbeef; Path=/")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":false,"message":"用户名或密码错误","data":null}`))
	}))
	defer server.Close()

	siteRecord := &model.Site{BaseURL: server.URL, Platform: model.SitePlatformNewAPI}
	account := &model.SiteAccount{
		CredentialType: model.SiteCredentialTypeUsernamePassword,
		Username:       "u",
		Password:       "wrong",
	}

	token, err := resolveManagedAccessToken(context.Background(), siteRecord, account)
	if err == nil {
		t.Fatalf("expected failure on bad password, got token %q", token)
	}
	if !strings.Contains(err.Error(), "用户名或密码错误") {
		t.Fatalf("expected upstream message to surface, got: %v", err)
	}
}

// 既没有 token 字段也没有 Set-Cookie 时，仍要报 token missing 而不是静默成功。
func TestResolveManagedAccessTokenFailsWithoutTokenOrCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":7,"username":"u"}}`))
	}))
	defer server.Close()

	siteRecord := &model.Site{BaseURL: server.URL, Platform: model.SitePlatformNewAPI}
	account := &model.SiteAccount{
		CredentialType: model.SiteCredentialTypeUsernamePassword,
		Username:       "u",
		Password:       "p",
	}

	if token, err := resolveManagedAccessToken(context.Background(), siteRecord, account); err == nil {
		t.Fatalf("expected token-missing error when neither body token nor cookie exists, got %q", token)
	}
}

// body 里确实带 token 时，优先用 token，不能被 cookie 覆盖。
func TestResolveManagedAccessTokenPrefersBodyTokenOverCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "session=should-not-win; Path=/")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"access_token":"real-token-123"}}`))
	}))
	defer server.Close()

	siteRecord := &model.Site{BaseURL: server.URL, Platform: model.SitePlatformNewAPI}
	account := &model.SiteAccount{
		CredentialType: model.SiteCredentialTypeUsernamePassword,
		Username:       "u",
		Password:       "p",
	}

	token, err := resolveManagedAccessToken(context.Background(), siteRecord, account)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if token != "real-token-123" {
		t.Fatalf("expected body access_token to win, got %q", token)
	}
}
