package task

import (
	"context"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/llm"
)

// SyncModelsTask treats the model registry as if AutoSync channels were its only
// legitimate source: it diffs the whole registry against the models it just
// fetched and deletes every leftover that carries no price.
//
// Site-projected channels are built with AutoSync:false
// (internal/sitesync/project.go), so the fetch loop skips them
// (internal/task/sync.go:72) and none of their models appear in totalNewModels.
// Site models with no known upstream price are inserted at zero price
// (internal/helper/price.go:24-26), which is exactly the deletion predicate.
//
// Net effect before the fix: every zero-priced site model is deleted from the
// registry on each sync run, so it vanishes from 模型广场 while still rendering
// on the site-channel page (which reads site_models directly).
func TestSyncModelsTaskKeepsModelsOfNonAutoSyncChannels(t *testing.T) {
	ctx := context.Background()
	if err := db.InitDB("sqlite", "file:task_sync_site_model_retention?mode=memory&cache=shared", false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}

	// A projected site channel: enabled, keyed, but AutoSync:false exactly as
	// sitesync builds it. Its model is therefore never fetched by the task.
	projected := &model.Channel{
		Name:     "site/acct/default-oai",
		Enabled:  true,
		AutoSync: false,
		Model:    "site-only-model",
		BaseUrls: []model.BaseUrl{{URL: "https://upstream.example/v1"}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-test"}},
	}
	if err := op.ChannelCreate(projected, ctx); err != nil {
		t.Fatalf("ChannelCreate: %v", err)
	}

	// Registered at zero price, which is what LLMPriceAddToDB does for a model
	// with no known price.
	if err := llm.BatchCreate([]model.LLMInfo{{Name: "site-only-model"}}, ctx); err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}
	if _, err := llm.Get("site-only-model"); err != nil {
		t.Fatalf("precondition failed, model not registered: %v", err)
	}

	SyncModelsTask()

	if _, err := llm.Get("site-only-model"); err != nil {
		t.Fatalf("site model was deleted from the registry by SyncModelsTask: %v", err)
	}
}

// The retention must not become a blanket "never delete anything": a model whose
// only channel is gone entirely, and which carries no price, is still garbage the
// task is supposed to collect.
func TestSyncModelsTaskStillDeletesModelWithNoChannelAtAll(t *testing.T) {
	ctx := context.Background()
	if err := db.InitDB("sqlite", "file:task_sync_orphan_deletion?mode=memory&cache=shared", false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}

	// A channel must exist and declare SOMETHING, otherwise the "no channel
	// declares anything" early-return short-circuits the filter and this test
	// stops exercising it at all.
	other := &model.Channel{
		Name:     "site/acct/other-oai",
		Enabled:  true,
		AutoSync: false,
		Model:    "some-other-model",
		BaseUrls: []model.BaseUrl{{URL: "https://upstream.example/v1"}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-test"}},
	}
	if err := op.ChannelCreate(other, ctx); err != nil {
		t.Fatalf("ChannelCreate: %v", err)
	}

	if err := llm.BatchCreate([]model.LLMInfo{{Name: "orphan-model"}}, ctx); err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}
	if _, err := llm.Get("orphan-model"); err != nil {
		t.Fatalf("precondition failed, model not registered: %v", err)
	}

	SyncModelsTask()

	if _, err := llm.Get("orphan-model"); err == nil {
		t.Fatal("orphan model with no channel and no price should have been collected")
	}
}

// CustomModel is the second place a channel declares models. Site projection
// leaves it empty, but hand-made channels use it, and a model declared only there
// must not be treated as an orphan.
func TestSyncModelsTaskKeepsModelDeclaredOnlyInCustomModel(t *testing.T) {
	ctx := context.Background()
	if err := db.InitDB("sqlite", "file:task_sync_custom_model_retention?mode=memory&cache=shared", false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}

	custom := &model.Channel{
		Name:        "hand-made",
		Enabled:     true,
		AutoSync:    false,
		Model:       "listed-model",
		CustomModel: "custom-only-model",
		BaseUrls:    []model.BaseUrl{{URL: "https://upstream.example/v1"}},
		Keys:        []model.ChannelKey{{Enabled: true, ChannelKey: "sk-test"}},
	}
	if err := op.ChannelCreate(custom, ctx); err != nil {
		t.Fatalf("ChannelCreate: %v", err)
	}

	if err := llm.BatchCreate([]model.LLMInfo{{Name: "custom-only-model"}}, ctx); err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}

	SyncModelsTask()

	if _, err := llm.Get("custom-only-model"); err != nil {
		t.Fatalf("model declared via CustomModel was deleted: %v", err)
	}
}

// Registry keys are lowercased on insert while a channel may declare mixed case.
// Matching must fold case, or a mixed-case declaration fails to protect its own
// registry row.
func TestSyncModelsTaskKeepsModelWhenChannelDeclaresMixedCase(t *testing.T) {
	ctx := context.Background()
	if err := db.InitDB("sqlite", "file:task_sync_case_retention?mode=memory&cache=shared", false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}

	mixed := &model.Channel{
		Name:     "site/acct/default-oai",
		Enabled:  true,
		AutoSync: false,
		Model:    "Qwen/Qwen3-8B",
		BaseUrls: []model.BaseUrl{{URL: "https://upstream.example/v1"}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-test"}},
	}
	if err := op.ChannelCreate(mixed, ctx); err != nil {
		t.Fatalf("ChannelCreate: %v", err)
	}

	// BatchCreate lowercases, mirroring how projection registers it.
	if err := llm.BatchCreate([]model.LLMInfo{{Name: "Qwen/Qwen3-8B"}}, ctx); err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}

	SyncModelsTask()

	if _, err := llm.Get("qwen/qwen3-8b"); err != nil {
		t.Fatalf("mixed-case declaration failed to protect its registry row: %v", err)
	}
}

// A priced model is retained by the existing zero-price predicate regardless of
// channel wiring; asserted so the fix cannot be mistaken for the reason.
func TestSyncModelsTaskKeepsPricedModelWithoutChannel(t *testing.T) {
	ctx := context.Background()
	if err := db.InitDB("sqlite", "file:task_sync_priced_retention?mode=memory&cache=shared", false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}

	if err := llm.BatchCreate([]model.LLMInfo{
		{Name: "priced-model", LLMPrice: model.LLMPrice{Input: 1, Output: 2}},
	}, ctx); err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}

	SyncModelsTask()

	if _, err := llm.Get("priced-model"); err != nil {
		t.Fatalf("priced model was deleted: %v", err)
	}
}
