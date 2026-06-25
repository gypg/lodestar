package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/backup"
	"github.com/gypg/lodestar/internal/op/dbmigration"
	"github.com/gypg/lodestar/internal/op/ops"
	stg "github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
	"github.com/gypg/lodestar/internal/task"
	"github.com/gypg/lodestar/internal/utils/log"
)

var (
	maxDBImportBytes               int64 = 64 << 20
	maxDBImportMultipartExtraBytes int64 = 1 << 20
)

func init() {
	router.NewGroupRouter("/api/v1/setting").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermSettingsRead)).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(getSettingList),
		).
		AddRoute(
			router.NewRoute("/set", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Use(middleware.RequireJSON()).
				Handle(setSetting),
		).
		AddRoute(
			router.NewRoute("/export", http.MethodGet).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Handle(exportDB),
		).
		AddRoute(
			router.NewRoute("/import", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Handle(importDB),
		).
		AddRoute(
			router.NewRoute("/database/test", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Use(middleware.RequireJSON()).
				Handle(testDatabaseConnection),
		).
		AddRoute(
			router.NewRoute("/database/migrate", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Use(middleware.RequireJSON()).
				Handle(migrateDatabase),
		).
		AddRoute(
			router.NewRoute("/image-bed/test", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Handle(testImageBedConnection),
		)
}

func getSettingList(c *gin.Context) {
	settings, err := stg.List(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	// Mask secret values for ALL roles (including admin) to limit blast radius.
	maskSensitiveSettings(settings)
	if isViewerRole(c.GetString("user_role")) {
		redactSettingsURLsForViewer(settings)
	}
	resp.Success(c, settings)
}

func setSetting(c *gin.Context) {
	var setting model.Setting
	if err := c.ShouldBindJSON(&setting); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := setting.Validate(); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := stg.SetString(setting.Key, setting.Value); err != nil {
		resp.InternalError(c)
		return
	}
	// Setting is now persisted. All downstream effects are best-effort:
	// log failures but do not return an error status to the client,
	// which would misleadingly suggest the setting was NOT saved.
	if shouldRefreshSemanticCacheRuntime(setting.Key) {
		if err := ops.RefreshSemanticCacheRuntime(); err != nil {
			log.Warnf("semantic cache refresh failed after setting %s: %v", setting.Key, err)
		}
	}
	switch setting.Key {
	case model.SettingKeyStatsSaveInterval:
		minutes, err := strconv.Atoi(setting.Value)
		if err != nil {
			log.Warnf("invalid stats_save_interval value %q after persist: %v", setting.Value, err)
			break
		}
		interval := time.Duration(minutes) * time.Minute
		task.Update(task.TaskStatsSave, interval)
		task.Update(task.TaskRuntimeState, interval)
	case model.SettingKeyModelInfoUpdateInterval:
		hours, err := strconv.Atoi(setting.Value)
		if err != nil {
			log.Warnf("invalid model_info_update_interval value %q after persist: %v", setting.Value, err)
			break
		}
		task.Update(string(setting.Key), time.Duration(hours)*time.Hour)
	case model.SettingKeySyncLLMInterval:
		hours, err := strconv.Atoi(setting.Value)
		if err != nil {
			log.Warnf("invalid sync_llm_interval value %q after persist: %v", setting.Value, err)
			break
		}
		task.Update(string(setting.Key), time.Duration(hours)*time.Hour)
	case model.SettingKeyLogLevel:
		log.SetLevel(setting.Value)
	case model.SettingKeyRelayLogKeepEnabled:
		// 独立日志库模式下：关闭日志则断开日志库连接，开启则重连。
		// 共用主库时为空操作。失败仅记录，不影响设置已持久化的事实。
		enabled, err := strconv.ParseBool(setting.Value)
		if err != nil {
			log.Warnf("invalid relay_log_keep_enabled value %q after persist: %v", setting.Value, err)
			break
		}
		if err := op.RelayLogApplyKeepEnabled(c.Request.Context(), enabled); err != nil {
			log.Warnf("failed to apply log database lifecycle after toggling relay_log_keep_enabled: %v", err)
		}
	}
	resp.Success(c, setting)
}

func shouldRefreshSemanticCacheRuntime(key model.SettingKey) bool {
	switch key {
	case model.SettingKeySemanticCacheEnabled,
		model.SettingKeySemanticCacheTTL,
		model.SettingKeySemanticCacheThreshold,
		model.SettingKeySemanticCacheMaxEntries,
		model.SettingKeySemanticCacheEmbeddingBaseURL,
		model.SettingKeySemanticCacheEmbeddingAPIKey,
		model.SettingKeySemanticCacheEmbeddingModel,
		model.SettingKeySemanticCacheEmbeddingTimeoutSeconds:
		return true
	default:
		return false
	}
}

// exportDB 导出全库数据为 JSON 文件下载。
//
// 这是一个下载型接口（Content-Disposition: attachment），直接返回原始 JSON dump
// 供浏览器保存为文件，不使用管理端标准 {code, message, data} envelope——
// 这是有意例外，不是遗漏。
func exportDB(c *gin.Context) {
	if !backupMutex.TryLock() {
		resp.Error(c, http.StatusConflict, "backup or restore already in progress")
		return
	}
	defer backupMutex.Unlock()

	includeLogs, _ := strconv.ParseBool(c.DefaultQuery("include_logs", "false"))
	includeStats, _ := strconv.ParseBool(c.DefaultQuery("include_stats", "false"))

	dump, err := backup.ExportAll(c.Request.Context(), includeLogs, includeStats)
	if err != nil {
		resp.InternalError(c)
		return
	}

	// User.Password is tagged json:"-", so passwords are lost during JSON
	// serialisation. Exclude users from the JSON export to prevent importing
	// accounts with empty passwords that would lock the admin out.
	dump.Users = nil

	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", "attachment; filename=\"lodestar-export-"+time.Now().Format("20060102150405")+".json\"")
	c.Status(http.StatusOK)

	// Stream JSON to avoid buffering the entire dump in memory
	encoder := json.NewEncoder(c.Writer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(dump)
}

func importDB(c *gin.Context) {
	if !backupMutex.TryLock() {
		resp.Error(c, http.StatusConflict, "backup or restore already in progress")
		return
	}
	defer backupMutex.Unlock()

	var dump model.DBDump
	defer cleanupDBImportMultipartForm(c)

	if err := readDBDump(c, &dump); err != nil {
		status := http.StatusBadRequest
		if isDBImportTooLarge(err) {
			status = http.StatusRequestEntityTooLarge
		}
		resp.Error(c, status, err.Error())
		return
	}

	mode := c.DefaultQuery("mode", model.ImportModeIncremental)
	if mode != model.ImportModeIncremental && mode != model.ImportModeFull {
		resp.Error(c, http.StatusBadRequest, fmt.Sprintf("invalid import mode: %s (use 'incremental' or 'full')", mode))
		return
	}

	result, err := backup.ImportWithMode(c.Request.Context(), &dump, mode)
	if err != nil {
		log.Errorf("importDB failed: %v", err)
		resp.Error(c, http.StatusBadRequest, "Import failed: invalid or corrupted data")
		return
	}

	if err := op.InitCache(); err != nil {
		log.Errorf("importDB: cache init failed: %v", err)
		resp.InternalError(c)
		return
	}
	if err := ops.RefreshSemanticCacheRuntime(); err != nil {
		log.Errorf("importDB: semantic cache refresh failed: %v", err)
		resp.InternalError(c)
		return
	}

	resp.Success(c, result)
}

func testDatabaseConnection(c *gin.Context) {
	var req model.DatabaseMigrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := dbmigration.TestConnection(c.Request.Context(), req); err != nil {
		log.Errorf("testDatabaseConnection failed: %v", err)
		resp.Error(c, http.StatusBadRequest, "Database connection test failed")
		return
	}
	resp.Success(c, true)
}

func migrateDatabase(c *gin.Context) {
	var req model.DatabaseMigrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	result, err := dbmigration.Migrate(c.Request.Context(), req)
	if err != nil {
		log.Errorf("migrateDatabase failed: %v", err)
		resp.Error(c, http.StatusBadRequest, "Database migration failed")
		return
	}
	resp.Success(c, result)
}

func testImageBedConnection(c *gin.Context) {
	endpoint, _ := stg.GetString(model.SettingKeyImageBedEndpoint)
	token, _ := stg.GetString(model.SettingKeyImageBedToken)

	if endpoint == "" {
		resp.Error(c, http.StatusBadRequest, "image bed endpoint is not configured")
		return
	}

	// Send a tiny 1x1 PNG as a connectivity test.
	tinyPNG := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
		0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc, 0x33, 0x00, 0x00, 0x00,
		0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("image", "test.png")
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, "failed to build test request")
		return
	}
	if _, err := part.Write(tinyPNG); err != nil {
		resp.Error(c, http.StatusInternalServerError, "failed to write test image")
		return
	}
	writer.Close()

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
	if err != nil {
		log.Errorf("testImageBedConnection: build request failed: %v", err)
		resp.Error(c, http.StatusBadRequest, "Invalid endpoint URL")
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	httpResp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Errorf("testImageBedConnection: request failed: %v", err)
		resp.Error(c, http.StatusBadGateway, "Connection to image bed failed")
		return
	}
	defer httpResp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4*1024))

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		resp.Error(c, http.StatusBadGateway, fmt.Sprintf("image bed returned %d: %s", httpResp.StatusCode, string(body)))
		return
	}

	resp.Success(c, gin.H{
		"status":  httpResp.StatusCode,
		"body":    string(body),
		"message": "image bed connection successful",
	})
}

