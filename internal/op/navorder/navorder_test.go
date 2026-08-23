package navorder

import (
	"testing"

	"github.com/gypg/lodestar/internal/utils/semantic_cache"
)

// 这些测试原先住在 internal/op/ops_test.go，走的是 nav_order.go 里的转发壳
// （op.buildSemanticCacheEvaluationSummary）。壳已删除，测试搬到活实现所在的包 ——
// 这里的 BuildSemanticCacheEvaluationSummary 有真实调用点
// （internal/op/analytics/analytics.go:337），此前在该层零覆盖。
//
// ★ 同批搬过来的两个 NormalizeNavOrder 测试已随函数一并删除：那个函数全仓零调用点，
// 把测试搬到一个死函数上并不算"覆盖真实现"。见包注释。
func TestBuildSemanticCacheEvaluationSummary_ComputesRates(t *testing.T) {
	stats := semantic_cache.RuntimeStats{
		EvaluatedRequests: 12,
		CacheHitResponses: 8,
		CacheMissRequests: 3,
		BypassedRequests:  1,
		StoredResponses:   3,
	}
	got := BuildSemanticCacheEvaluationSummary(
		true, true, 3600, 98, 1000, 120, 80, 40, stats,
	)
	// 80 hits / 120 lookups = 66.66…%，120 entries / 1000 max = 12%。
	if got.HitRate != 66.66666666666666 {
		t.Fatalf("HitRate = %v", got.HitRate)
	}
	if got.UsageRate != 12 {
		t.Fatalf("UsageRate = %v", got.UsageRate)
	}
	// 该函数除了算两个比率就只做字段搬运，搬错列同样是缺陷，一并钉住。
	if got.EvaluatedRequests != 12 || got.CacheHitResponses != 8 || got.CacheMissRequests != 3 ||
		got.BypassedRequests != 1 || got.StoredResponses != 3 {
		t.Fatalf("runtime stats were not carried through: %+v", got)
	}
	if got.TTLSeconds != 3600 || got.Threshold != 98 || got.MaxEntries != 1000 ||
		got.CurrentEntries != 120 || got.Hits != 80 || got.Misses != 40 {
		t.Fatalf("config/counter fields were not carried through: %+v", got)
	}
	if !got.Enabled || !got.RuntimeEnabled {
		t.Fatalf("enabled flags were not carried through: %+v", got)
	}
}

func TestBuildSemanticCacheEvaluationSummary_ZeroDenominatorsStayZero(t *testing.T) {
	got := BuildSemanticCacheEvaluationSummary(
		false, false, 0, 0, 0, 0, 0, 0, semantic_cache.RuntimeStats{},
	)
	if got.HitRate != 0 {
		t.Fatalf("HitRate = %v, want 0 when there are no lookups", got.HitRate)
	}
	if got.UsageRate != 0 {
		t.Fatalf("UsageRate = %v, want 0 when maxEntries is 0", got.UsageRate)
	}
}
