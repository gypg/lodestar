package update

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/conf"
)

func TestDoRequest_AllowsLargeDownloadWithoutLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("a", maxUpdateAPIResponseBytes+1024))
	}))
	defer server.Close()

	data, err := doRequest(server.URL, false, 0, "")
	if err != nil {
		t.Fatalf("doRequest() unexpected error: %v", err)
	}

	if got, want := len(data), maxUpdateAPIResponseBytes+1024; got != want {
		t.Fatalf("download size = %d, want %d", got, want)
	}
}

func TestDoRequest_RejectsOversizedAPIResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("b", maxUpdateAPIResponseBytes+1))
	}))
	defer server.Close()

	_, err := doRequest(server.URL, false, maxUpdateAPIResponseBytes, "update API response")
	if err == nil {
		t.Fatal("doRequest() error = nil, want oversized response error")
	}

	want := fmt.Sprintf("update API response exceeds %d bytes limit", maxUpdateAPIResponseBytes)
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestGetLatestInfo_NotFoundMeansNoRelease 验证：GitHub API 在仓库无 release 时
// 返回 404 + {"message":"Not Found"}，应被解释为"暂无版本"（空结果 + nil error），
// 而非抛错导致设置页 500。回归 bug：仓库改名后无 release 时曾因此每次报错。
func TestGetLatestInfo_NotFoundMeansNoRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"Not Found"}`)
	}))
	defer server.Close()

	orig := conf.AppConfig.External.UpdateAPIURL
	conf.AppConfig.External.UpdateAPIURL = server.URL
	defer func() { conf.AppConfig.External.UpdateAPIURL = orig }()

	info, err := GetLatestInfo()
	if err != nil {
		t.Fatalf("GetLatestInfo() on Not Found: got error %v, want nil (no-release is not an error)", err)
	}
	if info == nil {
		t.Fatal("GetLatestInfo() on Not Found: got nil *LatestInfo, want non-nil empty struct")
	}
	if info.TagName != "" {
		t.Errorf("TagName = %q, want empty for no-release repo", info.TagName)
	}
}

// TestGetLatestInfo_RealAPIErrorStillReturned 验证：非 "Not Found" 的 message
// （如 rate limit）仍按错误返回，不被静默吞掉。
func TestGetLatestInfo_RealAPIErrorStillReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"API rate limit exceeded"}`)
	}))
	defer server.Close()

	orig := conf.AppConfig.External.UpdateAPIURL
	conf.AppConfig.External.UpdateAPIURL = server.URL
	defer func() { conf.AppConfig.External.UpdateAPIURL = orig }()

	_, err := GetLatestInfo()
	if err == nil {
		t.Fatal("GetLatestInfo() on rate-limit message: got nil error, want error (real API errors must propagate)")
	}
}
