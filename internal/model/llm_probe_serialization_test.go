package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestHealthyModelOmitsProbeFailedAt 钉死「健康模型的 JSON 里不能出现
// probe_failed_at」这一条。
//
// 缺陷原貌（WO-033 生产实测）：ProbeFailedAt 曾是非指针 time.Time 配
// `json:"...,omitempty"`。omitempty 对结构体无效（结构体永远不算 empty），
// 于是每个健康模型都序列化出 "probe_failed_at":"0001-01-01T00:00:00Z"。
// 前端三处判的都是 truthiness（web/src/components/modules/model/index.tsx 的
// probedBadCount 与默认视图过滤、Item.tsx、MobileModelItem.tsx），
// 非空字符串为真 → 广场默认视图把全部模型判为探测失败并隐藏。
// 生产侧的证据：model_probe_enabled=false、model_probe_states 零行
// （探测从未运行），却有 94 个模型被隐藏，含真实出活过的 Qwen3-Max。
//
// ★ 本条**故意不使用指针语法**，独占一个文件。把字段改回值类型时，这条测试仍能
// 编译，于是失败方式是"断言不通过"而不是"整包编译失败"——编译失败不算杀
// （记忆 lodestar-mutation-test-validity）。对称的另一半（失败模型必须带时刻）
// 必然要写 &t，放在 llm_probe_failed_test.go 里，两者互不牵连。
func TestHealthyModelOmitsProbeFailedAt(t *testing.T) {
	b, err := json.Marshal(ModelMarketItem{Name: "Qwen3-Max"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)

	if strings.Contains(got, "probe_failed_at") {
		t.Errorf(
			"healthy model serialised probe_failed_at, so the frontend truthiness check hides it.\n"+
				"omitempty does not work on a non-pointer time.Time -- the field must be *time.Time.\ngot: %s",
			got,
		)
	}
}

// TestModelMarketItemIsNotPersisted 钉死改指针无需数据迁移的前提：
// 这个 struct 是纯响应体。若有人给它接上真表名，指针字段的 GORM 语义
// （nil 写 NULL）就成了需要单独审的问题，此时这条测试会提醒他。
func TestModelMarketItemIsNotPersisted(t *testing.T) {
	if got := (ModelMarketItem{}).TableName(); got != "-" {
		t.Errorf(
			"ModelMarketItem.TableName() = %q, want \"-\": it is a response-only shape. "+
				"If it became a real table, the *time.Time probe field needs a migration review.",
			got,
		)
	}
}
