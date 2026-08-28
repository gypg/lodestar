package topup

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestGenCodeFormat(t *testing.T) {
	c := genCode()
	if !strings.HasPrefix(c, "ls-") {
		t.Errorf("genCode() = %q, want \"ls-\" prefix", c)
	}
	// "ls-" + 16 random bytes hex-encoded = 3 + 32 = 35 chars.
	if len(c) != len("ls-")+32 {
		t.Errorf("genCode() length = %d, want %d", len(c), len("ls-")+32)
	}
	hex := c[len("ls-"):]
	for _, r := range hex {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Errorf("genCode() suffix %q contains non-hex char %q", hex, r)
		}
	}
}

func TestGenCodeUniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 2000)
	for i := 0; i < 2000; i++ {
		c := genCode()
		if _, dup := seen[c]; dup {
			t.Fatalf("genCode() produced duplicate %q on iteration %d (not crypto-random?)", c, i)
		}
		seen[c] = struct{}{}
	}
}

func TestGenerateCodesValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		quota float64
	}{
		{"count=0", 0, 10},
		{"count=-1", -1, 10},
		{"count=1001", 1001, 10},
		{"quota=0", 5, 0},
		{"quota=-1", 5, -1},
	} {
		if _, err := validateGenerateCodes(tc.count, tc.quota, ""); err == nil {
			t.Errorf("validateGenerateCodes(%s): want error, got nil", tc.name)
		}
	}
}

// The note is the only record of who paid for an offline top-up, so an over-long
// one must be refused rather than truncated: a note clipped mid-sentence still
// reads as a complete audit trail.
func TestGenerateCodesRejectsOverlongNote(t *testing.T) {
	if _, err := validateGenerateCodes(1, 10, strings.Repeat("x", MaxNoteLen+1)); err == nil {
		t.Errorf("validateGenerateCodes(note=%d runes): want error, got nil", MaxNoteLen+1)
	}
	if _, err := validateGenerateCodes(1, 10, strings.Repeat("x", MaxNoteLen)); err != nil {
		t.Errorf("validateGenerateCodes(note=%d runes): want accepted, got %v", MaxNoteLen, err)
	}
}

// Counted in runes, not bytes. MaxNoteLen CJK characters are ~3x that many bytes,
// so a byte-based check would reject a note the operator was told is allowed --
// and these notes are written in Chinese: "wechat-<name>-2026-08-28-30 CNY".
func TestGenerateCodesNoteLimitCountsRunesNotBytes(t *testing.T) {
	note := strings.Repeat("码", MaxNoteLen) // MaxNoteLen runes, 3x that in bytes
	if len(note) <= MaxNoteLen {
		t.Fatalf("test premise broken: %d CJK runes should exceed %d bytes, got %d",
			MaxNoteLen, MaxNoteLen, len(note))
	}
	got, err := validateGenerateCodes(1, 10, note)
	if err != nil {
		t.Errorf("validateGenerateCodes(%d CJK runes, %d bytes): rejected, so the limit "+
			"is counting bytes: %v", MaxNoteLen, len(note), err)
	}
	if got != note {
		t.Errorf("validateGenerateCodes returned a note of %d runes, want %d unchanged",
			utf8.RuneCountInString(got), MaxNoteLen)
	}
}

// Whitespace-only notes must not survive as a phantom audit trail: a code showing
// a blank note is honest about having none, one showing "   " looks filled in.
func TestGenerateCodesTrimsNote(t *testing.T) {
	got, err := validateGenerateCodes(1, 10, "  wechat-2026-08-28  ")
	if err != nil {
		t.Fatalf("validateGenerateCodes: unexpected error %v", err)
	}
	if got != "wechat-2026-08-28" {
		t.Errorf("note = %q, want it trimmed to %q", got, "wechat-2026-08-28")
	}
	if blank, err := validateGenerateCodes(1, 10, "   "); err != nil || blank != "" {
		t.Errorf("validateGenerateCodes(whitespace-only) = %q, %v; want \"\", nil", blank, err)
	}
}
