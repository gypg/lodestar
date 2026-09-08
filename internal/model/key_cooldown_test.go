package model

import "testing"

// WO-044 #238：429 冷却键的 model 段大小写无关（写入/查询两侧经同一
// ToLower），大小写轮换不能洗掉冷却。
func TestKeyModelCooldownCaseInsensitive(t *testing.T) {
	RecordKeyModelCooldown(7, "zen/Foo")

	if !IsKeyModelOnCooldown(7, "zen/FOO", 60) {
		t.Fatal("case variant of cooled-down model reported not cooling — normalization broken")
	}
	if IsKeyModelOnCooldown(8, "zen/Foo", 60) {
		t.Fatal("different key id hit the same cooldown entry")
	}
	if IsKeyModelOnCooldown(7, "zen/Bar", 60) {
		t.Fatal("different model hit the same cooldown entry")
	}
}

// WO-045 阻断 2：Clear 漏 ToLower（WO-044 引入的回归——Record/Is 都归一化了，
// Clear 用原串，大小写混合的模型名 Clear 是 no-op）。生产模型名 Qwen3-Max /
// DeepSeek-V4-Pro 大小写混合必中：429 hold 复试前清不掉冷却，唯一 key 选不回来。
func TestKeyModelCooldownClearCaseInsensitive(t *testing.T) {
	RecordKeyModelCooldown(9, "Qwen3-Max")

	ClearKeyModelCooldown(9, "Qwen3-Max")
	if IsKeyModelOnCooldown(9, "Qwen3-Max", 60) {
		t.Fatal("Clear with mixed-case model name was a no-op — key must be re-selectable after hold")
	}

	// 同一条目的大小写变体也必须清干净。
	RecordKeyModelCooldown(9, "Qwen3-Max")
	ClearKeyModelCooldown(9, "qwen3-MAX")
	if IsKeyModelOnCooldown(9, "QWEN3-max", 60) {
		t.Fatal("variant-cased Clear missed the entry")
	}
}
