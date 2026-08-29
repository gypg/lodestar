package op

import (
	"context"
	"testing"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/llm"
)

func seedReclaimChannel(t *testing.T, ctx context.Context, name string, models string) *model.Channel {
	t.Helper()
	ch := &model.Channel{
		Name:     name,
		Enabled:  true,
		Model:    models,
		BaseUrls: []model.BaseUrl{{URL: "https://upstream.example/v1"}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-" + name}},
	}
	if err := ChannelCreate(ch, ctx); err != nil {
		t.Fatalf("ChannelCreate(%s): %v", name, err)
	}
	return ch
}

// Deleting a channel left its models in the llm registry forever: the sync task
// only collects zero-priced entries, so a priced model became a permanent shell
// in the model market showing "channels 0 / keys 0". Production had accumulated
// 20+ of these from channels deleted long ago.
func TestChannelDelReclaimsModelsNoOtherChannelOffers(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	ch := seedReclaimChannel(t, ctx, "solo-channel", "solo-model")
	if err := llm.BatchCreate([]model.LLMInfo{
		{Name: "solo-model", LLMPrice: model.LLMPrice{Input: 1, Output: 2}},
	}, ctx); err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}
	if _, err := llm.Get("solo-model"); err != nil {
		t.Fatalf("precondition: model not registered: %v", err)
	}

	if err := ChannelDel(ch.ID, ctx); err != nil {
		t.Fatalf("ChannelDel: %v", err)
	}

	// Priced deliberately: the operator asked for the model to go with its
	// channel, and keeping the price means keeping the shell.
	if _, err := llm.Get("solo-model"); err == nil {
		t.Fatal("model still registered after its only channel was deleted")
	}
}

// The reclaim must be scoped to models nothing else offers. Deleting one channel
// must not strip a model another channel is still serving, which would take that
// channel's traffic down with it.
func TestChannelDelKeepsModelAnotherChannelStillOffers(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	shared := seedReclaimChannel(t, ctx, "channel-a", "shared-model,only-on-a")
	keeper := seedReclaimChannel(t, ctx, "channel-b", "shared-model")

	if err := llm.BatchCreate([]model.LLMInfo{
		{Name: "shared-model"},
		{Name: "only-on-a"},
	}, ctx); err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}

	if err := ChannelDel(shared.ID, ctx); err != nil {
		t.Fatalf("ChannelDel: %v", err)
	}

	if _, err := llm.Get("shared-model"); err != nil {
		t.Fatalf("shared-model was reclaimed while channel %d still offers it: %v", keeper.ID, err)
	}
	if _, err := llm.Get("only-on-a"); err == nil {
		t.Fatal("only-on-a should have been reclaimed with its sole channel")
	}
}

// Two channels may spell the same upstream model differently. The
// still-declared lookup must fold case, or deleting the channel that used one
// spelling reclaims the registry row the survivor still depends on -- silently
// taking that channel's model out of the market.
func TestChannelDelKeepsModelSurvivorDeclaresInDifferentCase(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	// The SURVIVOR must be the mixed-case one. If the survivor spelled it
	// lowercase, a lookup that skipped folding would still match the lowercased
	// candidate and the test would prove nothing.
	deleted := seedReclaimChannel(t, ctx, "channel-lower", "shared-model")
	seedReclaimChannel(t, ctx, "channel-upper", "Shared-Model")

	if err := llm.BatchCreate([]model.LLMInfo{{Name: "shared-model"}}, ctx); err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}

	if err := ChannelDel(deleted.ID, ctx); err != nil {
		t.Fatalf("ChannelDel: %v", err)
	}

	if _, err := llm.Get("shared-model"); err != nil {
		t.Fatalf("survivor declares the same model in another case, yet it was reclaimed: %v", err)
	}
}

// A manually registered model that never had a channel is not in any deleted
// channel's list, so it must survive an unrelated deletion.
func TestChannelDelLeavesManuallyAddedModelAlone(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	ch := seedReclaimChannel(t, ctx, "unrelated-channel", "channel-model")
	if err := llm.BatchCreate([]model.LLMInfo{
		{Name: "channel-model"},
		{Name: "hand-added-model"},
	}, ctx); err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}

	if err := ChannelDel(ch.ID, ctx); err != nil {
		t.Fatalf("ChannelDel: %v", err)
	}

	if _, err := llm.Get("hand-added-model"); err != nil {
		t.Fatalf("hand-added model was reclaimed by an unrelated deletion: %v", err)
	}
}

// Registry keys are lowercased on insert while a channel may declare mixed case;
// the reclaim must fold case or it silently keeps the shell it meant to remove.
func TestChannelDelReclaimsMixedCaseDeclaration(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	ch := seedReclaimChannel(t, ctx, "mixed-case-channel", "Qwen/Qwen3-8B")
	if err := llm.BatchCreate([]model.LLMInfo{{Name: "Qwen/Qwen3-8B"}}, ctx); err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}
	if _, err := llm.Get("qwen/qwen3-8b"); err != nil {
		t.Fatalf("precondition: model not registered lowercased: %v", err)
	}

	if err := ChannelDel(ch.ID, ctx); err != nil {
		t.Fatalf("ChannelDel: %v", err)
	}

	if _, err := llm.Get("qwen/qwen3-8b"); err == nil {
		t.Fatal("mixed-case declaration failed to reclaim its lowercased registry row")
	}
}

// CustomModel is the second place a channel declares models; ignoring it would
// leave those shells behind.
func TestChannelDelReclaimsCustomModel(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	ch := &model.Channel{
		Name:        "custom-decl-channel",
		Enabled:     true,
		Model:       "listed-model",
		CustomModel: "custom-only-model",
		BaseUrls:    []model.BaseUrl{{URL: "https://upstream.example/v1"}},
		Keys:        []model.ChannelKey{{Enabled: true, ChannelKey: "sk-custom"}},
	}
	if err := ChannelCreate(ch, ctx); err != nil {
		t.Fatalf("ChannelCreate: %v", err)
	}
	if err := llm.BatchCreate([]model.LLMInfo{{Name: "custom-only-model"}}, ctx); err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}

	if err := ChannelDel(ch.ID, ctx); err != nil {
		t.Fatalf("ChannelDel: %v", err)
	}

	if _, err := llm.Get("custom-only-model"); err == nil {
		t.Fatal("model declared via CustomModel was not reclaimed")
	}
}
