package openai

import "strings"

func normalizeOpenAICompatReasoningEffort(effort string) string {
	normalized := strings.ToLower(strings.TrimSpace(effort))

	switch normalized {
	case "", "none":
		return ""
	case "low", "medium", "high":
		return normalized
	case "xhigh", "max":
		// Anthropic/DeepSeek extended reasoning levels —
		// map to the highest standard OpenAI level.
		return "high"
	default:
		// Unknown effort values are silently dropped rather than passed
		// through to providers that may reject them.
		return ""
	}
}
