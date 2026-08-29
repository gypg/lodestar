package sitesync

import (
	"testing"

	dbpkg "github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
)

// Projected channels must land in a folder named after the site. They used to
// inherit Default (op/channel.Create falls back to GroupDefaultID when GroupID is
// 0) and the channel page filters strictly by folder, so a site's channels were
// invisible unless Default was the selected tab.
//
// Asserted through ProjectAccount rather than against ChannelGroupEnsureByName
// directly: the folder helper being correct means nothing if projection never
// passes GroupID to the channel it builds.
func TestProjectAccountPlacesChannelsInSiteFolder(t *testing.T) {
	ctx := setupProjectTestDB(t)
	site, account := createProjectionFixture(t, ctx)

	if _, err := ProjectAccount(ctx, account.ID); err != nil {
		t.Fatalf("ProjectAccount failed: %v", err)
	}

	folderID, err := op.ChannelGroupEnsureByName(site.Name, ctx)
	if err != nil {
		t.Fatalf("ChannelGroupEnsureByName failed: %v", err)
	}

	defaultFolderID, err := op.ChannelGroupDefaultID(ctx)
	if err != nil {
		t.Fatalf("ChannelGroupDefaultID failed: %v", err)
	}
	if folderID == defaultFolderID {
		t.Fatal("site folder id equals Default folder id; the test cannot tell them apart")
	}

	var bindings []model.SiteChannelBinding
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ?", account.ID).Find(&bindings).Error; err != nil {
		t.Fatalf("list bindings failed: %v", err)
	}
	if len(bindings) == 0 {
		t.Fatal("no projected channels; fixture produced nothing to assert on")
	}

	for _, binding := range bindings {
		channel, err := op.ChannelGet(binding.ChannelID, ctx)
		if err != nil {
			t.Fatalf("ChannelGet(%d) failed: %v", binding.ChannelID, err)
		}
		if channel.GroupID != folderID {
			t.Fatalf("channel %d GroupID = %d, want site folder %d (Default is %d)",
				channel.ID, channel.GroupID, folderID, defaultFolderID)
		}
	}
}

// A site whose tokens are all unusable projects no channels, so it must not leave
// an empty folder behind cluttering the channel page's tab strip.
func TestProjectAccountSkipsFolderWhenNothingProjected(t *testing.T) {
	ctx := setupProjectTestDB(t)
	site, account := createProjectionFixture(t, ctx)

	// Disable every token: buildChannelKeys yields nothing, so desiredKeys is
	// empty and no channel is projected.
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteToken{}).
		Where("site_account_id = ?", account.ID).
		Update("enabled", false).Error; err != nil {
		t.Fatalf("disable tokens failed: %v", err)
	}

	if _, err := ProjectAccount(ctx, account.ID); err != nil {
		t.Fatalf("ProjectAccount failed: %v", err)
	}

	var bindings []model.SiteChannelBinding
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ?", account.ID).Find(&bindings).Error; err != nil {
		t.Fatalf("list bindings failed: %v", err)
	}
	if len(bindings) != 0 {
		t.Fatalf("bindings = %d, want 0; the premise (nothing projected) does not hold", len(bindings))
	}

	// Queried against the DB, not op.ChannelGroupList: the channel-group cache is
	// package-level in op and is not reset between tests, so a folder created by
	// an earlier test in this package would leak into the assertion.
	var folderCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.ChannelGroup{}).
		Where("name = ?", site.Name).
		Count(&folderCount).Error; err != nil {
		t.Fatalf("count folders failed: %v", err)
	}
	if folderCount != 0 {
		t.Fatalf("folder %q was created for a site that projected no channels", site.Name)
	}
}

// Re-projection must migrate channels created before the per-site folder existed,
// otherwise every install that already synced stays stuck in Default until each
// channel is edited by hand.
func TestProjectAccountMigratesExistingChannelOutOfDefaultFolder(t *testing.T) {
	ctx := setupProjectTestDB(t)
	site, account := createProjectionFixture(t, ctx)

	if _, err := ProjectAccount(ctx, account.ID); err != nil {
		t.Fatalf("first ProjectAccount failed: %v", err)
	}

	defaultFolderID, err := op.ChannelGroupDefaultID(ctx)
	if err != nil {
		t.Fatalf("ChannelGroupDefaultID failed: %v", err)
	}

	var bindings []model.SiteChannelBinding
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ?", account.ID).Find(&bindings).Error; err != nil {
		t.Fatalf("list bindings failed: %v", err)
	}
	if len(bindings) == 0 {
		t.Fatal("no projected channels to migrate")
	}

	// Simulate the pre-change state: shove every projected channel into Default.
	for _, binding := range bindings {
		if err := dbpkg.GetDB().WithContext(ctx).Model(&model.Channel{}).
			Where("id = ?", binding.ChannelID).
			Update("group_id", defaultFolderID).Error; err != nil {
			t.Fatalf("force channel %d into Default failed: %v", binding.ChannelID, err)
		}
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("InitCache failed: %v", err)
	}

	if _, err := ProjectAccount(ctx, account.ID); err != nil {
		t.Fatalf("second ProjectAccount failed: %v", err)
	}

	folderID, err := op.ChannelGroupEnsureByName(site.Name, ctx)
	if err != nil {
		t.Fatalf("ChannelGroupEnsureByName failed: %v", err)
	}
	for _, binding := range bindings {
		channel, err := op.ChannelGet(binding.ChannelID, ctx)
		if err != nil {
			t.Fatalf("ChannelGet(%d) failed: %v", binding.ChannelID, err)
		}
		if channel.GroupID != folderID {
			t.Fatalf("channel %d GroupID = %d after re-projection, want site folder %d",
				channel.ID, channel.GroupID, folderID)
		}
	}
}
