package update

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gypg/lodestar/internal/client"
	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/utils/log"
)

const maxUpdateAPIResponseBytes = 2 << 20 // 2 MiB — GitHub API release info JSON is typically < 100 KiB

// 解压炸弹防护：update 包只解压自更新归档，正常发布包是单个二进制 + 少量资源，
// 远低于这些上限。即使 SHA256 校验通过（或无校验文件时，见 verifyDownloadChecksum
// 的 non-blocking 语义），也拒绝异常膨胀的归档，避免磁盘撑爆。
const (
	maxZipEntries           = 1000    // 最大文件/目录条目数
	maxZipTotalUncompressed = 1 << 30 // 1 GiB 总解压上限（正常更新包 < 100 MiB）
	maxZipFileUncompressed  = 1 << 30 // 1 GiB 单文件解压上限
)

func getUpdateURL() string {
	if u := conf.AppConfig.External.UpdateURL; u != "" {
		return u
	}
	return "https://github.com/gypg/lodestar/releases/latest/download"
}

func getUpdateAPIURL() string {
	if u := conf.AppConfig.External.UpdateAPIURL; u != "" {
		return u
	}
	return "https://api.github.com/repos/gypg/lodestar/releases/latest"
}

type LatestInfo struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
	Message     string `json:"message"`
}

var github_pat = os.Getenv(strings.ToUpper(conf.APP_NAME) + "_GITHUB_PAT")

// doRequestWithFallback performs an HTTP GET request, first without proxy, then with proxy if failed.
func doRequestWithFallback(url string) ([]byte, error) {
	data, err := doRequest(url, false, 0, "")
	if err == nil {
		return data, nil
	}
	log.Warnf("direct request failed, trying with proxy: %v", err)
	return doRequest(url, true, 0, "")
}

func doAPIRequestWithFallback(url string) ([]byte, error) {
	data, err := doRequest(url, false, maxUpdateAPIResponseBytes, "update API response")
	if err == nil {
		return data, nil
	}
	log.Warnf("direct request failed, trying with proxy: %v", err)
	return doRequest(url, true, maxUpdateAPIResponseBytes, "update API response")
}

func doRequest(url string, useProxy bool, maxBytes int64, responseLabel string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hc, err := client.GetHTTPClientSystemProxy(useProxy)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Debugf("new request failed: %v", err)
		return nil, err
	}

	if github_pat != "" {
		req.Header.Set("Authorization", "Bearer "+github_pat)
	}

	resp, err := hc.Do(req)
	if err != nil {
		log.Debugf("request failed: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	reader := io.Reader(resp.Body)
	if maxBytes > 0 {
		reader = io.LimitReader(resp.Body, maxBytes+1)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		log.Debugf("read body failed: %v", err)
		return nil, err
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		if responseLabel == "" {
			responseLabel = "response"
		}
		return nil, fmt.Errorf("%s exceeds %d bytes limit", responseLabel, maxBytes)
	}
	return data, nil
}

func GetLatestInfo() (*LatestInfo, error) {
	body, err := doAPIRequestWithFallback(getUpdateAPIURL())
	if err != nil {
		return nil, err
	}

	var latestInfo LatestInfo
	if err := json.Unmarshal(body, &latestInfo); err != nil {
		log.Debugf("unmarshal body failed: %v", err)
		return nil, err
	}
	if latestInfo.Message != "" {
		// GitHub API responds 404 + {"message":"Not Found"} when the repo has no
		// releases yet. Treat that as "no release published" rather than a hard
		// error, so the settings page doesn't surface a 500 on a brand-new repo.
		if strings.Contains(strings.ToLower(latestInfo.Message), "not found") {
			log.Infof("no release published yet for current repo")
			return &LatestInfo{}, nil
		}
		return nil, fmt.Errorf("failed to get latest info: %s", latestInfo.Message)
	}
	return &latestInfo, nil
}

func unzip(data []byte, dest string) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		log.Debugf("new zip reader failed: %v", err)
		return err
	}

	// 解压炸弹防护：条目数、总解压大小、单文件解压大小三个维度。zip 头里的
	// UncompressedSize64 可能被篡改，所以既做预检查（拒绝明显异常的归档），
	// 也在 extractFile 里用 LimitReader 兜底（防止实际解压超过声明的上限）。
	if len(r.File) > maxZipEntries {
		return fmt.Errorf("zip archive has too many entries: %d (limit %d)", len(r.File), maxZipEntries)
	}
	var totalUncompressed uint64
	for _, f := range r.File {
		totalUncompressed += f.UncompressedSize64
		if totalUncompressed > maxZipTotalUncompressed {
			return fmt.Errorf("zip archive total uncompressed size exceeds %d bytes limit", maxZipTotalUncompressed)
		}
	}

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)

		if !isPathInDest(fpath, dest) {
			log.Debugf("invalid file path: %s", fpath)
			return fmt.Errorf("invalid file path: %s", fpath)
		}

		info := f.FileInfo()
		if info.IsDir() {
			os.MkdirAll(fpath, 0755)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		if err := extractFile(f, fpath); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, fpath string) error {
	if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
		log.Debugf("mkdir all failed: %v", err)
		return err
	}

	outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode().Perm())
	if err != nil {
		if err = os.Remove(fpath); err != nil {
			log.Debugf("remove file failed: %v", err)
			return err
		}
		outFile, err = os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			log.Debugf("open file failed: %v", err)
			return err
		}
	}
	defer outFile.Close()

	rc, err := f.Open()
	if err != nil {
		log.Debugf("open file failed: %v", err)
		return err
	}
	defer rc.Close()

	// 深度防御：zip 头里的 UncompressedSize64 可能被篡改（已在外层预检查，
	// 但预检查信任了头部声明）。用 LimitReader 读 maxZipFileUncompressed+1
	// 字节，若实际写入超过上限则拒绝——防止头部声明小但实际流膨胀的炸弹，
	// 也避免静默截断产出损坏的二进制。
	written, copyErr := io.Copy(outFile, io.LimitReader(rc, maxZipFileUncompressed+1))
	if copyErr != nil {
		log.Debugf("copy failed: %v", copyErr)
		return copyErr
	}
	if written > maxZipFileUncompressed {
		return fmt.Errorf("zip entry %q uncompressed size %d exceeds %d byte limit", f.Name, written, maxZipFileUncompressed)
	}
	return nil
}

func isPathInDest(fpath, dest string) bool {
	rel, err := filepath.Rel(dest, fpath)
	if err != nil {
		return false
	}
	return filepath.IsLocal(rel)
}
