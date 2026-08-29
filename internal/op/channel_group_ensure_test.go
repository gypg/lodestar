package op

import (
	"testing"

	"github.com/gypg/lodestar/internal/model"
)

// Projected site channels used to inherit the Default folder, because
// op/channel.Create falls back to GroupDefaultID when GroupID is 0. The channel
// page filters strictly by folder, so a site's channels were invisible unless
// Default happened to be the selected tab. Each site now gets its own folder.
func TestChannelGroupEnsureByName_CreatesFolder(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	id, err := ChannelGroupEnsureByName("my-site", ctx)
	if err != nil {
		t.Fatalf("ChannelGroupEnsureByName: %v", err)
	}
	if id == 0 {
		t.Fatal("id = 0, want a real folder id")
	}

	group, err := ChannelGroupGet(id, ctx)
	if err != nil {
		t.Fatalf("ChannelGroupGet: %v", err)
	}
	if group.Name != "my-site" {
		t.Fatalf("Name = %q, want %q", group.Name, "my-site")
	}
}

// Re-syncing a site must reuse its folder, not fail on the unique name index or
// spawn a second one.
func TestChannelGroupEnsureByName_ReusesExistingFolder(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	first, err := ChannelGroupEnsureByName("my-site", ctx)
	if err != nil {
		t.Fatalf("first ChannelGroupEnsureByName: %v", err)
	}
	second, err := ChannelGroupEnsureByName("my-site", ctx)
	if err != nil {
		t.Fatalf("second ChannelGroupEnsureByName: %v", err)
	}
	if first != second {
		t.Fatalf("second id = %d, want %d (must reuse)", second, first)
	}

	groups, err := ChannelGroupList(ctx)
	if err != nil {
		t.Fatalf("ChannelGroupList: %v", err)
	}
	matches := 0
	for _, group := range groups {
		if group.Name == "my-site" {
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("folders named my-site = %d, want 1", matches)
	}
}

// A site literally named "Default" must adopt the existing default folder rather
// than fail the insert against the unique index.
func TestChannelGroupEnsureByName_SiteNamedDefaultReusesDefaultFolder(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	defaultID, err := ChannelGroupDefaultID(ctx)
	if err != nil {
		t.Fatalf("ChannelGroupDefaultID: %v", err)
	}

	id, err := ChannelGroupEnsureByName(model.DefaultChannelGroupName, ctx)
	if err != nil {
		t.Fatalf("ChannelGroupEnsureByName: %v", err)
	}
	if id != defaultID {
		t.Fatalf("id = %d, want default folder id %d", id, defaultID)
	}
}

// Case and surrounding whitespace must not create a near-duplicate folder, since
// the unique index would reject it on some collations and the UI would show two
// folders that look identical.
func TestChannelGroupEnsureByName_MatchesCaseInsensitivelyAndTrimmed(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	first, err := ChannelGroupEnsureByName("My-Site", ctx)
	if err != nil {
		t.Fatalf("first ChannelGroupEnsureByName: %v", err)
	}
	second, err := ChannelGroupEnsureByName("  my-site  ", ctx)
	if err != nil {
		t.Fatalf("second ChannelGroupEnsureByName: %v", err)
	}
	if first != second {
		t.Fatalf("second id = %d, want %d (case/space must not fork a folder)", second, first)
	}
}

// An empty site name has no folder to make; fall back to Default rather than
// creating an unnamed folder or erroring the whole projection.
func TestChannelGroupEnsureByName_EmptyNameFallsBackToDefault(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	defaultID, err := ChannelGroupDefaultID(ctx)
	if err != nil {
		t.Fatalf("ChannelGroupDefaultID: %v", err)
	}

	id, err := ChannelGroupEnsureByName("   ", ctx)
	if err != nil {
		t.Fatalf("ChannelGroupEnsureByName: %v", err)
	}
	if id != defaultID {
		t.Fatalf("id = %d, want default folder id %d", id, defaultID)
	}
}
