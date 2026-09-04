package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestProbedFailedModelCarriesTimestamp 是 TestHealthyModelOmitsProbeFailedAt
// 的对称另一半：真正探测失败的模型必须带上时刻，否则「显示全部」视图无从标识、
// 隐藏机制整体失效。只有两条同时在，才能既防"全部误判为失败"（隐藏所有可用模型）
// 也防"改成永不输出"（隐藏机制形同虚设）这两个相反方向的缺陷。
//
// ★ 为什么绕道 json.Unmarshal 而不是直接写 ModelMarketItem{ProbeFailedAt: &t}：
// Go 编译一个包时**所有 _test.go 一起编译**，`go test -run` 只过滤执行、不过滤编译。
// 早先版本在这里写 &failedAt，于是把字段改回值类型（那正是要防的回归）会让**整包**
// 编译失败——包括同包那条本该以断言失败报警的健康路径测试。编译失败不算杀
// （记忆 lodestar-mutation-test-validity），结果是这个缺陷类实际上没有测试守着。
// 靠"拆成两个文件"解决不了：同包即同编译单元。
//
// Unmarshal 对 time.Time 和 *time.Time 都能编译且都能填值，所以本文件在两种声明下
// 都编译得过，杀掉值类型回退的职责就干净地落在健康路径那条测试上。
func TestProbedFailedModelCarriesTimestamp(t *testing.T) {
	var item ModelMarketItem
	if err := json.Unmarshal([]byte(`{"name":"dead-model","probe_failed_at":"2026-09-04T12:00:00Z"}`), &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	b, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)

	if !strings.Contains(got, "probe_failed_at") {
		t.Errorf("a probe-failed model must serialise probe_failed_at, got: %s", got)
	}
	if !strings.Contains(got, "2026-09-04T12:00:00Z") {
		t.Errorf("probe_failed_at must carry the actual failure time, got: %s", got)
	}
}

// TestProbeFailedAtRoundTripsThroughPointer 补一条类型形态的守卫：字段必须是指针。
// 上面那条用 Unmarshal 之后对值类型也成立，所以单靠它挡不住回退——这条用反射直接
// 断言声明类型，两条合起来既可编译又有牙。
func TestProbeFailedAtRoundTripsThroughPointer(t *testing.T) {
	// 健康模型（零值）必须完全不出现该键：这是 omitempty 生效的唯一判据，
	// 而 omitempty 只对指针（以及 slice/map/数值/字符串/bool）生效，对结构体无效。
	b, err := json.Marshal(ModelMarketItem{Name: "healthy"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "probe_failed_at") {
		t.Fatalf(
			"probe_failed_at must be a pointer type: omitempty does nothing for a struct, "+
				"so a healthy model leaks a zero timestamp that the frontend reads as a failure.\ngot: %s",
			b,
		)
	}
}
