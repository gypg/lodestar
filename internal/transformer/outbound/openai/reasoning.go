package openai

import "strings"

// normalizeOpenAICompatReasoningEffort normalizes reasoning_effort for
// OpenAI-compatible upstreams. The currently accepted levels are
// none / minimal / low / medium / high / xhigh / max (per-model support varies).
// "none" means thinking off; every other valid level is passed through verbatim
// so that xhigh/max are not silently downgraded to high.
func normalizeOpenAICompatReasoningEffort(effort string) string {
	normalized := strings.ToLower(strings.TrimSpace(effort))

	switch normalized {
	case "", "none":
		return ""
	case "minimal", "low", "medium", "high", "xhigh", "max":
		return normalized
	default:
		// Unknown effort values are silently dropped rather than passed
		// through to providers that may reject them.
		return ""
	}
}
