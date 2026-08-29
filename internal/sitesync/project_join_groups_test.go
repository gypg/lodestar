package sitesync

import (
	"testing"

	dbpkg "github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
)

// countGroupItemsForAccount counts GroupItem rows pointing at any channel
// projected from the account. Zero means the relay cannot reach the site's
// models at all: it resolves a request by model -> Group -> GroupItem -> Channel,
// and answers "group not found" when nothing matches.
func countGroupItemsForAccount(t *testing.T, ctx interface{ Done() <-chan struct{} }, accountID int) int {
	t.Helper()
	var bindings []model.SiteChannelBinding
	if err := dbpkg.GetDB().Where("site_account_id = ?", accountID).Find(&bindings).Error; err != nil {
		t.Fatalf("list bindings failed: %v", err)
	}
	if len(bindings) == 0 {
		return 0
	}
	channelIDs := make([]int, 0, len(bindings))
	for _, binding := range bindings {
		channelIDs = append(channelIDs, binding.ChannelID)
	}
	var count int64
	if err := dbpkg.GetDB().Model(&model.GroupItem{}).Where("channel_id IN ?", channelIDs).Count(&count).Error; err != nil {
		t.Fatalf("count group items failed: %v", err)
	}
	return int(count)
}

// With the global switch on, projection must leave the site's models routable.
//
// The original behaviour only ran ChannelAutoGroupWithMode, which *matches*
// pre-existing groups; on an install whose groups were never hand-built there
// was nothing to match, so no GroupItem was ever written and every site model
// stayed unroutable even with the switch enabled.
func TestProjectAccountJoinsGroupsWhenSwitchEnabled(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)

	// Seeds the default settings into the cache; SetString rejects unknown keys.
	if err := op.InitCache(); err != nil {
		t.Fatalf("InitCache failed: %v", err)
	}
	if err := op.SettingSetString(model.SettingKeyProjectedChannelAutoGroupEnabled, "1"); err != nil {
		t.Fatalf("enable projected auto group failed: %v", err)
	}

	if _, err := ProjectAccount(ctx, account.ID); err != nil {
		t.Fatalf("ProjectAccount failed: %v", err)
	}

	if got := countGroupItemsForAccount(t, ctx, account.ID); got == 0 {
		t.Fatal("no group items after projection: site models are unroutable")
	}

	var groupCount int64
	if err := dbpkg.GetDB().Model(&model.Group{}).Count(&groupCount).Error; err != nil {
		t.Fatalf("count groups failed: %v", err)
	}
	if groupCount == 0 {
		t.Fatal("no groups created: matching alone cannot route on a fresh install")
	}
}

// The switch must stay authoritative: with it off, projection must not invent
// groups behind the operator's back.
func TestProjectAccountDoesNotJoinGroupsWhenSwitchDisabled(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)

	if err := op.InitCache(); err != nil {
		t.Fatalf("InitCache failed: %v", err)
	}
	// "0" is also the seeded default; set it explicitly so the test states its
	// premise instead of depending on seed data.
	if err := op.SettingSetString(model.SettingKeyProjectedChannelAutoGroupEnabled, "0"); err != nil {
		t.Fatalf("disable projected auto group failed: %v", err)
	}

	if _, err := ProjectAccount(ctx, account.ID); err != nil {
		t.Fatalf("ProjectAccount failed: %v", err)
	}

	var groupCount int64
	if err := dbpkg.GetDB().Model(&model.Group{}).Count(&groupCount).Error; err != nil {
		t.Fatalf("count groups failed: %v", err)
	}
	if groupCount != 0 {
		t.Fatalf("group count = %d, want 0 while the switch is off", groupCount)
	}
	if got := countGroupItemsForAccount(t, ctx, account.ID); got != 0 {
		t.Fatalf("group items = %d, want 0 while the switch is off", got)
	}
}
