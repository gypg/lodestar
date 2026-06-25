package update

import (
	"crypto/sha256"
	"encoding/hex"
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

// --- SHA256 checksum verification tests ---

func TestSha256Hex(t *testing.T) {
	data := []byte("hello world")
	got := sha256Hex(data)
	want := hex.EncodeToString(sha256.New().Sum(data))
	// Recompute properly.
	h := sha256.Sum256(data)
	want = hex.EncodeToString(h[:])
	if got != want {
		t.Errorf("sha256Hex() = %s, want %s", got, want)
	}
}

func TestParseChecksumFile_BareHex(t *testing.T) {
	hash := strings.Repeat("a", 64)
	got := parseChecksumFile([]byte(hash))
	if got != hash {
		t.Errorf("parseChecksumFile(bare hex) = %q, want %q", got, hash)
	}
}

func TestParseChecksumFile_Sha256SumStyle(t *testing.T) {
	hash := strings.Repeat("b", 64)
	input := hash + "  lodestar-linux-x86_64.zip"
	got := parseChecksumFile([]byte(input))
	if got != hash {
		t.Errorf("parseChecksumFile(sha256sum style) = %q, want %q", got, hash)
	}
}

func TestParseChecksumFile_WithNewline(t *testing.T) {
	hash := strings.Repeat("c", 64)
	input := hash + "  file.zip\n"
	got := parseChecksumFile([]byte(input))
	if got != hash {
		t.Errorf("parseChecksumFile(with newline) = %q, want %q", got, hash)
	}
}

func TestParseChecksumFile_Empty(t *testing.T) {
	got := parseChecksumFile([]byte(""))
	if got != "" {
		t.Errorf("parseChecksumFile(empty) = %q, want empty string", got)
	}
}

func TestParseChecksumFile_InvalidHex(t *testing.T) {
	got := parseChecksumFile([]byte(strings.Repeat("z", 64)))
	if got != "" {
		t.Errorf("parseChecksumFile(invalid hex) = %q, want empty string", got)
	}
}

func TestParseChecksumFile_TooShort(t *testing.T) {
	got := parseChecksumFile([]byte("abcdef1234"))
	if got != "" {
		t.Errorf("parseChecksumFile(too short) = %q, want empty string", got)
	}
}

func TestVerifyDownloadChecksum_MatchPasses(t *testing.T) {
	testData := []byte("the actual binary content")
	h := sha256.Sum256(testData)
	hashHex := hex.EncodeToString(h[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = io.WriteString(w, hashHex+"  file.zip\n")
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	orig := conf.AppConfig.External.UpdateURL
	conf.AppConfig.External.UpdateURL = server.URL
	defer func() { conf.AppConfig.External.UpdateURL = orig }()

	err := verifyDownloadChecksum(testData, "file.zip")
	if err != nil {
		t.Errorf("verifyDownloadChecksum(match) = %v, want nil", err)
	}
}

func TestVerifyDownloadChecksum_MismatchReturnsError(t *testing.T) {
	testData := []byte("the actual binary content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = io.WriteString(w, strings.Repeat("0", 64))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	orig := conf.AppConfig.External.UpdateURL
	conf.AppConfig.External.UpdateURL = server.URL
	defer func() { conf.AppConfig.External.UpdateURL = orig }()

	err := verifyDownloadChecksum(testData, "file.zip")
	if err == nil {
		t.Fatal("verifyDownloadChecksum(mismatch) = nil, want error")
	}
	if !strings.Contains(err.Error(), "checksum verification FAILED") {
		t.Errorf("error = %q, want it to contain 'checksum verification FAILED'", err.Error())
	}
}

func TestVerifyDownloadChecksum_MissingFileReturnsNil(t *testing.T) {
	testData := []byte("binary content")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Both the zip and the .sha256 return 404.
		http.NotFound(w, r)
	}))
	defer server.Close()

	orig := conf.AppConfig.External.UpdateURL
	conf.AppConfig.External.UpdateURL = server.URL
	defer func() { conf.AppConfig.External.UpdateURL = orig }()

	err := verifyDownloadChecksum(testData, "file.zip")
	if err != nil {
		t.Errorf("verifyDownloadChecksum(missing .sha256) = %v, want nil (non-blocking)", err)
	}
}

func TestVerifyDownloadChecksum_Sha256SumStyleWithFilename(t *testing.T) {
	testData := []byte("release binary payload")
	h := sha256.Sum256(testData)
	hashHex := hex.EncodeToString(h[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			// sha256sum format: hash  two-spaces  filename
			_, _ = fmt.Fprintf(w, "%s  %s\n", hashHex, "file.zip")
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	orig := conf.AppConfig.External.UpdateURL
	conf.AppConfig.External.UpdateURL = server.URL
	defer func() { conf.AppConfig.External.UpdateURL = orig }()

	err := verifyDownloadChecksum(testData, "file.zip")
	if err != nil {
		t.Errorf("verifyDownloadChecksum(sha256sum style) = %v, want nil", err)
	}
}
