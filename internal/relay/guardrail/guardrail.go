package guardrail

import (
	"encoding/json"
	"regexp"
	"strings"

	dbmodel "github.com/gypg/lodestar/internal/model"
	stg "github.com/gypg/lodestar/internal/op/setting"
)

// GuardrailConfig holds parsed guardrail rules.
type GuardrailConfig struct {
	Enabled          bool     `json:"enabled"`
	BannedWords      []string `json:"banned_words"`       // regex patterns
	MaxInputLength   int      `json:"max_input_length"`   // 0 = no limit
	MaxOutputLength  int      `json:"max_output_length"`  // 0 = no limit
	PIIDetection     bool     `json:"pii_detection"`      // detect PII in content
	BlockMessage     string   `json:"block_message"`      // message returned when blocked
}

// Violation describes a guardrail rule breach.
type Violation struct {
	Rule        string `json:"rule"`
	Message     string `json:"message"`
	MatchedText string `json:"matched_text"` // redacted copy of matched content
}

// precompiled PII patterns (simple, stateless).
var (
	piiEmail      = regexp.MustCompile(`[\w.\-]+@[\w.\-]+\.\w+`)
	piiPhone      = regexp.MustCompile(`\b1[3-9]\d{9}\b`)
	piiCreditCard = regexp.MustCompile(`\b\d{4}[\s\-]?\d{4}[\s\-]?\d{4}[\s\-]?\d{4}\b`)
	piiSSN        = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
)

// piiPatternDefs lists the PII patterns and their human-readable rule names.
var piiPatternDefs = []struct {
	name  string
	re    *regexp.Regexp
	label string
}{
	{"pii_email", piiEmail, "email address"},
	{"pii_phone", piiPhone, "phone number"},
	{"pii_credit_card", piiCreditCard, "credit card number"},
	{"pii_ssn", piiSSN, "social security number"},
}

// LoadConfig reads guardrail settings from the database cache.
// This should be called per-request (the underlying cache is in-memory).
func LoadConfig() GuardrailConfig {
	enabled, _ := stg.GetBool(dbmodel.SettingKeyGuardrailEnabled)
	raw, _ := stg.GetString(dbmodel.SettingKeyGuardrailRules)

	cfg := GuardrailConfig{
		Enabled:      enabled,
		BlockMessage: "Content blocked by guardrail policy.",
	}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	return cfg
}

// CheckInput validates input content against the guardrail configuration.
// Returns nil if the content passes all checks.
func CheckInput(content string, cfg GuardrailConfig) *Violation {
	if !cfg.Enabled {
		return nil
	}

	// Length check
	if cfg.MaxInputLength > 0 && len(content) > cfg.MaxInputLength {
		return &Violation{
			Rule:    "max_input_length",
			Message: "Input exceeds maximum allowed length.",
		}
	}

	// Banned words (regex)
	if v := checkBannedWords(content, cfg.BannedWords); v != nil {
		return v
	}

	// PII detection
	if cfg.PIIDetection {
		if v := checkPII(content); v != nil {
			return v
		}
	}

	return nil
}

// CheckOutput validates output content against the guardrail configuration.
// Returns nil if the content passes all checks.
func CheckOutput(content string, cfg GuardrailConfig) *Violation {
	if !cfg.Enabled {
		return nil
	}

	// Length check
	if cfg.MaxOutputLength > 0 && len(content) > cfg.MaxOutputLength {
		return &Violation{
			Rule:    "max_output_length",
			Message: "Output exceeds maximum allowed length.",
		}
	}

	// Banned words (regex)
	if v := checkBannedWords(content, cfg.BannedWords); v != nil {
		return v
	}

	// PII detection
	if cfg.PIIDetection {
		if v := checkPII(content); v != nil {
			return v
		}
	}

	return nil
}

// checkBannedWords runs each compiled regex against the text.
func checkBannedWords(text string, patterns []string) *Violation {
	for _, pat := range patterns {
		if pat == "" {
			continue
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			// Skip invalid regex rather than failing the whole check.
			continue
		}
		if match := re.FindString(text); match != "" {
			return &Violation{
				Rule:        "banned_word",
				Message:     "Content contains a banned pattern.",
				MatchedText: redact(match),
			}
		}
	}
	return nil
}

// checkPII scans for common PII patterns.
func checkPII(text string) *Violation {
	for _, def := range piiPatternDefs {
		if match := def.re.FindString(text); match != "" {
			return &Violation{
				Rule:        def.name,
				Message:     "Content contains " + def.label + ".",
				MatchedText: redact(match),
			}
		}
	}
	return nil
}

// redact replaces the middle portion of a match with asterisks.
func redact(s string) string {
	runes := []rune(s)
	n := len(runes)
	if n <= 4 {
		return strings.Repeat("*", n)
	}
	// Show first 2 and last 2 characters.
	return string(runes[:2]) + strings.Repeat("*", n-4) + string(runes[n-2:])
}
