package redact

import "testing"

// TestRedactPII locks in the current regex-based redaction behavior for the
// curated corpus of inputs. Applies email, SSN, credit-card, then phone
// redaction in that order, each replacing matched substrings with a placeholder.
//
// Note: pure order number "12345678" is over-redacted as a phone number
// (known over-redaction). Lock the current behavior so tightening the phone
// regex later will surface as a test change rather than silently regressing.
func TestRedactPII(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"contact me at bob@example.com", "contact me at [EMAIL_REDACTED]"},
		{"SSN 123-45-6789 here", "SSN [SSN_REDACTED] here"},
		{"card 4111 1111 1111 1111 done", "card [CARD_REDACTED] done"},
		{"call 555-123-4567", "call [PHONE_REDACTED]"},
		{"order 12345678 shipped", "order [PHONE_REDACTED] shipped"},
		{"version 1.2.3 released", "version 1.2.3 released"},
		{"no pii at all", "no pii at all"},
		{"", ""},
		{"a@b.co and 987-65-4321", "[EMAIL_REDACTED] and [SSN_REDACTED]"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := RedactPII(tt.in)
			if got != tt.want {
				t.Errorf("RedactPII(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
