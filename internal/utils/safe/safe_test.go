package safe

import (
	"strings"
	"testing"
	"time"
)

// TestRunExecutesFn locks in that Run actually invokes the provided function by
// side effect; a captured variable must flip inside fn and be visible after.
func TestRunExecutesFn(t *testing.T) {
	called := false
	Run("x", func() { called = true })
	if !called {
		t.Error("Run did not invoke fn")
	}
}

// TestRunSwallowsPanic locks in that a panicking fn does not crash the test
// process: Run recovers the panic, logs it, and returns normally.
func TestRunSwallowsPanic(t *testing.T) {
	Run("x", func() { panic("boom") })
}

// TestRunNilFnIsSafe locks in the nil-guard: Run("x", nil) must neither panic
// nor invoke anything, and simply return.
func TestRunNilFnIsSafe(t *testing.T) {
	Run("x", nil)
}

// TestGoExecutesAsync locks in the asynchronous behavior of Go: fn runs in a
// separate goroutine, so a bare synchronous check could observe its effect
// before fn runs. A channel with a timeout (never bare <-ch, never Sleep) keeps
// the test from hanging on CI. The goroutine's own panic must not crash the
// process.
func TestGoExecutesAsync(t *testing.T) {
	done := make(chan struct{})
	Go("x", func() {
		defer close(done)
		panic("boom-in-goroutine") // panic must be recovered, not crash process
	})
	select {
	case <-done:
		// fn completed (and its panic was swallowed by Go's runner)
	case <-time.After(2 * time.Second):
		t.Fatal("Go fn did not complete within timeout")
	}
}

// TestRecoverHandlerOnPanic locks in RecoverHandler: when wrapped in defer (note
// the trailing parens - it returns the function to defer), a panic is converted
// into a non-nil error passed to the callback. Only the substring is compared,
// since the error text contains runtime values.
func TestRecoverHandlerOnPanic(t *testing.T) {
	var got error
	defer func() {
		if got == nil {
			t.Error("onPanic did not receive a non-nil error")
		}
		if !strings.Contains(got.Error(), "panic recovered") {
			t.Errorf("error %q does not contain %q", got.Error(), "panic recovered")
		}
		if !strings.Contains(got.Error(), "x") {
			t.Errorf("error %q does not contain %q", got.Error(), "x")
		}
	}()
	defer RecoverHandler("x", func(err error) {
		got = err
	})()
	panic("boom")
}

// TestRecoverHandlerNilOnPanic locks in that passing a nil onPanic is fine: the
// panic is still recovered and no second panic is thrown.
func TestRecoverHandlerNilOnPanic(t *testing.T) {
	defer RecoverHandler("x", nil)()
	panic("boom")
}
