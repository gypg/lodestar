package resp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/apperror"
)

func setupRespTest(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
}

// TestSuccessOmitsNilData locks in the omitempty behavior: Success with nil data
// produces a body with no "data" field at all (not "data":null).
func TestSuccessOmitsNilData(t *testing.T) {
	setupRespTest(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	Success(c, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if c.IsAborted() {
		t.Error("Success set the abort flag, want false (uses JSON, not AbortWithStatusJSON)")
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if _, ok := m["data"]; ok {
		t.Errorf("Success(nil) body has data field = %v, want absent (omitempty)", m["data"])
	}
	if m["code"] != float64(http.StatusOK) {
		t.Errorf("code = %v, want %d", m["code"], http.StatusOK)
	}
	if m["message"] != "success" {
		t.Errorf("message = %v, want %q", m["message"], "success")
	}
}

// TestSuccessIncludesData locks in that Success with a value includes it in the
// data field.
func TestSuccessIncludesData(t *testing.T) {
	setupRespTest(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	Success(c, map[string]any{"id": 7})
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if m["data"] == nil {
		t.Error("Success with data omitted it, want data present")
	}
}

// TestErrorAbortsAndInfersKey locks in C2 + C3: Error uses AbortWithStatusJSON
// and infers a message_key when the message matches an error.go constant.
func TestErrorAbortsAndInfersKey(t *testing.T) {
	setupRespTest(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	Error(c, http.StatusBadRequest, ErrInvalidJSON)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !c.IsAborted() {
		t.Error("Error did not set the abort flag, want true")
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if m["message_key"] != "errors.invalidJsonFormat" {
		t.Errorf("message_key = %v, want errors.invalidJsonFormat", m["message_key"])
	}
}

// TestErrorOmitsKeyWhenNoMatch locks in that Error with an unmatched message
// leaves message_key absent (empty string + omitempty).
func TestErrorOmitsKeyWhenNoMatch(t *testing.T) {
	setupRespTest(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	Error(c, http.StatusBadRequest, "some random message")
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if _, ok := m["message_key"]; ok {
		t.Errorf("message_key = %v, want absent (no match)", m["message_key"])
	}
}

// TestErrorWithKeySetsMessageKeyDirectly locks in C3: ErrorWithKey stores the
// provided key verbatim into message_key, with no inference.
func TestErrorWithKeySetsMessageKeyDirectly(t *testing.T) {
	setupRespTest(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ErrorWithKey(c, http.StatusTeapot, "boom", "custom.key", nil)
	if !c.IsAborted() {
		t.Error("ErrorWithKey did not set the abort flag, want true")
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if m["message_key"] != "custom.key" {
		t.Errorf("message_key = %v, want custom.key", m["message_key"])
	}
}

// TestInternalErrorAndBadGateway locks in the two convenience wrappers' status
// codes and that they abort.
func TestInternalErrorAndBadGateway(t *testing.T) {
	setupRespTest(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	InternalError(c)
	if rec.Code != http.StatusInternalServerError || !c.IsAborted() {
		t.Errorf("InternalError status=%d aborted=%v, want 500 true", rec.Code, c.IsAborted())
	}

	rec2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(rec2)
	BadGateway(c2)
	if rec2.Code != http.StatusBadGateway || !c2.IsAborted() {
		t.Errorf("BadGateway status=%d aborted=%v, want 502 true", rec2.Code, c2.IsAborted())
	}
}

// TestErrorWithCodeAndParamsStoresCodeAsKey locks in C3: ErrorWithCodeAndParams
// puts the code argument directly into message_key and forwards params to
// message_args.
func TestErrorWithCodeAndParamsStoresCodeAsKey(t *testing.T) {
	setupRespTest(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ErrorWithCodeAndParams(c, http.StatusInternalServerError, "my.code", "boom", map[string]any{"a": 1})
	if !c.IsAborted() {
		t.Error("ErrorWithCodeAndParams did not set the abort flag, want true")
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if m["message_key"] != "my.code" {
		t.Errorf("message_key = %v, want my.code", m["message_key"])
	}
	if m["message_args"] == nil {
		t.Error("message_args absent, want present")
	}
}

// TestErrorWithAppErrorUsesAppStatusOverridesFallback locks in C5: an *Error
// with a non-zero status overrides the fallback status.
func TestErrorWithAppErrorUsesAppStatusOverridesFallback(t *testing.T) {
	setupRespTest(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ErrorWithAppError(c, http.StatusInternalServerError, apperror.New("c", "m").WithStatus(429))
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 (app status overrides fallback)", rec.Code)
	}
}

// TestErrorWithAppErrorFallsBackStatusWhenZero locks in C5: an *Error with
// Status 0 keeps the fallback status.
func TestErrorWithAppErrorFallsBackStatusWhenZero(t *testing.T) {
	setupRespTest(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ErrorWithAppError(c, http.StatusInternalServerError, apperror.New("c", "m"))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (fallback when app status is 0)", rec.Code)
	}
}

// TestErrorWithAppErrorPlainErrorFallsBackAndOmitsKey locks in C5's most common
// path: a plain error gives Status 0 and Code "", so it keeps the fallback
// status and emits no message_key while message is the error text.
func TestErrorWithAppErrorPlainErrorFallsBackAndOmitsKey(t *testing.T) {
	setupRespTest(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ErrorWithAppError(c, http.StatusBadGateway, errPlain("boom"))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (fallback for plain error)", rec.Code)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if _, ok := m["message_key"]; ok {
		t.Errorf("message_key = %v, want absent (plain error has empty code)", m["message_key"])
	}
	if m["message"] != "boom" {
		t.Errorf("message = %v, want boom", m["message"])
	}
}

// TestInvalidJSONUsesAppErrorCode locks in C6: InvalidJSON surfaces the apperror
// code common.invalid_json as message_key, distinct from the inferred
// errors.invalidJsonFormat.
func TestInvalidJSONUsesAppErrorCode(t *testing.T) {
	setupRespTest(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	InvalidJSON(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if m["message_key"] != apperror.CodeCommonInvalidJSON {
		t.Errorf("message_key = %v, want %q (apperror code, not inferred)", m["message_key"], apperror.CodeCommonInvalidJSON)
	}
}

// TestInvalidParamUsesAppErrorCode locks in C6 analog for InvalidParam.
func TestInvalidParamUsesAppErrorCode(t *testing.T) {
	setupRespTest(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	InvalidParam(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if m["message_key"] != apperror.CodeCommonInvalidParam {
		t.Errorf("message_key = %v, want %q", m["message_key"], apperror.CodeCommonInvalidParam)
	}
}

// TestInferErrorMessageKeyMatchesConstants locks in C4: messages matching
// error.go constants map to their error keys.
func TestInferErrorMessageKeyMatchesConstants(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{ErrBadRequest, "errors.invalidRequestParameters"},
		{ErrInvalidJSON, "errors.invalidJsonFormat"},
		{ErrInvalidParam, "errors.invalidParameter"},
		{ErrValidation, "errors.inputValidationFailed"},
		{ErrDuplicateResource, "errors.resourceAlreadyExists"},
		{ErrResourceNotFound, "errors.resourceNotFound"},
		{ErrInternalServer, "errors.internalServer"},
		{ErrDatabase, "errors.database"},
		{ErrUnauthorized, "errors.authenticationFailed"},
		{ErrTooManyRequests, "errors.tooManyRequests"},
	}
	for _, tt := range tests {
		if got := inferErrorMessageKey(tt.in); got != tt.want {
			t.Errorf("inferErrorMessageKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestInferErrorMessageKeyMatchesLiterals locks in C4: hardcoded string literals
// also map to their keys, covering every branch of the switch.
func TestInferErrorMessageKeyMatchesLiterals(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"permission denied", "errors.permissionDenied"},
		{"channel not found", "errors.channelNotFound"},
		{"group not found", "errors.groupNotFound"},
		{"group test progress not found", "errors.groupTestProgressNotFound"},
		{"ai route progress not found", "errors.aiRouteProgressNotFound"},
		{"missing progress id", "errors.missingProgressId"},
		{"channel name already exists", "errors.channelNameAlreadyExists"},
		{"group name already exists", "errors.groupNameAlreadyExists"},
		{"group contains duplicate channel/model items", "errors.groupDuplicateChannelModelItems"},
		{"database schema is outdated", "errors.databaseSchemaOutdated"},
		{"database schema is outdated; restart the service to apply the latest migrations", "errors.databaseSchemaOutdatedRestart"},
		{"upstream service unavailable", "errors.upstreamServiceUnavailable"},
	}
	for _, tt := range tests {
		if got := inferErrorMessageKey(tt.in); got != tt.want {
			t.Errorf("inferErrorMessageKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestInferErrorMessageKeyCaseSpacingInsensitive locks in C4's only non-obvious
// behavior: matching does TrimSpace + ToLower, so a spaced lowercase variant
// still hits the constant branch.
func TestInferErrorMessageKeyCaseSpacingInsensitive(t *testing.T) {
	if got := inferErrorMessageKey("  invalid json format  "); got != "errors.invalidJsonFormat" {
		t.Errorf("inferErrorMessageKey(spaced lowercase) = %q, want errors.invalidJsonFormat", got)
	}
}

// TestInferErrorMessageKeyDefaultEmpty locks in C4's default branch: an unmatched
// message returns the empty string.
func TestInferErrorMessageKeyDefaultEmpty(t *testing.T) {
	if got := inferErrorMessageKey("no match here"); got != "" {
		t.Errorf("inferErrorMessageKey(unmatched) = %q, want empty", got)
	}
}

// errPlain is a tiny helper so the plain-error path is explicit without an
// import cycle.
func errPlain(msg string) error {
	return &plainError{msg}
}

type plainError struct{ msg string }

func (e *plainError) Error() string { return e.msg }
