package apperror

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

// TestErrorReturnsMessageWhenSet locks in the first fallback of Error(): when
// Message is non-empty it wins over Err and Code.
func TestErrorReturnsMessageWhenSet(t *testing.T) {
	e := New("code", "hello").WithParam("k", 1)
	if got := e.Error(); got != "hello" {
		t.Errorf("Error() = %q, want %q", got, "hello")
	}
}

// TestErrorFallsBackToErrWhenMessageEmpty locks in the second fallback: with an
// empty Message but a wrapped Err, Error() returns Err.Error().
func TestErrorFallsBackToErrWhenMessageEmpty(t *testing.T) {
	e := Wrap("code", "", errors.New("inner boom"))
	if got := e.Error(); got != "inner boom" {
		t.Errorf("Error() = %q, want %q", got, "inner boom")
	}
}

// TestErrorFallsBackToCodeWhenEmpty locks in the third fallback: when both
// Message and Err are absent, Error() returns the Code. This is the most
// surprising branch and is locked here as current behavior.
func TestErrorFallsBackToCodeWhenEmpty(t *testing.T) {
	e := New("common.some_code", "")
	if got := e.Error(); got != "common.some_code" {
		t.Errorf("Error() = %q, want %q", got, "common.some_code")
	}
}

// TestErrorNilReceiver locks in the nil-*Error guard: Error() and Unwrap() on a
// nil *Error return "" / nil rather than panicking.
func TestErrorNilReceiver(t *testing.T) {
	var e *Error
	if got := e.Error(); got != "" {
		t.Errorf("nil *Error Error() = %q, want empty", got)
	}
	if got := e.Unwrap(); got != nil {
		t.Errorf("nil *Error Unwrap() = %v, want nil", got)
	}
}

// TestUnwrapReturnsWrappedErr locks in that Unwrap() on a non-nil *Error returns
// the wrapped error, and nil when there is none.
func TestUnwrapReturnsWrappedErr(t *testing.T) {
	inner := errors.New("inner")
	if got := Wrap("c", "m", inner).Unwrap(); got != inner {
		t.Errorf("Unwrap() = %v, want inner error", got)
	}
	if got := New("c", "m").Unwrap(); got != nil {
		t.Errorf("New().Unwrap() = %v, want nil", got)
	}
}

// TestWithStatusNilReceiverAndMutates locks in two behaviors: WithStatus on a
// nil *Error returns nil, and on a real one it mutates the receiver in place,
// so the originally captured variable sees the change too.
func TestWithStatusNilReceiverAndMutates(t *testing.T) {
	var e *Error
	if got := e.WithStatus(500); got != nil {
		t.Errorf("nil *Error WithStatus() = %v, want nil", got)
	}

	orig := New("c", "m")
	ret := orig.WithStatus(429)
	if ret.Status != 429 || orig.Status != 429 {
		t.Errorf("WithStatus mutated in place: ret.Status=%d orig.Status=%d, want both 429", ret.Status, orig.Status)
	}
}

// TestWithParamNilReceiverAndEmptyKey locks in the nil guard and the empty-key
// guard: WithParam("", v) silently returns the receiver without writing.
func TestWithParamNilReceiverAndEmptyKey(t *testing.T) {
	var e *Error
	if got := e.WithParam("a", 1); got != nil {
		t.Errorf("nil *Error WithParam() = %v, want nil", got)
	}

	e2 := New("c", "m")
	ret := e2.WithParam("", "ignored")
	if ret != e2 {
		t.Error("WithParam with empty key returned a different pointer")
	}
	if e2.Params != nil {
		t.Errorf("WithParam(\"\", v) wrote Params = %v, want nil", e2.Params)
	}
}

// TestWithParamsNilClears locks in the counter-intuitive behavior: WithParams(nil)
// sets Params to nil rather than preserving previously set keys.
func TestWithParamsNilClears(t *testing.T) {
	e := New("c", "m").WithParam("a", 1)
	if e.Params == nil {
		t.Fatal("setup: Params should be non-nil before WithParams(nil)")
	}
	e.WithParams(nil)
	if e.Params != nil {
		t.Errorf("WithParams(nil) left Params = %v, want nil (cleared)", e.Params)
	}
}

// TestWithParamsEmptyMapClears locks in that WithParams(empty map) also clears
// Params to nil, same as nil input.
func TestWithParamsEmptyMapClears(t *testing.T) {
	e := New("c", "m").WithParam("a", 1)
	e.WithParams(map[string]any{})
	if e.Params != nil {
		t.Errorf("WithParams(empty map) left Params = %v, want nil", e.Params)
	}
}

// TestWithParamsSetsNonEmpty locks in that WithParams with a non-empty map
// stores it verbatim (replacing whatever was there).
func TestWithParamsSetsNonEmpty(t *testing.T) {
	e := New("c", "m").WithParam("a", 1)
	e.WithParams(map[string]any{"b": 2})
	want := map[string]any{"b": 2}
	if !reflect.DeepEqual(e.Params, want) {
		t.Errorf("WithParams(non-empty) Params = %v, want %v", e.Params, want)
	}
}

// TestWithParamsNilReceiver locks in the nil guard for WithParams.
func TestWithParamsNilReceiver(t *testing.T) {
	var e *Error
	if got := e.WithParams(map[string]any{"a": 1}); got != nil {
		t.Errorf("nil *Error WithParams() = %v, want nil", got)
	}
}

