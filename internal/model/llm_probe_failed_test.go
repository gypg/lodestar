package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestProbedFailedModelCarriesTimestamp 是 TestHealthyModelOmitsProbeFailedAt
// 的对称另一半：真正探测失败的模型必须带上时刻，否则「显示全部」视图无从标识、
// 隐藏机制整体失效。只有两条同时在，才能既防"全部误判为失败"（隐藏所有可用模型）
// 也防"改成永不输出"（隐藏机制形同虚设）这两个相反方向的缺陷。
//
// 这条必须写 &failedAt，所以字段被改回值类型时它是编译失败而非断言失败——
// 那不算杀。它独占本文件正是为了不把同包的健康测试一起拖进编译错误，
// 后者才是承担"杀掉值类型回退"职责的那条。
func TestProbedFailedModelCarriesTimestamp(t *testing.T) {
	failedAt := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	b, err := json.Marshal(ModelMarketItem{Name: "dead-model", ProbeFailedAt: &failedAt})
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
