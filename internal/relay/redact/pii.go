package redact

import "regexp"

// Pre-compiled regexps for PII patterns. Compiled once at init for fast hot-path use.
var (
	// email: standard email pattern
	emailRe = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	// phone: international or domestic formats (7-15 digits, optional separators)
	phoneRe = regexp.MustCompile(`(?:\+?\d{1,3}[\s\-]?)?\(?\d{2,4}\)?[\s\-]?\d{3,4}[\s\-]?\d{3,4}(?:[\s\-]?\d{1,4})?`)
	// credit card: 13-19 digits, optional spaces/dashes between groups
	creditCardRe = regexp.MustCompile(`\b(?:\d{4}[\s\-]?){3}\d{1,7}\b`)
	// SSN: US Social Security Number (3-2-4 digits)
	ssnRe = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
)

// RedactPII replaces personally identifiable information in the given content
// with redaction placeholders: emails, phone numbers, credit cards, and SSNs.
func RedactPII(content string) string {
	content = emailRe.ReplaceAllString(content, "[EMAIL_REDACTED]")
	content = ssnRe.ReplaceAllString(content, "[SSN_REDACTED]")
	content = creditCardRe.ReplaceAllString(content, "[CARD_REDACTED]")
	content = phoneRe.ReplaceAllString(content, "[PHONE_REDACTED]")
	return content
}