func decodeDBDump(body []byte, dump *model.DBDump) error {
	return decodeDBDumpReader(bytes.NewReader(body), dump)
}

func readDBDump(c *gin.Context, dump *model.DBDump) error {
	contentType := c.GetHeader("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		limitDBImportRequestBody(c)
		fh, err := c.FormFile("file")
		if err != nil {
			return normalizeDBImportMultipartError(err)
		}
		if fh.Size > 0 && fh.Size > maxDBImportBytes {
			return newDBImportTooLargeError()
		}

		f, err := fh.Open()
		if err != nil {
			return err
		}
		defer f.Close()

		return decodeDBDumpReader(f, dump)
	}

	return decodeDBDumpReader(c.Request.Body, dump)
}

func cleanupDBImportMultipartForm(c *gin.Context) {
	if c == nil || c.Request == nil || c.Request.MultipartForm == nil {
		return
	}
	_ = c.Request.MultipartForm.RemoveAll()
}

func limitDBImportRequestBody(c *gin.Context) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxDBImportBytes+maxDBImportMultipartExtraBytes)
}

func normalizeDBImportMultipartError(err error) error {
	if err == nil {
		return nil
	}
	if isHTTPMaxBytesError(err) {
		return newDBImportTooLargeError()
	}
	if errors.Is(err, http.ErrMissingFile) {
		return fmt.Errorf("missing upload file field 'file'")
	}
	return err
}

