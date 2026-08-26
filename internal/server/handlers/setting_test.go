package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	stg "github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/utils/semantic_cache"
)

func TestDecodeDBDumpReaderSupportsWrappedDump(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := `{"code":200,"message":"success","data":{"version":1,"settings":[{"key":"stats_save_interval","value":"10"}]}}`

	var dump model.DBDump
	if err := decodeDBDumpReader(strings.NewReader(body), &dump); err != nil {
		t.Fatalf("decodeDBDumpReader() error = %v", err)
	}

	if dump.Version != 1 {
		t.Fatalf("dump.Version = %d, want 1", dump.Version)
	}
	if len(dump.Settings) != 1 {
		t.Fatalf("len(dump.Settings) = %d, want 1", len(dump.Settings))
	}
	if dump.Settings[0].Key != model.SettingKeyStatsSaveInterval {
		t.Fatalf("dump.Settings[0].Key = %q, want %q", dump.Settings[0].Key, model.SettingKeyStatsSaveInterval)
	}
	if dump.Settings[0].Value != "10" {
		t.Fatalf("dump.Settings[0].Value = %q, want %q", dump.Settings[0].Value, "10")
	}
}

func TestImportDBRejectsOversizedJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	restore := setDBImportLimitForTest(128)
	defer restore()

	body := `{"padding":"` + strings.Repeat("a", 256) + `"}`

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/setting/import", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	importDB(c)

	assertOversizedImportResponse(t, recorder)
}

