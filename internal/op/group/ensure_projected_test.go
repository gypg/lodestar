package group

import (
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

// countGroupItemsForChannel reports how many GroupItem rows name the channel,
// which is what the relay walks to reach it: zero items means unroutable.
func countGroupItemsForChannel(t *testing.T, channelID int) int {
	t.Helper()
	var count int64
	if err := db.GetDB().Model(&model.GroupItem{}).Where("channel_id = ?", channelID).Count(&count).Error; err != nil {
		t.Fatalf("count group items: %v", err)
	}
	return int(count)
}

// The bug this guards: projected site channels were created with
// AutoGroup=None, and the only pass that ran against them
// (ChannelAutoGroupWithMode) merely *matches* pre-existing groups. On an install
// whose groups were never hand-built there is nothing to match, so every site
// model ended up with no GroupItem and the relay answered "group not found".
func TestEnsureCanonicalGroupsForChannel_CreatesGroupsOnEmptyInstall(t *testing.T) {
	ctx := setupGroupTestDB(t)
	groupMap.Clear()
	t.Cleanup(func() {
		groupCache.Clear()
		groupMap.Clear()
	})

	ch := model.Channel{ID: 41, Name: "site/acct/default-oai", Model: "gpt-4o,claude-3-5-sonnet"}

	created, err := EnsureCanonicalGroupsForChannel(ch, ctx)
	if err != nil {
		t.Fatalf("EnsureCanonicalGroupsForChannel: %v", err)
	}
	if created != 2 {
		t.Fatalf("created = %d, want 2 (one per distinct canonical model)", created)
	}
	if got := countGroupItemsForChannel(t, 41); got != 2 {
		t.Fatalf("group items for channel 41 = %d, want 2", got)
	}
}

// Must not disturb groups it did not create: the previous retroactive path
// deleted every non-default group first, which destroyed hand-made groups and
// other sites' groups.
func TestEnsureCanonicalGroupsForChannel_DoesNotDeleteExistingGroups(t *testing.T) {
	ctx := setupGroupTestDB(t)
	groupMap.Clear()
	t.Cleanup(func() {
		groupCache.Clear()
		groupMap.Clear()
	})

	handMade := &model.Group{Name: "my-handmade-group", EndpointType: model.EndpointTypeChat}
	if err := GroupCreate(handMade, ctx); err != nil {
		t.Fatalf("GroupCreate: %v", err)
	}

	ch := model.Channel{ID: 42, Name: "site/acct/default-oai", Model: "gpt-4o"}
	if _, err := EnsureCanonicalGroupsForChannel(ch, ctx); err != nil {
		t.Fatalf("EnsureCanonicalGroupsForChannel: %v", err)
	}

	var survived model.Group
	if err := db.GetDB().Where("name = ?", "my-handmade-group").First(&survived).Error; err != nil {
		t.Fatalf("hand-made group was destroyed: %v", err)
	}
}

// A model that already has a GroupItem for this channel is left alone, so
// running after ChannelAutoGroupWithMode preserves the operator's match choice
// instead of creating a second, competing group.
func TestEnsureCanonicalGroupsForChannel_SkipsAlreadyCoveredModels(t *testing.T) {
	ctx := setupGroupTestDB(t)
	groupMap.Clear()
	t.Cleanup(func() {
		groupCache.Clear()
		groupMap.Clear()
	})

	existing := &model.Group{Name: "operators-choice", EndpointType: model.EndpointTypeChat}
	if err := GroupCreate(existing, ctx); err != nil {
		t.Fatalf("GroupCreate: %v", err)
	}
	if err := GroupItemBatchAdd(existing.ID, []model.GroupIDAndLLMName{
		{ChannelID: 43, ModelName: "gpt-4o"},
	}, ctx); err != nil {
		t.Fatalf("GroupItemBatchAdd: %v", err)
	}

	ch := model.Channel{ID: 43, Name: "site/acct/default-oai", Model: "gpt-4o"}
	created, err := EnsureCanonicalGroupsForChannel(ch, ctx)
	if err != nil {
		t.Fatalf("EnsureCanonicalGroupsForChannel: %v", err)
	}
	if created != 0 {
		t.Fatalf("created = %d, want 0 (gpt-4o already covered)", created)
	}
	if got := countGroupItemsForChannel(t, 43); got != 1 {
		t.Fatalf("group items for channel 43 = %d, want 1 (no duplicate)", got)
	}
}

// Idempotent: a second sync of the same account must not pile up duplicate
// groups or items.
func TestEnsureCanonicalGroupsForChannel_IsIdempotent(t *testing.T) {
	ctx := setupGroupTestDB(t)
	groupMap.Clear()
	t.Cleanup(func() {
		groupCache.Clear()
		groupMap.Clear()
	})

	ch := model.Channel{ID: 44, Name: "site/acct/default-oai", Model: "gpt-4o"}
	if _, err := EnsureCanonicalGroupsForChannel(ch, ctx); err != nil {
		t.Fatalf("first EnsureCanonicalGroupsForChannel: %v", err)
	}
	firstItems := countGroupItemsForChannel(t, 44)

	created, err := EnsureCanonicalGroupsForChannel(ch, ctx)
	if err != nil {
		t.Fatalf("second EnsureCanonicalGroupsForChannel: %v", err)
	}
	if created != 0 {
		t.Fatalf("created on second run = %d, want 0", created)
	}
	if got := countGroupItemsForChannel(t, 44); got != firstItems {
		t.Fatalf("group items after second run = %d, want %d", got, firstItems)
	}

	var groupCount int64
	if err := db.GetDB().Model(&model.Group{}).Count(&groupCount).Error; err != nil {
		t.Fatalf("count groups: %v", err)
	}
	if groupCount != 1 {
		t.Fatalf("group count = %d, want 1 (no duplicate group)", groupCount)
	}
}

// The per-model covered check must run BEFORE candidates are built, not be left
// to IsCandidateCoveredByExistingGroups afterwards.
//
// "gpt-4o" and "gpt-4o-2024-05-13" share the canonical "gpt-4o". If the covered
// model is allowed into the candidate, the candidate is judged already-covered
// (a group item names "gpt-4o") and the whole thing is skipped -- taking the
// *uncovered* sibling down with it, which then has no GroupItem and cannot be
// routed. Dropping covered models first keeps the sibling's group.
func TestEnsureCanonicalGroupsForChannel_CoveredSiblingDoesNotStarveUncovered(t *testing.T) {
	ctx := setupGroupTestDB(t)
	groupMap.Clear()
	t.Cleanup(func() {
		groupCache.Clear()
		groupMap.Clear()
	})

	// Deliberately NOT named "gpt-4o": attachChannelToCoveringGroup only adopts a
	// group whose name equals the canonical, so a differently-named covering
	// group cannot rescue the sibling.
	existing := &model.Group{Name: "operators-choice", EndpointType: model.EndpointTypeChat}
	if err := GroupCreate(existing, ctx); err != nil {
		t.Fatalf("GroupCreate: %v", err)
	}
	if err := GroupItemBatchAdd(existing.ID, []model.GroupIDAndLLMName{
		{ChannelID: 47, ModelName: "gpt-4o"},
	}, ctx); err != nil {
		t.Fatalf("GroupItemBatchAdd: %v", err)
	}

	ch := model.Channel{ID: 47, Name: "site/acct/default-oai", Model: "gpt-4o,gpt-4o-2024-05-13"}
	created, err := EnsureCanonicalGroupsForChannel(ch, ctx)
	if err != nil {
		t.Fatalf("EnsureCanonicalGroupsForChannel: %v", err)
	}
	if created != 1 {
		t.Fatalf("created = %d, want 1 (a group for the uncovered sibling)", created)
	}

	// 2 items total: the pre-existing gpt-4o one, plus one for the sibling.
	if got := countGroupItemsForChannel(t, 47); got != 2 {
		t.Fatalf("group items for channel 47 = %d, want 2", got)
	}

	var siblingItems int64
	if err := db.GetDB().Model(&model.GroupItem{}).
		Where("channel_id = ? AND model_name = ?", 47, "gpt-4o-2024-05-13").
		Count(&siblingItems).Error; err != nil {
		t.Fatalf("count sibling items: %v", err)
	}
	if siblingItems != 1 {
		t.Fatalf("group items for gpt-4o-2024-05-13 = %d, want 1 (it is unroutable at 0)", siblingItems)
	}
}

// CustomModel is a second source of model names on a channel; ignoring it would
// leave those models unroutable.
func TestEnsureCanonicalGroupsForChannel_CoversCustomModel(t *testing.T) {
	ctx := setupGroupTestDB(t)
	groupMap.Clear()
	t.Cleanup(func() {
		groupCache.Clear()
		groupMap.Clear()
	})

	ch := model.Channel{ID: 45, Name: "site/acct/default-oai", CustomModel: "some-private-model"}
	created, err := EnsureCanonicalGroupsForChannel(ch, ctx)
	if err != nil {
		t.Fatalf("EnsureCanonicalGroupsForChannel: %v", err)
	}
	if created != 1 {
		t.Fatalf("created = %d, want 1", created)
	}
	if got := countGroupItemsForChannel(t, 45); got != 1 {
		t.Fatalf("group items for channel 45 = %d, want 1", got)
	}
}

// When a group already exists under the canonical name, a newly projected
// channel must be wired INTO it rather than skipped. Skipping is what makes a
// second site's identical model unroutable: the group exists, so nothing is
// created, but nothing points at the new channel either.
func TestEnsureCanonicalGroupsForChannel_AttachesToExistingCanonicalGroup(t *testing.T) {
	ctx := setupGroupTestDB(t)
	groupMap.Clear()
	t.Cleanup(func() {
		groupCache.Clear()
		groupMap.Clear()
	})

	// First site's channel creates the "gpt-4o" group.
	first := model.Channel{ID: 48, Name: "site-a/acct/default-oai", Model: "gpt-4o"}
	if _, err := EnsureCanonicalGroupsForChannel(first, ctx); err != nil {
		t.Fatalf("first EnsureCanonicalGroupsForChannel: %v", err)
	}

	// Second site offers the same model; the group already exists by name.
	second := model.Channel{ID: 49, Name: "site-b/acct/default-oai", Model: "gpt-4o"}
	created, err := EnsureCanonicalGroupsForChannel(second, ctx)
	if err != nil {
		t.Fatalf("second EnsureCanonicalGroupsForChannel: %v", err)
	}
	if created != 0 {
		t.Fatalf("created = %d, want 0 (must reuse the existing gpt-4o group)", created)
	}

	if got := countGroupItemsForChannel(t, 49); got != 1 {
		t.Fatalf("group items for channel 49 = %d, want 1 (channel is unroutable at 0)", got)
	}

	// Both channels must sit in the SAME group, giving the relay two candidates.
	var groupCount int64
	if err := db.GetDB().Model(&model.Group{}).Count(&groupCount).Error; err != nil {
		t.Fatalf("count groups: %v", err)
	}
	if groupCount != 1 {
		t.Fatalf("group count = %d, want 1", groupCount)
	}

	var itemsInGroup int64
	if err := db.GetDB().Model(&model.GroupItem{}).
		Where("model_name = ?", "gpt-4o").
		Count(&itemsInGroup).Error; err != nil {
		t.Fatalf("count gpt-4o items: %v", err)
	}
	if itemsInGroup != 2 {
		t.Fatalf("gpt-4o group items = %d, want 2 (one per channel)", itemsInGroup)
	}
}

// A channel advertising no models must be a no-op, not an error or an empty
// group.
func TestEnsureCanonicalGroupsForChannel_NoModelsIsNoop(t *testing.T) {
	ctx := setupGroupTestDB(t)
	groupMap.Clear()
	t.Cleanup(func() {
		groupCache.Clear()
		groupMap.Clear()
	})

	ch := model.Channel{ID: 46, Name: "site/acct/empty"}
	created, err := EnsureCanonicalGroupsForChannel(ch, ctx)
	if err != nil {
		t.Fatalf("EnsureCanonicalGroupsForChannel: %v", err)
	}
	if created != 0 {
		t.Fatalf("created = %d, want 0", created)
	}

	var groupCount int64
	if err := db.GetDB().Model(&model.Group{}).Count(&groupCount).Error; err != nil {
		t.Fatalf("count groups: %v", err)
	}
	if groupCount != 0 {
		t.Fatalf("group count = %d, want 0", groupCount)
	}
}
