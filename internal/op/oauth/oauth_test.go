package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetUserInfo_LimitsSuccessBodyDecode verifies the 200 (success) path caps
// JSON decoding at maxGitHubUserInfoBytes. A valid JSON object larger than the
// cap is truncated by LimitReader before json.Decode sees the closing brace,
// so decoding fails instead of consuming unbounded memory.
//
// 变异自检（去掉 decoder 的 LimitReader）：超大有效 JSON 会被完整解码，
// getUserInfo 返回成功而非 error，断言"want decode error"变红。**抓得到**。
func TestGetUserInfo_LimitsSuccessBodyDecode(t *testing.T) {
	// 构造一个超大的"有效 JSON"——有起始 { 和 "login" 字段，但 value 是
	// 2 MiB 字符串，远超 1 MiB 上限。LimitReader 会在 1 MiB 处截断，
	// json.Decode 拿到的是不完整 JSON → 报错。无 LimitReader 时会完整解码。
	hugeValue := strings.Repeat("a", 2<<20)
	body := `{"id":12345,"login":"octocat","name":"` + hugeValue + `"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	oldURL := githubUserInfoURL
	githubUserInfoURL = srv.URL
	t.Cleanup(func() { githubUserInfoURL = oldURL })

	_, err := getUserInfo(context.Background(), "fake-token")
	if err == nil {
		t.Fatal("getUserInfo: want decode error for body exceeding maxGitHubUserInfoBytes, got nil (LimitReader may be missing on the decode path)")
	}
}

// TestGetUserInfo_LimitsErrorBodyRead verifies the non-200 (error) path caps
// body reads at maxGitHubErrorBodyBytes. A 1 MiB error body must not cause
// unbounded memory growth; the read is capped and the body is then truncated
// to 500 chars in the error message.
//
// 行为断言：函数对超大错误体仍快速返回错误，不挂起。注意：删掉错误路径的
// LimitReader 后，io.ReadAll 读 1 MiB 仍很快、仍返回 error，所以这个测试
// 对"是否限流读取"是弱断言——真正的限流防护意义在防止"超大体"（如 GiB 级）
// 撑爆内存，而测试无法构造 GiB 级且不拖慢 CI。更强断言见
// TestGetUserInfo_LimitsSuccessBodyDecode（成功路径的 LimitReader 可被
// 变异自检抓到）。错误路径的限流是同源修复，沿用同一常量族。
func TestGetUserInfo_LimitsErrorBodyRead(t *testing.T) {
	hugeBody := strings.Repeat("x", 1<<20) // 1 MiB, 远超 4 KiB 上限
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(hugeBody))
	}))
	defer srv.Close()

	oldURL := githubUserInfoURL
	githubUserInfoURL = srv.URL
	t.Cleanup(func() { githubUserInfoURL = oldURL })

	_, err := getUserInfo(context.Background(), "fake-token")
	if err == nil {
		t.Fatal("getUserInfo: want error for non-200 status, got nil")
	}
}

// TestGetUserInfo_NormalErrorBodyStillReported is a baseline guard: a small,
// legitimate error body is still surfaced in the error message (truncated to 500).
func TestGetUserInfo_NormalErrorBodyStillReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("Bad credentials"))
	}))
	defer srv.Close()

	oldURL := githubUserInfoURL
	githubUserInfoURL = srv.URL
	t.Cleanup(func() { githubUserInfoURL = oldURL })

	_, err := getUserInfo(context.Background(), "fake-token")
	if err == nil {
		t.Fatal("getUserInfo: want error for 401, got nil")
	}
	if !strings.Contains(err.Error(), "Bad credentials") {
		t.Fatalf("error message should contain upstream body, got: %v", err)
	}
}

// TestGetUserInfo_ValidUser_Succeeds is a baseline: a normal valid user object
// decodes correctly (confirms the LimitReader on the success path doesn't break
// legitimate traffic — the body is well under the 1 MiB cap).
func TestGetUserInfo_ValidUser_Succeeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":12345,"login":"octocat","name":"The Octocat","email":"octo@cat.com"}`))
	}))
	defer srv.Close()

	oldURL := githubUserInfoURL
	githubUserInfoURL = srv.URL
	t.Cleanup(func() { githubUserInfoURL = oldURL })

	u, err := getUserInfo(context.Background(), "fake-token")
	if err != nil {
		t.Fatalf("getUserInfo: want success, got error: %v", err)
	}
	if u.Username != "octocat" {
		t.Fatalf("Username: want octocat, got %q", u.Username)
	}
	if u.ProviderUserID != "12345" {
		t.Fatalf("ProviderUserID: want 12345, got %q", u.ProviderUserID)
	}
}
