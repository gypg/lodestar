package op

import (
	"context"
	"testing"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

func TestGroupUpdateOutboundFormatPersists(t *testing.T) {
	ctx := context.Background()
	if err := db.InitDB("sqlite", "file:group-update-outbound-format?mode=memory&cache=shared", false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		groupCache.Clear()
		groupMap.Clear()
		rebuildGroupIndexesFromCache()
	})

	groupCache.Clear()
	groupMap.Clear()
	rebuildGroupIndexesFromCache()

	// Step 1: Create a group with default (empty) outbound_format
	group := &model.Group{
		Name:         "test-outbound-format",
		EndpointType: model.EndpointTypeChat,
		Mode:         model.GroupModeRoundRobin,
		Items: []model.GroupItem{
			{ChannelID: 1, ModelName: "gpt-4o", Priority: 1, Weight: 1},
		},
	}
	if err := GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}

	// Verify initial outbound_format is empty
	if group.OutboundFormat != "" {
		t.Fatalf("expected initial outbound_format to be empty, got %q", group.OutboundFormat)
	}

	// Step 2: Update outbound_format to "responses"
	responsesFormat := "responses"
	req := &model.GroupUpdateRequest{
		ID:             group.ID,
		OutboundFormat: &responsesFormat,
	}

	updated, err := GroupUpdate(req, ctx)
	if err != nil {
		t.Fatalf("update group: %v", err)
	}

	// Step 3: Verify the returned group has the correct outbound_format
	if updated.OutboundFormat != "responses" {
		t.Errorf("expected outbound_format to be 'responses' after update, got %q", updated.OutboundFormat)
	}

	// Step 4: Re-read from cache and verify
	cached, ok := groupCache.Get(group.ID)
	if !ok {
		t.Fatalf("group not found in cache after update")
	}
	if cached.OutboundFormat != "responses" {
		t.Errorf("cache: expected outbound_format to be 'responses', got %q", cached.OutboundFormat)
	}

	// Step 5: Verify via GroupList (which applies normalizeGroup)
	groups, err := GroupList(ctx)
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	found := false
	for _, g := range groups {
		if g.ID == group.ID {
			found = true
			if g.OutboundFormat != "responses" {
				t.Errorf("list: expected outbound_format to be 'responses', got %q", g.OutboundFormat)
			}
		}
	}
	if !found {
		t.Errorf("group not found in GroupList results")
	}

	// Step 6: Direct DB read to verify persistence
	var dbGroup model.Group
	if err := db.GetDB().First(&dbGroup, group.ID).Error; err != nil {
		t.Fatalf("direct DB read failed: %v", err)
	}
	if dbGroup.OutboundFormat != "responses" {
		t.Errorf("DB: expected outbound_format to be 'responses', got %q", dbGroup.OutboundFormat)
	}

	// Step 7: Update to "chat" and verify
	chatFormat := "chat"
	req2 := &model.GroupUpdateRequest{
		ID:             group.ID,
		OutboundFormat: &chatFormat,
	}
	updated2, err := GroupUpdate(req2, ctx)
	if err != nil {
		t.Fatalf("update group to chat: %v", err)
	}
	if updated2.OutboundFormat != "chat" {
		t.Errorf("expected outbound_format to be 'chat', got %q", updated2.OutboundFormat)
	}

	// Step 8: Update back to empty (auto)
	emptyFormat := ""
	req3 := &model.GroupUpdateRequest{
		ID:             group.ID,
		OutboundFormat: &emptyFormat,
	}
	updated3, err := GroupUpdate(req3, ctx)
	if err != nil {
		t.Fatalf("update group to empty: %v", err)
	}
	if updated3.OutboundFormat != "" {
		t.Errorf("expected outbound_format to be empty, got %q", updated3.OutboundFormat)
	}

	// Step 9: Verify the empty value persisted in DB
	var dbGroup2 model.Group
	if err := db.GetDB().First(&dbGroup2, group.ID).Error; err != nil {
		t.Fatalf("direct DB read failed: %v", err)
	}
	if dbGroup2.OutboundFormat != "" {
		t.Errorf("DB: expected outbound_format to be empty, got %q", dbGroup2.OutboundFormat)
	}
}
