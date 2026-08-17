package llm

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

// setupPriceCategoryTestDB 为价格分类测试初始化一个内存 SQLite 库并清空全局缓存。
func setupPriceCategoryTestDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	// 每个测试从空缓存开始，避免读到其他测试遗留的快照。
	priceCategoryCache.Store(nil)
	t.Cleanup(func() {
		priceCategoryCache.Store(nil)
	})
}

func TestPriceCategoryCategoryMatches(t *testing.T) {
	cases := []struct {
		name      string
		ruleType  string
		ruleValue string
		modelName string
		want      bool
	}{
		{"exact hit", "exact", "glm-4.7", "glm-4.7", true},
		{"exact miss", "exact", "glm-4.7", "glm-4.7-flash", false},
		{"exact case-insensitive rule value", "exact", "  GLM-4.7  ", "glm-4.7", true},
		{"prefix hit", "prefix", "glm-", "glm-4.7-flash", true},
		{"prefix miss", "prefix", "gpt-", "glm-4.7", false},
		{"contains hit", "contains", "flash", "glm-4.7-flash", true},
		{"contains miss", "contains", "flash", "glm-4.7", false},
		{"empty rule value never matches", "contains", "  ", "glm-4.7", false},
		{"unknown rule falls back to contains", "glob", "flash", "glm-4.7-flash", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := model.ModelPriceCategory{RuleType: tc.ruleType, RuleValue: tc.ruleValue}
			if got := categoryMatches(c, tc.modelName); got != tc.want {
				t.Fatalf("categoryMatches(rule=%q value=%q model=%q) = %v, want %v",
					tc.ruleType, tc.ruleValue, tc.modelName, got, tc.want)
			}
		})
	}
}

func TestPriceCategoryMatchSortOrderAndEnabled(t *testing.T) {
	setupPriceCategoryTestDB(t)
	ctx := context.Background()

	// sort_order=10 的 contains 规则先建，sort_order=1 的 prefix 规则后建；
	// 匹配必须按 sort_order 而非插入顺序。
	if _, err := CreatePriceCategory(model.ModelPriceCategory{
		Name: "catch-all-flash", RuleType: "contains", RuleValue: "flash",
		LLMPrice: model.LLMPrice{Input: 9, Output: 9}, SortOrder: 10, Enabled: true,
	}, ctx); err != nil {
		t.Fatalf("create catch-all-flash: %v", err)
	}
	if _, err := CreatePriceCategory(model.ModelPriceCategory{
		Name: "glm-prefix", RuleType: "prefix", RuleValue: "glm-",
		LLMPrice: model.LLMPrice{Input: 1, Output: 2}, SortOrder: 1, Enabled: true,
	}, ctx); err != nil {
		t.Fatalf("create glm-prefix: %v", err)
	}
	// 禁用的分类即使 sort_order 最小也不参与匹配。
	if _, err := CreatePriceCategory(model.ModelPriceCategory{
		Name: "disabled-exact", RuleType: "exact", RuleValue: "glm-4.7",
		LLMPrice: model.LLMPrice{Input: 100, Output: 100}, SortOrder: 0, Enabled: false,
	}, ctx); err != nil {
		t.Fatalf("create disabled-exact: %v", err)
	}

	// glm-4.7 命中 sort_order=1 的 glm-prefix，而非 disabled-exact。
	p := PriceCategoryMatch("glm-4.7")
	if p == nil {
		t.Fatal("PriceCategoryMatch(glm-4.7) = nil, want glm-prefix price")
	}
	if p.Input != 1 || p.Output != 2 {
		t.Fatalf("PriceCategoryMatch(glm-4.7) = %+v, want glm-prefix {Input:1 Output:2}", p)
	}

	// xyz-flash 不命中 glm-prefix，落到 sort_order=10 的 catch-all。
	p = PriceCategoryMatch("xyz-flash")
	if p == nil || p.Input != 9 {
		t.Fatalf("PriceCategoryMatch(xyz-flash) = %+v, want catch-all-flash {Input:9}", p)
	}

	// 完全不命中。
	if p := PriceCategoryMatch("totally-unknown"); p != nil {
		t.Fatalf("PriceCategoryMatch(totally-unknown) = %+v, want nil", p)
	}
}

func TestPriceCategoryCRUDRefreshesCache(t *testing.T) {
	setupPriceCategoryTestDB(t)
	ctx := context.Background()

	created, err := CreatePriceCategory(model.ModelPriceCategory{
		Name: "glm-prefix", RuleType: "prefix", RuleValue: "glm-",
		LLMPrice: model.LLMPrice{Input: 1, Output: 2}, SortOrder: 0, Enabled: true,
	}, ctx)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Name != "glm-prefix" {
		t.Fatalf("created.Name = %q, want lowercased glm-prefix", created.Name)
	}
	if p := PriceCategoryMatch("glm-anything"); p == nil || p.Input != 1 {
		t.Fatalf("PriceCategoryMatch after create = %+v, want {Input:1}", p)
	}

	// 更新价格后缓存即时生效。
	created.Input = 5
	if _, err := UpdatePriceCategory(created, ctx); err != nil {
		t.Fatalf("update: %v", err)
	}
	if p := PriceCategoryMatch("glm-anything"); p == nil || p.Input != 5 {
		t.Fatalf("PriceCategoryMatch after update = %+v, want {Input:5}", p)
	}

	// 删除后不再命中。
	if err := DeletePriceCategory(created.ID, ctx); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if p := PriceCategoryMatch("glm-anything"); p != nil {
		t.Fatalf("PriceCategoryMatch after delete = %+v, want nil", p)
	}
}

func TestPriceCategoryValidation(t *testing.T) {
	setupPriceCategoryTestDB(t)
	ctx := context.Background()

	cases := []struct {
		name string
		cat  model.ModelPriceCategory
	}{
		{"empty name", model.ModelPriceCategory{Name: "  ", RuleType: "contains", RuleValue: "x"}},
		{"invalid rule type", model.ModelPriceCategory{Name: "n", RuleType: "glob", RuleValue: "x"}},
		{"empty rule value", model.ModelPriceCategory{Name: "n", RuleType: "contains", RuleValue: "  "}},
		{"negative price", model.ModelPriceCategory{Name: "n", RuleType: "contains", RuleValue: "x", LLMPrice: model.LLMPrice{Input: -1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := CreatePriceCategory(tc.cat, ctx); err == nil {
				t.Fatal("CreatePriceCategory error = nil, want validation error")
			}
		})
	}
}

func TestPriceCategoryAutoMigrateCreatesTable(t *testing.T) {
	setupPriceCategoryTestDB(t)
	if !db.GetDB().Migrator().HasTable(&model.ModelPriceCategory{}) {
		t.Fatal("AutoMigrate did not create model_price_categories table")
	}
	// ListPriceCategories 在空表上应返回空切片而非错误。
	rows, err := ListPriceCategories(context.Background())
	if err != nil {
		t.Fatalf("ListPriceCategories on empty table: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("ListPriceCategories on empty table returned %d rows, want 0", len(rows))
	}
}
