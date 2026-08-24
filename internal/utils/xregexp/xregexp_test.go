package xregexp

import (
	"strings"
	"testing"
	"time"
)

func TestCompileECMAScriptAlwaysAttachesTheTimeout(t *testing.T) {
	for _, pattern := range []string{`^gpt-4o`, `.*`, `(a+)+$`, `claude|gemini`} {
		re, err := CompileECMAScript(pattern)
		if err != nil {
			t.Fatalf("CompileECMAScript(%q) error = %v", pattern, err)
		}
		if re.MatchTimeout != MatchTimeout {
			t.Fatalf("CompileECMAScript(%q).MatchTimeout = %v, want %v", pattern, re.MatchTimeout, MatchTimeout)
		}
	}
}

func TestCompileECMAScriptRejectsBadPatterns(t *testing.T) {
	if _, err := CompileECMAScript("("); err == nil {
		t.Fatal("CompileECMAScript(\"(\") error = nil, want a compile error")
	}
}

// 这是本包存在的理由：regexp2 是回溯引擎，没有 MatchTimeout 时一个灾难性模式
// 可以把请求 goroutine 无限期钉住。这里用一个经典的指数回溯模式配一条不匹配的长串，
// 断言它**在超时内返回**而不是挂死。
//
// 判据是「返回了 timeout 错误」，不是「跑得快」—— 后者在慢机器上会假红。
func TestCompiledRegexBailsOutInsteadOfBacktrackingForever(t *testing.T) {
	// (a+)+$ 对一串 a 后跟一个非 a 会指数回溯。
	re, err := CompileECMAScript(`(a+)+$`)
	if err != nil {
		t.Fatalf("CompileECMAScript() error = %v", err)
	}

	input := strings.Repeat("a", 40) + "!"

	start := time.Now()
	_, matchErr := re.MatchString(input)
	elapsed := time.Since(start)

	if matchErr == nil {
		t.Fatal("MatchString() error = nil; the pathological pattern completed, so this input no longer exercises backtracking — pick a harder one")
	}
	if !strings.Contains(strings.ToLower(matchErr.Error()), "timeout") {
		t.Fatalf("MatchString() error = %v, want a timeout error", matchErr)
	}
	// 宽松上限：只要没跑成无限期就行。给 MatchTimeout 留 40 倍余量，避免慢机器假红。
	if elapsed > 40*MatchTimeout {
		t.Fatalf("match took %v, far beyond the %v timeout", elapsed, MatchTimeout)
	}
}
