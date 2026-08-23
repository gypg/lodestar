// Package secretmask holds the shared secret-masking helpers.
//
// It exists because three near-identical copies had drifted apart, and two of
// them leaked: for a secret of 8 characters or fewer they returned the trimmed
// input *verbatim*. Key length is chosen by whoever creates the channel or API
// key, so a short key was echoed in full by the channel-probe result and the
// projected-channel listing while the ordinary channel list masked it.
//
// Two output shapes are kept on purpose — they appear in different API responses
// and changing either would change what the UI renders. Keeping both here makes
// the difference deliberate and visible instead of an accident of copy-paste.
package secretmask

import "strings"

// Ellipsis renders a secret as "abcd...wxyz".
//
// Used where the response is a diagnostic (channel probe, projected channels).
func Ellipsis(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 8 {
		return stars(len(trimmed))
	}
	return trimmed[:4] + "..." + trimmed[len(trimmed)-4:]
}

// Stars renders a secret as "abcd****wxyz", the star run standing in for the
// elided middle.
//
// Used in the channel and API-key listings. Note this shape reveals the secret's
// exact length; that is pre-existing, deliberate behaviour those views rely on,
// not an oversight.
func Stars(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 8 {
		return stars(len(trimmed))
	}
	return trimmed[:4] + stars(len(trimmed)-8) + trimmed[len(trimmed)-4:]
}

func stars(n int) string {
	if n < 1 {
		return ""
	}
	return strings.Repeat("*", n)
}
