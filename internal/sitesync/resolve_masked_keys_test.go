package sitesync

import (
	"testing"

	"github.com/gypg/lodestar/internal/model"
)

// Management platforms (new-api and forks) mask keys in every listing endpoint --
// list, get-by-id and update all return `key[:4] + "**********" + key[-4:]` -- so
// a synced token was stored as that mask and projection refused to build a
// channel from it. The plaintext is reachable through a dedicated per-id
// endpoint the sync never called, leaving the operator to retype keys by hand.
func TestApplyResolvedTokenKeysReplacesMaskedValue(t *testing.T) {
	tokens := []model.SiteToken{
		{UpstreamID: 7, Name: "SUB", Token: "sk-a**********wxyz", GroupKey: "default"},
	}

	resolved := applyResolvedTokenKeys(tokens, map[int]string{7: "sk-real-plaintext-value"})

	if resolved[0].Token != "sk-real-plaintext-value" {
		t.Fatalf("Token = %q, want the plaintext", resolved[0].Token)
	}
	if model.IsMaskedSiteTokenValue(resolved[0].Token) {
		t.Fatal("resolved token still reads as masked, so projection would still refuse it")
	}
}

// An upstream that echoes the mask back must not be accepted: the record would
// flip to "ready" while remaining unusable, which is worse than staying pending
// because the operator loses the signal that it needs completing.
func TestApplyResolvedTokenKeysRejectsEchoedMask(t *testing.T) {
	tokens := []model.SiteToken{
		{UpstreamID: 7, Token: "sk-a**********wxyz", GroupKey: "default"},
	}

	// The echoed mask must DIFFER from the stored one. Echoing the identical
	// string makes "substituted" and "left alone" indistinguishable, so the
	// assertion below would hold even without the guard.
	resolved := applyResolvedTokenKeys(tokens, map[int]string{7: "sk-z**********diff"})

	if resolved[0].Token != "sk-a**********wxyz" {
		t.Fatalf("Token = %q, want the original kept; an echoed mask was accepted", resolved[0].Token)
	}
}

// A token whose value is already plaintext must be left alone, even when the map
// happens to carry an entry for its id.
func TestApplyResolvedTokenKeysLeavesReadyTokenAlone(t *testing.T) {
	tokens := []model.SiteToken{
		{UpstreamID: 7, Token: "sk-already-real", GroupKey: "default"},
	}

	resolved := applyResolvedTokenKeys(tokens, map[int]string{7: "sk-something-else"})

	if resolved[0].Token != "sk-already-real" {
		t.Fatalf("Token = %q, want the original plaintext untouched", resolved[0].Token)
	}
}

// Only the matching id may be substituted; a token with no entry keeps its mask
// so it still surfaces as pending for manual completion.
func TestApplyResolvedTokenKeysOnlySubstitutesMatchingID(t *testing.T) {
	tokens := []model.SiteToken{
		{UpstreamID: 7, Token: "sk-a**********wxyz", GroupKey: "default"},
		{UpstreamID: 9, Token: "sk-b**********wxyz", GroupKey: "vip"},
	}

	resolved := applyResolvedTokenKeys(tokens, map[int]string{7: "sk-real-seven"})

	if resolved[0].Token != "sk-real-seven" {
		t.Fatalf("id 7 Token = %q, want substitution", resolved[0].Token)
	}
	if !model.IsMaskedSiteTokenValue(resolved[1].Token) {
		t.Fatalf("id 9 Token = %q, want the mask kept so it stays pending", resolved[1].Token)
	}
}

// A token the site listed without an id cannot be looked up, so it must be left
// pending rather than picking up an unrelated key.
func TestApplyResolvedTokenKeysSkipsTokenWithoutUpstreamID(t *testing.T) {
	tokens := []model.SiteToken{
		{UpstreamID: 0, Token: "sk-a**********wxyz", GroupKey: "default"},
	}

	resolved := applyResolvedTokenKeys(tokens, map[int]string{0: "sk-should-not-apply"})

	if !model.IsMaskedSiteTokenValue(resolved[0].Token) {
		t.Fatalf("Token = %q, want the mask kept when no upstream id is known", resolved[0].Token)
	}
}

// An empty result set (upstream refused, rate-limited, or lacks the endpoint)
// must degrade to the previous behaviour rather than blanking values.
func TestApplyResolvedTokenKeysNoResultsKeepsTokensIntact(t *testing.T) {
	tokens := []model.SiteToken{
		{UpstreamID: 7, Token: "sk-a**********wxyz", GroupKey: "default"},
	}

	resolved := applyResolvedTokenKeys(tokens, map[int]string{})

	if len(resolved) != 1 || resolved[0].Token != "sk-a**********wxyz" {
		t.Fatalf("tokens were altered despite an empty result: %+v", resolved)
	}
}
