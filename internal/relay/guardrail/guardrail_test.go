package guardrail

import "testing"

func TestCheckInput_DisabledNeverChecks(t *testing.T) {
	cfg := GuardrailConfig{Enabled: false, BannedWords: []string{`bad`}, PIIDetection: true}
	if v := CheckInput("bad a@b.com", cfg); v != nil {
		t.Errorf("disabled guardrail must not check, got violation %v", v)
	}
}

func TestCheckInput_BannedWord(t *testing.T) {
	cfg := GuardrailConfig{Enabled: true, BannedWords: []string{`(?i)forbidden`}}
	v := CheckInput("this is FORBIDDEN content", cfg)
	if v == nil || v.Rule != "banned_word" {
		t.Errorf("banned word: got %v, want rule=banned_word", v)
	}
}

func TestCheckInput_LengthLimit(t *testing.T) {
	cfg := GuardrailConfig{Enabled: true, MaxInputLength: 5}
	if v := CheckInput("123456", cfg); v == nil || v.Rule != "max_input_length" {
		t.Errorf("over length: got %v, want max_input_length", v)
	}
	if v := CheckInput("12345", cfg); v != nil {
		t.Errorf("within limit should pass, got %v", v)
	}
}

func TestCheckInput_PII(t *testing.T) {
	cfg := GuardrailConfig{Enabled: true, PIIDetection: true}
	cases := []struct {
		text, wantRule string
	}{
		{"contact me at a@b.com", "pii_email"},
		{"call 13800138000", "pii_phone"},
		{"card 4111111111111111", "pii_credit_card"},
		{"ssn 123-45-6789", "pii_ssn"},
	}
	for _, c := range cases {
		v := CheckInput(c.text, cfg)
		if v == nil || v.Rule != c.wantRule {
			t.Errorf("CheckInput(%q): got %v, want rule=%s", c.text, v, c.wantRule)
		}
	}
}

func TestCheckInput_CleanPasses(t *testing.T) {
	cfg := GuardrailConfig{Enabled: true, PIIDetection: true, MaxInputLength: 1000}
	if v := CheckInput("hello world, just a normal question", cfg); v != nil {
		t.Errorf("clean input should pass, got %v", v)
	}
}

func TestCheckInput_InvalidRegexSkipped(t *testing.T) {
	// An invalid regex must be skipped, not fail the whole check.
	cfg := GuardrailConfig{Enabled: true, BannedWords: []string{`(unclosed`}}
	if v := CheckInput("clean text", cfg); v != nil {
		t.Errorf("invalid regex should be skipped, got %v", v)
	}
}

func TestCheckOutput_BannedWordAndLength(t *testing.T) {
	// banned word without a length limit
	cfgBan := GuardrailConfig{Enabled: true, BannedWords: []string{`secret`}}
	if v := CheckOutput("the secret value", cfgBan); v == nil || v.Rule != "banned_word" {
		t.Errorf("output banned word: got %v, want banned_word", v)
	}
	// over length, no banned word
	cfgLen := GuardrailConfig{Enabled: true, MaxOutputLength: 3}
	if v := CheckOutput("toolong", cfgLen); v == nil || v.Rule != "max_output_length" {
		t.Errorf("output over length: got %v, want max_output_length", v)
	}
}

func TestRedact_HidesMiddle(t *testing.T) {
	// short strings fully redacted; longer ones show first/last 2 chars
	if got := redact("ab"); got != "**" {
		t.Errorf("redact(ab) = %q, want **", got)
	}
	if got := redact("abcdef"); got != "ab**ef" {
		t.Errorf("redact(abcdef) = %q, want ab**ef", got)
	}
}
