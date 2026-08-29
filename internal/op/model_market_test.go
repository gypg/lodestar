package op

import (
	"testing"
	"time"

	"github.com/gypg/lodestar/internal/model"
)

func TestBuildModelMarket_AggregatesChannelsKeysAndStats(t *testing.T) {
	items, summary := buildModelMarket(
		[]model.LLMInfo{
			{
				Name: "gpt-5.2",
				LLMPrice: model.LLMPrice{
					Input:      1,
					Output:     2,
					CacheRead:  0.1,
					CacheWrite: 0.2,
				},
			},
		},
		[]model.LLMChannel{
			{Name: "gpt-5.2", ChannelID: 1, ChannelName: "NMapi", Enabled: true},
			{Name: "gpt-5.2", ChannelID: 2, ChannelName: "Ygxz", Enabled: false},
		},
		map[int]model.Channel{
			1: {ID: 1, Enabled: true, Keys: []model.ChannelKey{{Enabled: true}, {Enabled: true}, {Enabled: false}}},
			2: {ID: 2, Enabled: false, Keys: []model.ChannelKey{{Enabled: true}}},
		},
		[]model.StatsModel{
			{ID: 1, Name: "gpt-5.2", ChannelID: 1, StatsMetrics: model.StatsMetrics{WaitTime: 3000, RequestSuccess: 9, RequestFailed: 1}},
			{ID: 2, Name: "gpt-5.2", ChannelID: 2, StatsMetrics: model.StatsMetrics{WaitTime: 1000, RequestSuccess: 1, RequestFailed: 1}},
		},
		time.Date(2026, 4, 29, 10, 0, 0, 0, time.FixedZone("CST", 8*3600)),
	)

	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].ChannelCount != 2 {
		t.Fatalf("ChannelCount = %d, want 2", items[0].ChannelCount)
	}
	if items[0].EnabledKeyCount != 3 {
		t.Fatalf("EnabledKeyCount = %d, want 3", items[0].EnabledKeyCount)
	}
	if items[0].AverageLatencyMS != 333 {
		t.Fatalf("AverageLatencyMS = %d, want 333", items[0].AverageLatencyMS)
	}
	if items[0].SuccessRate != 0.8333333333333334 {
		t.Fatalf("SuccessRate = %v, want 0.8333333333333334", items[0].SuccessRate)
	}
	if summary.UniqueChannelCount != 2 {
		t.Fatalf("UniqueChannelCount = %d, want 2", summary.UniqueChannelCount)
	}
}

func TestBuildModelMarket_SortsItemsBySuccessRateThenSuccessCount(t *testing.T) {
	items, _ := buildModelMarket(
		[]model.LLMInfo{
			{Name: "z-model"},
			{Name: "a-model"},
			{Name: "b-model"},
			{Name: "c-model"},
		},
		nil,
		nil,
		[]model.StatsModel{
			{ID: 1, Name: "z-model", StatsMetrics: model.StatsMetrics{RequestSuccess: 8, RequestFailed: 2}},
			{ID: 2, Name: "a-model", StatsMetrics: model.StatsMetrics{RequestSuccess: 4, RequestFailed: 0}},
			{ID: 3, Name: "b-model", StatsMetrics: model.StatsMetrics{RequestSuccess: 6, RequestFailed: 0}},
		},
		time.Time{},
	)

	if len(items) != 4 {
		t.Fatalf("len(items) = %d, want 4", len(items))
	}
	if items[0].Name != "b-model" || items[1].Name != "a-model" || items[2].Name != "z-model" || items[3].Name != "c-model" {
		t.Fatalf("unexpected item order: %+v", items)
	}
}

func TestBuildModelMarket_UsesEmptyChannelsSliceWhenModelHasNoChannels(t *testing.T) {
	items, _ := buildModelMarket(
		[]model.LLMInfo{
			{Name: "standalone-model"},
		},
		nil,
		nil,
		nil,
		time.Time{},
	)

	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Channels == nil {
		t.Fatal("Channels = nil, want empty slice")
	}
	if len(items[0].Channels) != 0 {
		t.Fatalf("len(Channels) = %d, want 0", len(items[0].Channels))
	}
}

// The registry lowercases every name on insert, so a site model synced as
// "Qwen/Qwen3-8B" is stored as "qwen/qwen3-8b" and used to render that way --
// visibly inconsistent with the site channel page, which reads site_models
// directly. DisplayName carries the channel's spelling back to the UI.
func TestBuildModelMarket_DisplayNameKeepsUpstreamCasing(t *testing.T) {
	items, _ := buildModelMarket(
		[]model.LLMInfo{
			{Name: "qwen/qwen3-8b"},
		},
		[]model.LLMChannel{
			{Name: "Qwen/Qwen3-8B", ChannelID: 7, ChannelName: "site/acct/default-oai", Enabled: true},
		},
		map[int]model.Channel{
			7: {ID: 7, Enabled: true, Keys: []model.ChannelKey{{Enabled: true}}},
		},
		nil,
		time.Time{},
	)

	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].DisplayName != "Qwen/Qwen3-8B" {
		t.Fatalf("DisplayName = %q, want %q", items[0].DisplayName, "Qwen/Qwen3-8B")
	}
	// Name must stay the lowercased registry key: the UI addresses update and
	// delete mutations by it, and exact group lookup is case-sensitive.
	if items[0].Name != "qwen/qwen3-8b" {
		t.Fatalf("Name = %q, want %q", items[0].Name, "qwen/qwen3-8b")
	}
}

// A model in the registry that no channel advertises has no upstream spelling to
// borrow; DisplayName stays empty and the UI falls back to Name.
func TestBuildModelMarket_DisplayNameEmptyWithoutChannel(t *testing.T) {
	items, _ := buildModelMarket(
		[]model.LLMInfo{{Name: "orphan-model"}},
		nil,
		nil,
		nil,
		time.Time{},
	)

	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].DisplayName != "" {
		t.Fatalf("DisplayName = %q, want empty", items[0].DisplayName)
	}
}

// The first channel to advertise a model decides its display casing, so the
// result does not flip between renders when two channels disagree.
func TestBuildModelMarket_DisplayNameStableAcrossDisagreeingChannels(t *testing.T) {
	items, _ := buildModelMarket(
		[]model.LLMInfo{{Name: "gpt-5.2"}},
		[]model.LLMChannel{
			{Name: "GPT-5.2", ChannelID: 1, ChannelName: "first", Enabled: true},
			{Name: "gpt-5.2", ChannelID: 2, ChannelName: "second", Enabled: true},
		},
		map[int]model.Channel{
			1: {ID: 1, Enabled: true},
			2: {ID: 2, Enabled: true},
		},
		nil,
		time.Time{},
	)

	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].DisplayName != "GPT-5.2" {
		t.Fatalf("DisplayName = %q, want %q (first channel wins)", items[0].DisplayName, "GPT-5.2")
	}
	if items[0].ChannelCount != 2 {
		t.Fatalf("ChannelCount = %d, want 2", items[0].ChannelCount)
	}
}
