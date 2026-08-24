package xregexp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 这道门挡住本包存在的理由被绕过:任何直接 regexp2.Compile 的新调用点都会得到一个
// 没有 MatchTimeout 的回溯正则,而这正是 4 个活匹配点曾经的状态。
//
// 之所以要门而不是只靠 code review:超时缺失不会让任何测试变红、不会报错、
// 也不会在日志里留痕 —— 它只在遇到灾难性模式时把请求 goroutine 钉住。
//
// 唯一允许的例外见 allowed。
func TestNoDirectRegexp2CompileOutsideThisPackage(t *testing.T) {
	root := repoRoot(t)

	// 本包自己必须直接调 —— 它就是那个包装。
	allowed := map[string]bool{
		filepath.Join("internal", "utils", "xregexp", "xregexp.go"): true,
	}

	// 拼接构造，否则本文件自己就会成为一个"违规样本"。
	needle := "regexp2" + ".Compile("

	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "web", "static", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if allowed[rel] {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for i, line := range strings.Split(string(content), "\n") {
			trimmed := strings.TrimSpace(line)
			// 注释里提到不算违规 —— 本包的文档注释就提到了它。
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if strings.Contains(line, needle) {
				offenders = append(offenders, rel+":"+itoa(i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(offenders) > 0 {
		t.Fatalf("these sites compile regexp2 directly and therefore get no MatchTimeout;\n"+
			"use xregexp.CompileECMAScript instead:\n  %s", strings.Join(offenders, "\n  "))
	}
}

// repoRoot 从测试工作目录向上找 go.mod。写成向上查找而不是写死相对层数，
// 这样本包换位置时这道门不会静默失效。
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate go.mod above the test working directory")
		}
		dir = parent
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
