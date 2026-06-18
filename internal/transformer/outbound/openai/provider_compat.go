package openai

import (
	"strings"

	"github.com/gypg/lodestar/internal/transformer/model"
)

// Provider-specific request sanitization for OpenAI-compatible upstreams that
// are *not* reasoning-compat (deepseek/mimo are handled in deepseek.go).
//
// These providers speak the OpenAI Chat protocol but reject or mis-handle a
// few standard fields. We detect them by base URL / model name — the same
// identification strategy as isDeepSeekCompatRequest — and rewrite the request
// just before it is forwarded, matching how new-api's per-provider adaptors
// (relay/channel/moonshot, relay/channel/zhipu_4v) normalize their inputs.

// isMoonshotCompatRequest reports whether the target is a Moonshot / Kimi
// upstream (api.moonshot.cn, api.kimi.com, or a kimi-* model).
func isMoonshotCompatRequest(baseURL string, request *model.InternalLLMRequest) bool {
	lowerBase := strings.ToLower(strings.TrimSpace(baseURL))
	if strings.Contains(lowerBase, "moonshot") || strings.Contains(lowerBase, "kimi") {
		return true
	}
	if request == nil {
		return false
	}
	lowerModel := strings.ToLower(strings.TrimSpace(request.Model))
	return strings.Contains(lowerModel, "kimi") || strings.Contains(lowerModel, "moonshot")
}

// isZhipuCompatRequest reports whether the target is a Zhipu / GLM upstream
// (open.bigmodel.cn, api.z.ai, or a glm-* model).
func isZhipuCompatRequest(baseURL string, request *model.InternalLLMRequest) bool {
	lowerBase := strings.ToLower(strings.TrimSpace(baseURL))
	if strings.Contains(lowerBase, "bigmodel") || strings.Contains(lowerBase, "z.ai") {
		return true
	}
	if request == nil {
		return false
	}
	lowerModel := strings.ToLower(strings.TrimSpace(request.Model))
	return strings.Contains(lowerModel, "glm")
}

// sanitizeMoonshotRequest applies Moonshot-specific request tweaks.
//
// new-api (relay/channel/moonshot/adaptor.go) forces temperature=1.0 for the
// kimi-k2.6 family: the model rejects non-1.0 temperatures. Following new-api's
// test semantics, we only override when the client explicitly set a value
// (non-nil pointer); we leave a nil temperature untouched so the upstream
// default applies.
func sanitizeMoonshotRequest(request *model.InternalLLMRequest, baseURL string) {
	if request == nil || !isMoonshotCompatRequest(baseURL, request) {
		return
	}
	if !strings.Contains(strings.ToLower(request.Model), "kimi-k2.6") {
		return
	}
	if request.Temperature == nil {
		return
	}
	one := 1.0
	request.Temperature = &one
}

// sanitizeZhipuRequest applies Zhipu GLM-specific request tweaks.
//
// Ported from new-api relay/channel/zhipu_4v:
//   - TopP >= 1.0 is rejected by Zhipu; clamp to 0.99.
//   - Zhipu rejects data-URI image prefixes ("data:image/png;base64,...");
//     strip the prefix and forward raw base64 in image_url.url.
func sanitizeZhipuRequest(request *model.InternalLLMRequest, baseURL string) {
	if request == nil || !isZhipuCompatRequest(baseURL, request) {
		return
	}

	if request.TopP != nil && *request.TopP >= 1.0 {
		clamped := 0.99
		request.TopP = &clamped
	}

	for i := range request.Messages {
		stripZhipuDataURLPrefixes(&request.Messages[i])
	}
}

// stripZhipuDataURLPrefixes rewrites any image_url content part whose URL is a
// "data:image/...;base64," data URI down to its raw base64 payload.
func stripZhipuDataURLPrefixes(msg *model.Message) {
	if msg == nil || len(msg.Content.MultipleContent) == 0 {
		return
	}
	for i := range msg.Content.MultipleContent {
		part := &msg.Content.MultipleContent[i]
		if part.Type != "image_url" || part.ImageURL == nil {
			continue
		}
		part.ImageURL.URL = stripDataURLPrefix(part.ImageURL.URL)
	}
}

// stripDataURLPrefix removes a leading "data:image/<mime>;base64," prefix,
// returning the raw base64 body. Non-data-URI URLs are returned unchanged.
func stripDataURLPrefix(raw string) string {
	const marker = ";base64,"
	idx := strings.Index(strings.ToLower(raw), "data:image/")
	if idx != 0 {
		return raw
	}
	pos := strings.Index(raw, marker)
	if pos < 0 {
		return raw
	}
	return raw[pos+len(marker):]
}