func TestImportDBRejectsOversizedMultipartFile(t *testing.T) {
	gin.SetMode(gin.TestMode)

	restore := setDBImportLimitsForTest(128, 0)
	defer restore()

	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	fileWriter, err := writer.CreateFormFile("file", "backup.json")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := fileWriter.Write([]byte(`{"padding":"` + strings.Repeat("a", 256) + `"}`)); err != nil {
		t.Fatalf("fileWriter.Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/setting/import", bytes.NewReader(payload.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	importDB(c)

	assertOversizedImportResponse(t, recorder)
}

func TestReadDBDumpRejectsMultipartEnvelopeLargerThanImportLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	fileBody := []byte(`{"version":1}`)
	padding := strings.Repeat("a", 256)

	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	if err := writer.WriteField("padding", padding); err != nil {
		t.Fatalf("WriteField() error = %v", err)
	}
	fileWriter, err := writer.CreateFormFile("file", "backup.json")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := fileWriter.Write(fileBody); err != nil {
		t.Fatalf("fileWriter.Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}

	restore := setDBImportLimitsForTest(int64(len(fileBody)+32), 64)
	defer restore()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/setting/import", bytes.NewReader(payload.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	var dump model.DBDump
	err = readDBDump(c, &dump)
	if !isDBImportTooLarge(err) {
		t.Fatalf("readDBDump() error = %v, want import-size error", err)
	}
}

func TestImportDBRefreshesSemanticCacheRuntime(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	t.Cleanup(func() {
		semantic_cache.Reset()
		_ = db.Close()
	})

	semantic_cache.ApplyRuntimeConfig(semantic_cache.RuntimeConfig{
		Enabled:          true,
		MaxEntries:       8,
		Threshold:        0.98,
		TTL:              time.Hour,
		EmbeddingBaseURL: "https://stale.example.com",
		EmbeddingModel:   "text-embedding-3-small",
	})
	if !semantic_cache.RuntimeEnabled() {
		t.Fatal("expected seeded semantic cache runtime to be enabled")
	}

	dump := model.DBDump{
		Version: 1,
		Settings: []model.Setting{
			{Key: model.SettingKeySemanticCacheEnabled, Value: "false"},
		},
	}
	body, err := json.Marshal(dump)
	if err != nil {
		t.Fatalf("marshal dump: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/setting/import", bytes.NewReader(body))
	c.Request = c.Request.WithContext(context.Background())
	c.Request.Header.Set("Content-Type", "application/json")

	importDB(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if semantic_cache.RuntimeEnabled() {
		t.Fatal("expected importDB to refresh semantic cache runtime and clear stale state")
	}
}

// TestSetSettingRejectsNonFiniteOverdraftBound 钉死 setSetting → Setting.Validate()
// 这条接线本身。
//
// model 层的校验器单测挡不住"handler 根本不调它"这类改动：把 setSetting 里那两行
// setting.Validate() 删掉，internal/model 的测试依然全绿，而 max_expected_request_cost
// 又能存成 "NaN" —— 并发闸门的 `headroom <= inflight * bound` 随之恒为 false，连欠款
// 账户都放行（见 internal/op/billing/inflight.go）。
//
// 断言两件事：被拒的请求返回 400，且库里的值没被改动。只断状态码不够 —— 先写库再校验
// 的实现也能回 400。
func TestSetSettingRejectsNonFiniteOverdraftBound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if got, err := stg.GetString(model.SettingKeyMaxExpectedRequestCost); err != nil || got != "0.5" {
		t.Fatalf("出厂默认值 = %q (err=%v), want 0.5 —— 种子没进来，下面「值未改动」的断言会变成空过", got, err)
	}

	for _, tt := range []struct {
		value      string
		wantStatus int
	}{
		{value: "2", wantStatus: http.StatusOK},
		{value: "0", wantStatus: http.StatusOK},
		{value: "NaN", wantStatus: http.StatusBadRequest},
		{value: "nan", wantStatus: http.StatusBadRequest},
		{value: "Inf", wantStatus: http.StatusBadRequest},
		{value: "+Inf", wantStatus: http.StatusBadRequest},
		{value: "-1", wantStatus: http.StatusBadRequest},
		{value: "abc", wantStatus: http.StatusBadRequest},
		{value: "", wantStatus: http.StatusBadRequest},
	} {
		before, err := stg.GetString(model.SettingKeyMaxExpectedRequestCost)
		if err != nil {
			t.Fatalf("read before %q: %v", tt.value, err)
		}

		body, err := json.Marshal(model.Setting{Key: model.SettingKeyMaxExpectedRequestCost, Value: tt.value})
		if err != nil {
			t.Fatalf("marshal %q: %v", tt.value, err)
		}
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/setting/set", bytes.NewReader(body))
		c.Request = c.Request.WithContext(context.Background())
		c.Request.Header.Set("Content-Type", "application/json")

		setSetting(c)

		if recorder.Code != tt.wantStatus {
			t.Errorf("value=%q status = %d, want %d; body=%s", tt.value, recorder.Code, tt.wantStatus, recorder.Body.String())
		}

		after, err := stg.GetString(model.SettingKeyMaxExpectedRequestCost)
		if err != nil {
			t.Fatalf("read after %q: %v", tt.value, err)
		}
		if tt.wantStatus == http.StatusBadRequest && after != before {
			t.Errorf("value=%q 被拒却写进了库: %q → %q（校验发生在写之后）", tt.value, before, after)
		}
		if tt.wantStatus == http.StatusOK && after != tt.value {
			t.Errorf("value=%q 回了 200 但库里是 %q —— 没真的写进去", tt.value, after)
		}
	}

	// 收尾写回种子值：stg 的设置缓存是包级的，把 "0" 留在里面会让同包后续测试
	// 在一个"并发闸门关着"的环境里跑。顺带再确认一次合法值确实落库。
	body, err := json.Marshal(model.Setting{Key: model.SettingKeyMaxExpectedRequestCost, Value: "0.5"})
	if err != nil {
		t.Fatalf("marshal restore: %v", err)
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/setting/set", bytes.NewReader(body))
	c.Request = c.Request.WithContext(context.Background())
	c.Request.Header.Set("Content-Type", "application/json")
	setSetting(c)
	if recorder.Code != http.StatusOK {
		t.Fatalf("写回种子值 status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if got, err := stg.GetString(model.SettingKeyMaxExpectedRequestCost); err != nil || got != "0.5" {
		t.Fatalf("写回种子值后库里是 %q (err=%v), want 0.5", got, err)
	}
}

func setDBImportLimitForTest(limit int64) func() {
	return setDBImportLimitsForTest(limit, maxDBImportMultipartExtraBytes)
}

func setDBImportLimitsForTest(limit int64, extra int64) func() {
	original := maxDBImportBytes
	originalExtra := maxDBImportMultipartExtraBytes
	maxDBImportBytes = limit
	maxDBImportMultipartExtraBytes = extra
	return func() {
		maxDBImportBytes = original
		maxDBImportMultipartExtraBytes = originalExtra
	}
}

func assertOversizedImportResponse(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}

	var response struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("response code = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
	if !strings.Contains(response.Message, "backup file exceeds") {
		t.Fatalf("response message = %q, want substring %q", response.Message, "backup file exceeds")
	}
}

func TestExportDBReturnsJSONContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/setting/export?include_logs=false&include_stats=false", nil)
	c.Request = c.Request.WithContext(context.Background())

	exportDB(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	contentType := recorder.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}

	disposition := recorder.Header().Get("Content-Disposition")
	if !strings.Contains(disposition, "attachment") {
		t.Fatalf("Content-Disposition = %q, want attachment", disposition)
	}

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("export body is not valid JSON: %v", err)
	}
}