func decodeDBDumpReader(r io.Reader, dump *model.DBDump) error {
	limitedReader := &io.LimitedReader{R: r, N: maxDBImportBytes + 1}
	if dump == nil {
		var empty struct{}
		if err := json.NewDecoder(limitedReader).Decode(&empty); err != nil {
			if limitedReader.N <= 0 {
				return newDBImportTooLargeError()
			}
			return err
		}
		if limitedReader.N <= 0 {
			return newDBImportTooLargeError()
		}
		return nil
	}

	var envelope struct {
		model.DBDump
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(limitedReader).Decode(&envelope); err != nil {
		if limitedReader.N <= 0 {
			return newDBImportTooLargeError()
		}
		return err
	}
	if limitedReader.N <= 0 {
		return newDBImportTooLargeError()
	}

	*dump = envelope.DBDump

	if isEmptyDBDump(*dump) && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, dump); err != nil {
			return err
		}
	}

	return nil
}

func isEmptyDBDump(dump model.DBDump) bool {
	return dump.Version == 0 &&
		len(dump.Channels) == 0 &&
		len(dump.ChannelKeys) == 0 &&
		len(dump.Groups) == 0 &&
		len(dump.GroupItems) == 0 &&
		len(dump.Settings) == 0 &&
		len(dump.APIKeys) == 0 &&
		len(dump.LLMInfos) == 0 &&
		len(dump.RelayLogs) == 0 &&
		len(dump.StatsDaily) == 0 &&
		len(dump.StatsHourly) == 0 &&
		len(dump.StatsTotal) == 0 &&
		len(dump.StatsChannel) == 0 &&
		len(dump.StatsModel) == 0 &&
		len(dump.StatsAPIKey) == 0
}

func isDBImportTooLarge(err error) bool {
	return err != nil && strings.Contains(err.Error(), "backup file exceeds")
}

func isHTTPMaxBytesError(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

func newDBImportTooLargeError() error {
	return fmt.Errorf("backup file exceeds %s import limit; retry without logs/stats or use a database-level backup for larger datasets", formatDBImportLimit(maxDBImportBytes))
}

func formatDBImportLimit(limit int64) string {
	switch {
	case limit%(1<<20) == 0:
		return fmt.Sprintf("%d MiB", limit>>20)
	case limit%(1<<10) == 0:
		return fmt.Sprintf("%d KiB", limit>>10)
	default:
		return fmt.Sprintf("%d bytes", limit)
	}
}
