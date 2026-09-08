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
