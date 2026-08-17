package price

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/llm"
)

// setupPriceCategoryFallbackDB 初始化内存库并注册一条启用的分类规则，
// 返回清理函数。
func setupPriceCategoryFallbackDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_ = llm.ResetPriceCategoryCacheForTest()
	})
	_ = llm.ResetPriceCategoryCacheForTest()
	if _, err := llm.CreatePriceCategory(model.ModelPriceCategory{
		Name: "vendor-prefix", RuleType: "prefix", RuleValue: "vendor-",
		LLMPrice: model.LLMPrice{Input: 7, Output: 8}, SortOrder: 0, Enabled: true,
	}, context.Background()); err != nil {
		t.Fatalf("create price category: %v", err)
	}
}

// TestGetLLMPriceCategoryBeforeSubstring 验证兜底链顺序：
// 精确价（presets）→ 分类规则 → 整词子串启发式。
func TestGetLLMPriceCategoryBeforeSubstring(t *testing.T) {
	setupPriceCategoryFallbackDB(t)

	generated := map[string]model.LLMPrice{
		// 精确键，用于验证精确价优先于分类。
		"vendor-x": {Input: 1},
		// 整词子串目标：query "vendor-gpt-fallback" 的子串命中来源。
		"gpt-fallback": {Input: 2},
	}
	restore := setPricesForTest(generated)
	t.Cleanup(restore)

	cases := []struct {
		name      string
		modelName string
		wantInput float64
		wantNil   bool
	}{
		// 精确价最优先：分类规则虽然也命中 vendor-x，但精确键先返回。
		{"exact preset beats category", "vendor-x", 1, false},
		// 分类优先于子串：vendor-gpt-fallback 同时命中分类（前缀 vendor-）和
		// 子串（gpt-fallback），必须返回分类价 7。
		{"category beats substring", "vendor-gpt-fallback", 7, false},
		// 无分类命中时子串兜底仍生效（原行为不回归）。
		{"substring when no category", "other-gpt-fallback", 2, false},
		// 都不命中返回 nil。
		{"no match returns nil", "totally-unknown", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := GetLLMPrice(tc.modelName)
			if tc.wantNil {
				if p != nil {
					t.Fatalf("GetLLMPrice(%q) = %+v, want nil", tc.modelName, p)
				}
				return
			}
			if p == nil {
				t.Fatalf("GetLLMPrice(%q) = nil, want Input %v", tc.modelName, tc.wantInput)
			}
			if p.Input != tc.wantInput {
				t.Fatalf("GetLLMPrice(%q).Input = %v, want %v", tc.modelName, p.Input, tc.wantInput)
			}
		})
	}
}

// TestGetLLMPriceUnknownModelViaCategory 未收录模型（无精确价、无子串命中）
// 走分类规则得到非零价。
func TestGetLLMPriceUnknownModelViaCategory(t *testing.T) {
	setupPriceCategoryFallbackDB(t)
	restore := setPricesForTest(map[string]model.LLMPrice{"gpt-4o": {Input: 5}})
	t.Cleanup(restore)

	p := GetLLMPrice("vendor-brand-new-model")
	if p == nil {
		t.Fatal("GetLLMPrice(vendor-brand-new-model) = nil, want category fallback price")
	}
	if p.Input != 7 || p.Output != 8 {
		t.Fatalf("GetLLMPrice(vendor-brand-new-model) = %+v, want category {Input:7 Output:8}", p)
	}
}

// TestManualPricesSurviveRegeneration 模拟生成脚本重跑：脚本只产出一张全新的
// 生成表，applyManualPrices 必须把手工层重新合并回去（同名覆盖、生成键保留）。
func TestManualPricesSurviveRegeneration(t *testing.T) {
	manual := map[string]model.LLMPrice{
		"manual-only-model": {Input: 3},
		"gpt-overridden":    {Input: 42},
	}
	restoreManual := setManualPricesForTest(manual)
	t.Cleanup(restoreManual)

	// 模拟一次全新生成：只有生成器能产出的键。
	regenerated := map[string]model.LLMPrice{
		"gpt-overridden": {Input: 1}, // 官方数据后来收录，覆盖手工同名键属预期
		"gpt-generated":  {Input: 2},
	}
	merged := applyManualPrices(regenerated)

	if p, ok := merged["manual-only-model"]; !ok || p.Input != 3 {
		t.Fatalf("merged[manual-only-model] = %+v ok=%v, want manual price {Input:3}", p, ok)
	}
	if p, ok := merged["gpt-generated"]; !ok || p.Input != 2 {
		t.Fatalf("merged[gpt-generated] = %+v ok=%v, want generated price preserved", p, ok)
	}
	if p := merged["gpt-overridden"]; p.Input != 42 {
		t.Fatalf("merged[gpt-overridden] = %+v, want manual override {Input:42}", p)
	}
}

// TestPriceGeneratorOnlyWritesGeneratedFile 结构性验证：生成脚本的输出路径
// 只指向 presets.go，绝不触碰 presets_manual.go——重跑脚本不可能丢手工价。
func TestPriceGeneratorOnlyWritesGeneratedFile(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "updatePrice.py"))
	if err != nil {
		t.Fatalf("read updatePrice.py: %v", err)
	}
	src := string(script)
	if strings.Contains(src, "presets_manual") {
		t.Fatal("updatePrice.py references presets_manual.go; generator must never write the manual file")
	}
	if !strings.Contains(src, `"presets.go"`) {
		t.Fatal("updatePrice.py no longer writes presets.go; isolation test needs updating")
	}
}

// setManualPricesForTest 临时替换手工价表，返回恢复函数。
func setManualPricesForTest(prices map[string]model.LLMPrice) func() {
	old := manualLLMPrices
	manualLLMPrices = prices
	return func() {
		manualLLMPrices = old
	}
}
