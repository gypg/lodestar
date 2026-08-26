package user

/*
WO-017 门 A：users.quota / users.used_quota 的写入必须走漏斗。

为什么要有这道门：余额变动散落在 5 个包里各写裸 SQL（epay / Stripe / 兑换码 /
管理员调整 / 订阅购买），任何一处新增写入点都会绕过流水表，于是流水表当天就失真。
静态扫描把"绕过"变成编译期之外的硬失败。

这道门的**已知盲区**（诚实记录，不要以为它是完备的）：
  - 只认字面量写法。`Update(colName, ...)` 里 colName 是变量时抓不到。
  - 抓不到 Raw SQL（`db.Exec("UPDATE users SET quota = ...")`）。
  - 抓不到通过 struct 整体 Save/Updates(&user) 的写入。
配套的行为测试（门 B + 各调用点的接线测试）才是真守卫，这道门只挡"顺手再写一处裸 SQL"。
*/

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// quotaWritePatterns 匹配对 quota / used_quota 列的直接写入。
//
// 刻意不匹配裸 `"quota":` —— 那会命中 gin.H 响应体这类读路径。所选的四条模式已
// 覆盖 §2 表格里的全部 7 处写入点，且对 `json:"quota"` / `data["quota"]` /
// `Select("quota", ...)` / `{Key: "quota", ...}` 全部不误报。
var quotaWritePatterns = []string{
	`gorm.Expr("quota`,
	`gorm.Expr("used_quota`,
	`Update("quota"`,
	`Update("used_quota"`,
}

// allowedQuotaWriteLines 是白名单：文件 → 允许命中的行数上限。
//
// 用行数上限而不是单纯的文件白名单，是为了让"在已白名单的文件里再加一处写入"
// 也会触发失败 —— 否则白名单本身就是绕过门的方法。
var allowedQuotaWriteLines = map[string]int{
	// 漏斗自身，是唯一被允许的离散变动写入点。
	"internal/op/user/ledger.go": 4,

	// SettleUsage 的两行（quota -= amount / used_quota += amount）。
	// 用量结算刻意不走漏斗：它在热路径上每请求一次，而 used_quota 已经精确累计了
	// 累计钱包消耗，不变式 quota == Σledger.delta - used_quota 因此仍然闭合。
	// 详见工单 §3。
	"internal/op/user/quota.go": 2,
}

func TestGateA_noDirectQuotaWritesOutsideFunnel(t *testing.T) {
	root := repoRoot(t)

	hits := map[string][]string{} // 相对路径 → 命中行的描述

	scanDir := filepath.Join(root, "internal")
	err := filepath.Walk(scanDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		for i, line := range strings.Split(string(data), "\n") {
			for _, pat := range quotaWritePatterns {
				if strings.Contains(line, pat) {
					hits[rel] = append(hits[rel],
						formatHit(rel, i+1, strings.TrimSpace(line)))
					break // 一行只计一次，避免 Update(...gorm.Expr(...)) 重复计数
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", scanDir, err)
	}

	var violations []string
	for _, rel := range sortedKeys(hits) {
		allowed := allowedQuotaWriteLines[rel]
		found := len(hits[rel])
		if found <= allowed {
			continue
		}
		if allowed == 0 {
			violations = append(violations,
				"【未走漏斗】"+rel+" 有 "+strconv.Itoa(found)+" 处直接写入：")
		} else {
			violations = append(violations,
				"【超出白名单】"+rel+" 允许 "+strconv.Itoa(allowed)+" 处，实际 "+strconv.Itoa(found)+" 处：")
		}
		violations = append(violations, hits[rel]...)
	}

	if len(violations) > 0 {
		t.Fatalf("quota 写入必须走 user.MutateQuota 漏斗（WO-017）。违规：\n%s\n\n"+
			"修法：把该处改为在同一事务内调用 user.MutateQuota(tx, userID, delta, entry)；\n"+
			"若确属用量结算这类刻意例外，在 allowedQuotaWriteLines 里显式提高上限并写明理由。",
			strings.Join(violations, "\n"))
	}
}

// repoRoot 从当前测试所在目录向上找 go.mod，避免把 ../../.. 写死。
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
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

func formatHit(rel string, line int, text string) string {
	return "  " + rel + ":" + strconv.Itoa(line) + "\t" + text
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
