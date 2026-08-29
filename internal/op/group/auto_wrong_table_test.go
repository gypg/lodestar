package group

import (
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

// AutoGroupModels used to call a helper that queried model.ChannelGroup -- the
// channel *folder* table -- while its purpose is routing groups (model.Group).
// model.Group has no IsDefault column at all, so `is_default = false` could only
// ever have addressed folders; it was a wrong-table bug, not a design choice.
//
// Consequence: pressing 自动分组 deleted every operator-created channel folder,
// bypassing ChannelGroupDelete's non-empty guard, and left the channels holding
// dangling group_ids. Since the 渠道 page filters strictly by folder, those
// channels then disappeared from the UI entirely.
//
// This also regressed the per-site folders that site projection now creates:
// they are non-default folders, so one press of 自动分组 undid them.
func TestAutoGroupModelsDoesNotDeleteChannelFolders(t *testing.T) {
	ctx := setupGroupTestDB(t)
	groupMap.Clear()
	t.Cleanup(func() {
		groupCache.Clear()
		groupMap.Clear()
	})

	// Default is auto-ensured elsewhere; seeding it here would collide on the
	// unique name index.
	folders := []model.ChannelGroup{
		{Name: "My Site", IsDefault: false},
		{Name: "Hand Made Folder", IsDefault: false},
	}
	if err := db.GetDB().WithContext(ctx).Create(&folders).Error; err != nil {
		t.Fatalf("seed channel folders: %v", err)
	}

	if _, err := AutoGroupModels(ctx, false); err != nil {
		t.Fatalf("AutoGroupModels: %v", err)
	}

	var survivors int64
	if err := db.GetDB().WithContext(ctx).Model(&model.ChannelGroup{}).
		Where("is_default = ?", false).
		Count(&survivors).Error; err != nil {
		t.Fatalf("count folders: %v", err)
	}
	if survivors != 2 {
		t.Fatalf("non-default channel folders = %d, want 2; auto-group destroyed operator folders", survivors)
	}
}

// force=true is what "rebuild from scratch" now means: drop routing groups whose
// name is a known auto-group canonical, leave everything the operator named
// themselves. Previously force was dead -- read only by a log line -- while the
// wipe ran on every call and hit the wrong table.
func TestAutoGroupModelsForceDeletesOnlyAutoCreatedRoutingGroups(t *testing.T) {
	ctx := setupGroupTestDB(t)
	groupMap.Clear()
	t.Cleanup(func() {
		groupCache.Clear()
		groupMap.Clear()
	})

	// "gpt-4o" is a canonical auto-group name; "my-handmade-group" is not.
	autoCreated := &model.Group{Name: "gpt-4o", EndpointType: model.EndpointTypeChat}
	if err := GroupCreate(autoCreated, ctx); err != nil {
		t.Fatalf("GroupCreate auto: %v", err)
	}
	// Also needs a channel source. Without one deleteStaleGroups collects it and
	// the assertion below would pass even if the force path did nothing.
	if err := GroupItemBatchAdd(autoCreated.ID, []model.GroupIDAndLLMName{
		{ChannelID: 79, ModelName: "gpt-4o"},
	}, ctx); err != nil {
		t.Fatalf("GroupItemBatchAdd auto: %v", err)
	}
	handMade := &model.Group{Name: "my-handmade-group", EndpointType: model.EndpointTypeChat}
	if err := GroupCreate(handMade, ctx); err != nil {
		t.Fatalf("GroupCreate hand-made: %v", err)
	}
	// Needs a channel source, otherwise the pre-existing deleteStaleGroups pass
	// collects it and the test would credit that to the force path.
	if err := GroupItemBatchAdd(handMade.ID, []model.GroupIDAndLLMName{
		{ChannelID: 78, ModelName: "some-model"},
	}, ctx); err != nil {
		t.Fatalf("GroupItemBatchAdd hand-made: %v", err)
	}

	if _, err := AutoGroupModels(ctx, true); err != nil {
		t.Fatalf("AutoGroupModels(force): %v", err)
	}

	var handMadeSurvives int64
	if err := db.GetDB().WithContext(ctx).Model(&model.Group{}).
		Where("name = ?", "my-handmade-group").Count(&handMadeSurvives).Error; err != nil {
		t.Fatalf("count hand-made: %v", err)
	}
	if handMadeSurvives != 1 {
		t.Fatal("force deleted an operator-named routing group")
	}

	// No channel declares gpt-4o in this fixture, so nothing recreates it.
	var autoSurvives int64
	if err := db.GetDB().WithContext(ctx).Model(&model.Group{}).
		Where("name = ?", "gpt-4o").Count(&autoSurvives).Error; err != nil {
		t.Fatalf("count auto: %v", err)
	}
	if autoSurvives != 0 {
		t.Fatal("force did not clear the auto-created routing group")
	}
}

// Without force, an auto-created group must survive: the non-force path relies on
// IsCandidateCoveredByExistingGroups to skip what exists rather than rebuilding.
func TestAutoGroupModelsWithoutForceKeepsAutoCreatedGroups(t *testing.T) {
	ctx := setupGroupTestDB(t)
	groupMap.Clear()
	t.Cleanup(func() {
		groupCache.Clear()
		groupMap.Clear()
	})

	autoCreated := &model.Group{Name: "gpt-4o", EndpointType: model.EndpointTypeChat}
	if err := GroupCreate(autoCreated, ctx); err != nil {
		t.Fatalf("GroupCreate: %v", err)
	}
	if err := GroupItemBatchAdd(autoCreated.ID, []model.GroupIDAndLLMName{
		{ChannelID: 77, ModelName: "gpt-4o"},
	}, ctx); err != nil {
		t.Fatalf("GroupItemBatchAdd: %v", err)
	}

	if _, err := AutoGroupModels(ctx, false); err != nil {
		t.Fatalf("AutoGroupModels: %v", err)
	}

	var survives int64
	if err := db.GetDB().WithContext(ctx).Model(&model.Group{}).
		Where("name = ?", "gpt-4o").Count(&survives).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if survives != 1 {
		t.Fatal("non-force run deleted an existing auto-created group")
	}
}

// The same helper deleted GroupItem rows keyed by *folder* ids. GroupItem.GroupID
// references model.Group, a different id space, so unrelated routing groups were
// silently emptied wherever the integers happened to collide -- taking their
// channels out of routing with them.
func TestAutoGroupModelsDoesNotEmptyUnrelatedRoutingGroups(t *testing.T) {
	ctx := setupGroupTestDB(t)
	groupMap.Clear()
	t.Cleanup(func() {
		groupCache.Clear()
		groupMap.Clear()
	})

	routing := &model.Group{Name: "hand-made-routing", EndpointType: model.EndpointTypeChat}
	if err := GroupCreate(routing, ctx); err != nil {
		t.Fatalf("GroupCreate: %v", err)
	}

	// Force the collision the bug depends on: a non-default FOLDER carrying the
	// same integer id as the routing group above. Without pinning the id the two
	// sequences may never overlap and the test would silently prove nothing.
	collidingFolder := model.ChannelGroup{ID: routing.ID, Name: "Colliding Folder", IsDefault: false}
	if err := db.GetDB().WithContext(ctx).Create(&collidingFolder).Error; err != nil {
		t.Fatalf("seed colliding folder: %v", err)
	}
	if collidingFolder.ID != routing.ID {
		t.Fatalf("folder id = %d, want %d; the collision premise does not hold", collidingFolder.ID, routing.ID)
	}
	if err := GroupItemBatchAdd(routing.ID, []model.GroupIDAndLLMName{
		{ChannelID: 91, ModelName: "gpt-4o"},
		{ChannelID: 92, ModelName: "gpt-4o"},
	}, ctx); err != nil {
		t.Fatalf("GroupItemBatchAdd: %v", err)
	}

	var before int64
	if err := db.GetDB().WithContext(ctx).Model(&model.GroupItem{}).
		Where("group_id = ?", routing.ID).Count(&before).Error; err != nil {
		t.Fatalf("count items before: %v", err)
	}
	if before != 2 {
		t.Fatalf("precondition: items = %d, want 2", before)
	}

	if _, err := AutoGroupModels(ctx, false); err != nil {
		t.Fatalf("AutoGroupModels: %v", err)
	}

	var after int64
	if err := db.GetDB().WithContext(ctx).Model(&model.GroupItem{}).
		Where("group_id = ?", routing.ID).Count(&after).Error; err != nil {
		t.Fatalf("count items after: %v", err)
	}
	if after != before {
		t.Fatalf("routing group %d items = %d after auto-group, want %d; folder ids leaked into the GroupItem id space",
			routing.ID, after, before)
	}
}
