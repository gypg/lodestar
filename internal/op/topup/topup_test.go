package topup

import (
	"context"
	"strings"
	"testing"
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
	ctx := context.Background()
	if _, err := GenerateCodes(0, 10, ctx); err == nil {
		t.Errorf("GenerateCodes(count=0): want error, got nil")
	}
	if _, err := GenerateCodes(-1, 10, ctx); err == nil {
		t.Errorf("GenerateCodes(count=-1): want error, got nil")
	}
	if _, err := GenerateCodes(1001, 10, ctx); err == nil {
		t.Errorf("GenerateCodes(count=1001): want error, got nil")
	}
	if _, err := GenerateCodes(5, 0, ctx); err == nil {
		t.Errorf("GenerateCodes(quota=0): want error, got nil")
	}
	if _, err := GenerateCodes(5, -1, ctx); err == nil {
		t.Errorf("GenerateCodes(quota=-1): want error, got nil")
	}
}