// TestConstructorsAndChain locks in the basic constructors and that WriteParam
// accumulates into a freshly allocated map.
func TestConstructorsAndChain(t *testing.T) {
	e := New("c", "m")
	if e.Code != "c" || e.Message != "m" {
		t.Errorf("New = %v, want code c message m", e)
	}
	if got := Newf("c", "v=%d", 5).Error(); got != "v=5" {
		t.Errorf("Newf Error() = %q, want v=5", got)
	}
	if got := Wrapf("c", errors.New("inner"), "w=%s", "x").Error(); got != "w=x" {
		t.Errorf("Wrapf Error() = %q, want w=x", got)
	}
	if got := Wrap("c", "m", errors.New("inner")).Error(); got != "m" {
		t.Errorf("Wrap Error() = %q, want m", got)
	}

	chained := New("c", "m").WithParam("a", 1).WithParam("b", 2)
	want := map[string]any{"a": 1, "b": 2}
	if !reflect.DeepEqual(chained.Params, want) {
		t.Errorf("chained Params = %v, want %v", chained.Params, want)
	}
}

// TestCodeExtractor locks in Code() across inputs: *Error with code, *Error with
// empty code, wrapped *Error, plain error, and nil.
func TestCodeExtractor(t *testing.T) {
	appErr := New("my.code", "m")
	if got := Code(appErr); got != "my.code" {
		t.Errorf("Code(*Error) = %q, want my.code", got)
	}
	if got := Code(New("", "m")); got != "" {
		t.Errorf("Code(*Error empty code) = %q, want empty", got)
	}
	if got := Code(fmt.Errorf("wrap: %w", appErr)); got != "my.code" {
		t.Errorf("Code(wrapped *Error) = %q, want my.code (errors.As pierces the chain)", got)
	}
	if got := Code(errors.New("plain")); got != "" {
		t.Errorf("Code(plain error) = %q, want empty", got)
	}
	if got := Code(nil); got != "" {
		t.Errorf("Code(nil) = %q, want empty", got)
	}
}

// TestMessageExtractor locks in Message() behavior: it returns the *Error's
// Error() when present, and the underlying error's message for plain errors.
func TestMessageExtractor(t *testing.T) {
	if got := Message(New("c", "m")); got != "m" {
		t.Errorf("Message(*Error) = %q, want m", got)
	}
	if got := Message(New("c", "")); got != "c" {
		t.Errorf("Message(*Error empty message) = %q, want code c", got)
	}
	if got := Message(errors.New("plain")); got != "plain" {
		t.Errorf("Message(plain error) = %q, want plain", got)
	}
	if got := Message(nil); got != "" {
		t.Errorf("Message(nil) = %q, want empty", got)
	}
}

// TestStatusExtractor locks in Status(): it returns the *Error's Status, and 0
// for plain errors and nil.
func TestStatusExtractor(t *testing.T) {
	if got := Status(New("c", "m").WithStatus(418)); got != 418 {
		t.Errorf("Status(*Error) = %d, want 418", got)
	}
	if got := Status(New("c", "m")); got != 0 {
		t.Errorf("Status(*Error no status) = %d, want 0", got)
	}
	// plain error: 0, not err.Error()
	if got := Status(errors.New("boom")); got != 0 {
		t.Errorf("Status(plain error) = %d, want 0", got)
	}
	if got := Status(nil); got != 0 {
		t.Errorf("Status(nil) = %d, want 0", got)
	}
}

// TestParamsExtractor locks in Params(): it returns the *Error's Params, and nil
// for plain errors and nil.
func TestParamsExtractor(t *testing.T) {
	e := New("c", "m").WithParam("a", 1)
	if got := Params(e); !reflect.DeepEqual(got, map[string]any{"a": 1}) {
		t.Errorf("Params(*Error) = %v, want map[a:1]", got)
	}
	if got := Params(errors.New("boom")); got != nil {
		t.Errorf("Params(plain error) = %v, want nil", got)
	}
	if got := Params(nil); got != nil {
		t.Errorf("Params(nil) = %v, want nil", got)
	}
}

// TestIsCode locks in IsCode across inputs.
func TestIsCode(t *testing.T) {
	if !IsCode(New("c", "m"), "c") {
		t.Error("IsCode(matching code) = false, want true")
	}
	if IsCode(New("c", "m"), "other") {
		t.Error("IsCode(non-matching code) = true, want false")
	}
	if IsCode(errors.New("boom"), "c") {
		t.Error("IsCode(plain error) = true, want false")
	}
	if IsCode(nil, "c") {
		t.Error("IsCode(nil) = true, want false")
	}
}

// TestInvalidJSON locks in InvalidJSON's default message and status.
func TestInvalidJSON(t *testing.T) {
	e := InvalidJSON("")
	if e.Code != CodeCommonInvalidJSON || e.Status != http.StatusBadRequest {
		t.Errorf("InvalidJSON(\"\") = code %q status %d, want %q %d", e.Code, e.Status, CodeCommonInvalidJSON, http.StatusBadRequest)
	}
	if got := e.Message; got != "Invalid JSON format" {
		t.Errorf("InvalidJSON(\"\") default message = %q, want %q", got, "Invalid JSON format")
	}
	if got := InvalidJSON("custom").Message; got != "custom" {
		t.Errorf("InvalidJSON(\"custom\") message = %q, want custom", got)
	}
}

// TestInvalidParam locks in InvalidParam's default message and status.
func TestInvalidParam(t *testing.T) {
	e := InvalidParam("")
	if e.Code != CodeCommonInvalidParam || e.Status != http.StatusBadRequest {
		t.Errorf("InvalidParam(\"\") = code %q status %d, want %q %d", e.Code, e.Status, CodeCommonInvalidParam, http.StatusBadRequest)
	}
	if got := e.Message; got != "Invalid parameter" {
		t.Errorf("InvalidParam(\"\") default message = %q, want %q", got, "Invalid parameter")
	}
}
