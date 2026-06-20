package relay

import (
	"strings"

	"github.com/gypg/lodestar/internal/transformer/model"
)

// extractRequestText concatenates the text portions of every message in the
// request. Used as input for guardrail.CheckInput so banned-word / PII / length
// rules can scan what the user actually sent. Non-text parts (images, audio,
// files) are skipped.
func extractRequestText(req *model.InternalLLMRequest) string {
	if req == nil {
		return ""
	}
	var sb strings.Builder
	for _, msg := range req.Messages {
		if msg.Content.Content != nil {
			sb.WriteString(*msg.Content.Content)
			sb.WriteByte('\n')
		}
		for _, part := range msg.Content.MultipleContent {
			if part.Type == "text" && part.Text != nil {
				sb.WriteString(*part.Text)
				sb.WriteByte('\n')
			}
		}
	}
	return sb.String()
}
