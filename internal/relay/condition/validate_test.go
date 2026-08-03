package condition

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateConditionAcceptsValidInput(t *testing.T) {
	valid := []string{
		"",    // empty
		"   ", // whitespace
		"[]",  // empty array
		`[{"key":"model","op":"equals","value":"gpt-4"}]`,
		`[{"key":"model","op":"contains","value":"gpt-4"},{"key":"hour","op":"gt","value":"5"}]`,
		// Every legal key, one rule each (header_ prefix form).
		`[{"key":"model","op":"equals","value":"x"}]`,
		`[{"key":"api_key_id","op":"equals","value":"1"}]`,
		`[{"key":"api_key_name","op":"equals","value":"a"}]`,
		`[{"key":"content_type","op":"equals","value":"a"}]`,
		`[{"key":"hour","op":"equals","value":"1"}]`,
		`[{"key":"header_x-tenant-id","op":"equals","value":"a"}]`,
		// Every legal op, one rule each.
		`[{"key":"model","op":"equals","value":"x"}]`,
		`[{"key":"model","op":"not_equals","value":"x"}]`,
		`[{"key":"model","op":"contains","value":"x"}]`,
		`[{"key":"model","op":"not_contains","value":"x"}]`,
		`[{"key":"model","op":"starts_with","value":"x"}]`,
		`[{"key":"model","op":"ends_with","value":"x"}]`,
		`[{"key":"model","op":"regex","value":"x"}]`,
		`[{"key":"model","op":"in_list","value":"x,y"}]`,
		`[{"key":"model","op":"gt","value":"1"}]`,
		`[{"key":"model","op":"lt","value":"1"}]`,
	}
	for _, c := range valid {
		if err := ValidateCondition(c); err != nil {
			t.Errorf("ValidateCondition(%q) unexpected error: %v", c, err)
		}
	}
}

func TestValidateConditionRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"unknown key", `[{"key":"api_key_nmae","op":"equals","value":"x"}]`},
		{"unknown op", `[{"key":"model","op":"contais","value":"x"}]`},
		{"empty key", `[{"key":"","op":"equals","value":"x"}]`},
		{"empty op", `[{"key":"model","op":"","value":"x"}]`},
		{"json syntax error", `[{"key":"model"`},
		{"object instead of array", `{"key":"model","op":"equals","value":"x"}`},
		{"uppercase header prefix", `[{"key":"HEADER_x-tenant-id","op":"equals","value":"x"}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateCondition(tc.in); err == nil {
				t.Fatalf("ValidateCondition(%q) expected error, got nil", tc.in)
			}
		})
	}
}

func TestValidateConditionErrorNamesOffendingField(t *testing.T) {
	// The error must identify the offending value so the user can fix it.
	if err := ValidateCondition(`[{"key":"api_key_nmae","op":"equals","value":"x"}]`); err == nil {
		t.Fatal("expected error for unknown key")
	} else if !strings.Contains(err.Error(), "api_key_nmae") {
		t.Errorf("error %q should contain the offending key", err)
	} else if !strings.Contains(err.Error(), "rule 1") {
		t.Errorf("error %q should mention the rule number", err)
	}

	if err := ValidateCondition(`[{"key":"model","op":"contais","value":"x"}]`); err == nil {
		t.Fatal("expected error for unknown op")
	} else if !strings.Contains(err.Error(), "contais") {
		t.Errorf("error %q should contain the offending op", err)
	}
}

func TestValidateConditionErrorIsSentinel(t *testing.T) {
	err := ValidateCondition(`[{"key":"nope","op":"equals","value":"x"}]`)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidCondition) {
		t.Errorf("error should wrap ErrInvalidCondition, got %v", err)
	}
}
