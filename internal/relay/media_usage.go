package relay

import (
	"encoding/json"

	"github.com/gypg/lodestar/internal/pkg/billingexpr"
)

const (
	// maxUsageObjectBytes caps how many bytes of a single "usage" object the
	// scanner retains. Real usage objects are well under 1 KiB; the cap keeps a
	// malformed or hostile upstream from growing this buffer without bound.
	// An over-long capture is abandoned rather than truncated — truncated JSON
	// would not parse anyway.
	maxUsageObjectBytes = 64 << 10
	// usageKey is the JSON object key that carries token usage.
	usageKey = "usage"
)

// mediaUsage is the upstream-reported token usage of one media request,
// normalized across the field spellings different providers use
// (input_tokens/prompt_tokens, *_tokens_details, ...).
//
// Zero value means "upstream reported nothing". Media endpoints must never
// fabricate tokens: a request with no usage keeps the request-param pricing
// path (param('size') and friends) and logs zero tokens, exactly as before.
type mediaUsage struct {
	// Totals as reported (or derived from details when only details exist).
	InputTokens  int64
	OutputTokens int64

	// Input sub-categories. TextTokens excludes the others.
	TextTokens       int64
	ImageTokens      int64
	AudioInputTokens int64
	CachedTokens     int64

	// Output sub-categories. TextOutputTokens excludes the others.
	TextOutputTokens  int64
	ImageOutputTokens int64
	AudioOutputTokens int64
}

// Valid reports whether upstream actually gave us a usable token count.
// Only a positive count counts: providers routinely echo a `usage` object with
// all-zero fields, and treating that as "measured" would let a zero-token
// expression silently replace param() pricing.
func (u mediaUsage) Valid() bool {
	return u.InputTokens > 0 || u.OutputTokens > 0 ||
		u.TextTokens > 0 || u.ImageTokens > 0 || u.AudioInputTokens > 0 || u.CachedTokens > 0 ||
		u.TextOutputTokens > 0 || u.ImageOutputTokens > 0 || u.AudioOutputTokens > 0
}

// TokenParams maps the normalized usage onto the billing-expression token
// dimensions. P and C are the *text* legs: sub-categories priced separately
// (image / audio / cache-read) are subtracted so an expression that sums all
// dimensions cannot double-count them — matching how TokenParams.P is
// documented ("auto-excludes sub-categories priced separately").
func (u mediaUsage) TokenParams() billingexpr.TokenParams {
	// The text leg is the input total minus every sub-category that gets its own
	// dimension. Cached tokens are a *subset* of the input total (not an extra
	// bucket), so they are subtracted here and priced via CR — the same split the
	// LLM path uses in metrics.go (cached*CacheRead + (prompt-cached)*Input).
	// TextTokens is deliberately not used directly: providers that report it also
	// report cache hits inside it, and P must not double-count what CR prices.
	promptText := u.InputTokens - u.ImageTokens - u.AudioInputTokens - u.CachedTokens
	completionText := u.OutputTokens - u.ImageOutputTokens - u.AudioOutputTokens

	return billingexpr.TokenParams{
		P:    float64(clampNonNegative(promptText)),
		C:    float64(clampNonNegative(completionText)),
		Len:  float64(clampNonNegative(u.InputTokens)),
		CR:   float64(clampNonNegative(u.CachedTokens)),
		Img:  float64(clampNonNegative(u.ImageTokens)),
		ImgO: float64(clampNonNegative(u.ImageOutputTokens)),
		AI:   float64(clampNonNegative(u.AudioInputTokens)),
		AO:   float64(clampNonNegative(u.AudioOutputTokens)),
	}
}

// clampFields floors every token field at 0. Called once, right after parsing,
// so nothing downstream has to reason about negative upstream input.
func (u *mediaUsage) clampFields() {
	u.InputTokens = clampNonNegative(u.InputTokens)
	u.OutputTokens = clampNonNegative(u.OutputTokens)
	u.TextTokens = clampNonNegative(u.TextTokens)
	u.ImageTokens = clampNonNegative(u.ImageTokens)
	u.AudioInputTokens = clampNonNegative(u.AudioInputTokens)
	u.CachedTokens = clampNonNegative(u.CachedTokens)
	u.TextOutputTokens = clampNonNegative(u.TextOutputTokens)
	u.ImageOutputTokens = clampNonNegative(u.ImageOutputTokens)
	u.AudioOutputTokens = clampNonNegative(u.AudioOutputTokens)
}

// clampNonNegative floors a token count at 0. Subtracting sub-categories from a
// reported total can go negative when a provider's details exceed its total
// (seen with double-counted image tokens); a negative dimension would turn a
// per-token expression into a credit.
func clampNonNegative(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

// mediaUsagePayload is the union of the `usage` shapes the media endpoints
// return. Images (gpt-image-1) use input_tokens/output_tokens with
// input_tokens_details; audio/rerank/moderation responses that pass through a
// chat-completions-shaped upstream use prompt_tokens/completion_tokens.
// Pointers are used where 0 and "absent" must stay distinguishable.
type mediaUsagePayload struct {
	Usage *struct {
		InputTokens      int64 `json:"input_tokens"`
		OutputTokens     int64 `json:"output_tokens"`
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`

		InputTokensDetails *struct {
			TextTokens   int64 `json:"text_tokens"`
			ImageTokens  int64 `json:"image_tokens"`
			AudioTokens  int64 `json:"audio_tokens"`
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		PromptTokensDetails *struct {
			TextTokens   int64 `json:"text_tokens"`
			ImageTokens  int64 `json:"image_tokens"`
			AudioTokens  int64 `json:"audio_tokens"`
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`

		OutputTokensDetails *struct {
			TextTokens  int64 `json:"text_tokens"`
			ImageTokens int64 `json:"image_tokens"`
			AudioTokens int64 `json:"audio_tokens"`
		} `json:"output_tokens_details"`
		CompletionTokensDetails *struct {
			TextTokens  int64 `json:"text_tokens"`
			ImageTokens int64 `json:"image_tokens"`
			AudioTokens int64 `json:"audio_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
}

// parseMediaUsage extracts and normalizes the `usage` object from a media
// response body. Returns (zero, false) when the body is not JSON, carries no
// usage object, or reports only zeros — callers must then keep param() pricing
// rather than bill zero tokens.
func parseMediaUsage(body []byte) (mediaUsage, bool) {
	if len(body) == 0 {
		return mediaUsage{}, false
	}

	var payload mediaUsagePayload
	if err := json.Unmarshal(body, &payload); err != nil || payload.Usage == nil {
		return mediaUsage{}, false
	}
	raw := payload.Usage

	usage := mediaUsage{
		InputTokens:  firstPositive(raw.InputTokens, raw.PromptTokens),
		OutputTokens: firstPositive(raw.OutputTokens, raw.CompletionTokens),
	}

	if in := raw.InputTokensDetails; in != nil {
		usage.TextTokens = in.TextTokens
		usage.ImageTokens = in.ImageTokens
		usage.AudioInputTokens = in.AudioTokens
		usage.CachedTokens = in.CachedTokens
	}
	if in := raw.PromptTokensDetails; in != nil {
		usage.TextTokens = firstPositive(usage.TextTokens, in.TextTokens)
		usage.ImageTokens = firstPositive(usage.ImageTokens, in.ImageTokens)
		usage.AudioInputTokens = firstPositive(usage.AudioInputTokens, in.AudioTokens)
		usage.CachedTokens = firstPositive(usage.CachedTokens, in.CachedTokens)
	}
	if out := raw.OutputTokensDetails; out != nil {
		usage.TextOutputTokens = out.TextTokens
		usage.ImageOutputTokens = out.ImageTokens
		usage.AudioOutputTokens = out.AudioTokens
	}
	if out := raw.CompletionTokensDetails; out != nil {
		usage.TextOutputTokens = firstPositive(usage.TextOutputTokens, out.TextTokens)
		usage.ImageOutputTokens = firstPositive(usage.ImageOutputTokens, out.ImageTokens)
		usage.AudioOutputTokens = firstPositive(usage.AudioOutputTokens, out.AudioTokens)
	}

	// Clamp every field at the trust boundary. A negative count is never
	// meaningful, and a negative *sub-category* is actively dangerous: the text
	// leg is computed by subtracting sub-categories from the total, so
	// input_tokens=100 with audio_tokens=-50 would bill P=150 — more than
	// upstream reported. Clamping here (not only in TokenParams) also keeps the
	// derived totals and the persisted relay_logs columns non-negative.
	usage.clampFields()

	// Providers that report only details and no total still need a usable
	// aggregate for Len / relay_logs / MaxTokens.
	//
	// CachedTokens is deliberately excluded from the sum: it is a *subset* of the
	// input total (how many of those tokens were cache hits), not an extra bucket.
	// Adding it would inflate the reported total — a 100-token request with 9
	// cached would log 109.
	if parts := usage.TextTokens + usage.ImageTokens + usage.AudioInputTokens; parts > usage.InputTokens {
		usage.InputTokens = parts
	}
	if parts := usage.TextOutputTokens + usage.ImageOutputTokens + usage.AudioOutputTokens; parts > usage.OutputTokens {
		usage.OutputTokens = parts
	}

	if !usage.Valid() {
		return mediaUsage{}, false
	}
	return usage, true
}

// firstPositive returns the first argument greater than zero, else 0. Providers
// spell the same field several ways and send 0 for the ones they don't use, so
// "first non-zero wins" is the normalization rule throughout.
func firstPositive(values ...int64) int64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

// usageKeyLiteral is the quoted key the scanner hunts for. Requiring the
// closing quote (and, below, a following colon + '{') keeps a string *value*
// of "usage" from being mistaken for the usage object.
var usageKeyLiteral = []byte(`"` + usageKey + `"`)

type usageScanState int

const (
	usageScanSearchKey usageScanState = iota
	usageScanAwaitColon
	usageScanAwaitBrace
	usageScanCapture
)

// usageScanner extracts the `usage` object out of a byte stream *without*
// buffering the stream. Media responses carry multi-megabyte base64 images, so
// the earlier plan of ReadAll-then-parse would have turned every image request
// into a full-body allocation; instead the bytes are forwarded to the client as
// they arrive and only the usage object (capped at maxUsageObjectBytes) is kept.
//
// It is a byte-level scanner, not a JSON parser: it looks for `"usage"` followed
// by `:` and `{`, then copies the brace-balanced object, tracking string state so
// that braces or quotes inside string values cannot end the capture early.
// The scanner is stateful across Write calls, so a usage object split across
// chunk boundaries (the SSE case) is still captured.
//
// Not safe for concurrent use; one scanner belongs to one response.
type usageScanner struct {
	state      usageScanState
	keyMatched int
	depth      int
	inString   bool
	escaped    bool
	buf        []byte
	captured   []byte
}

// Write makes the scanner an io.Writer so it can sit in an io.MultiWriter next
// to the client, letting the response stream through without being buffered.
// It never fails and never short-writes: reporting an error here would abort
// io.Copy and break a response that is otherwise fine.
func (s *usageScanner) Write(p []byte) (int, error) {
	s.Scan(p)
	return len(p), nil
}

// Scan feeds the next chunk of response bytes to the scanner.
func (s *usageScanner) Scan(chunk []byte) {
	for _, b := range chunk {
		switch s.state {
		case usageScanSearchKey:
			s.matchKeyByte(b)
		case usageScanAwaitColon:
			switch {
			case isJSONSpace(b):
			case b == ':':
				s.state = usageScanAwaitBrace
			default:
				// Not `"usage":` after all (e.g. a string value). This byte may
				// itself open the real key, so re-run it through the matcher.
				s.state = usageScanSearchKey
				s.matchKeyByte(b)
			}
		case usageScanAwaitBrace:
			switch {
			case isJSONSpace(b):
			case b == '{':
				s.buf = s.buf[:0]
				s.buf = append(s.buf, b)
				s.depth = 1
				s.inString = false
				s.escaped = false
				s.state = usageScanCapture
			default:
				// `"usage": null` and friends — nothing to capture.
				s.state = usageScanSearchKey
				s.matchKeyByte(b)
			}
		case usageScanCapture:
			s.captureByte(b)
		}
	}
}

// matchKeyByte advances the rolling match against `"usage"`.
func (s *usageScanner) matchKeyByte(b byte) {
	if b == usageKeyLiteral[s.keyMatched] {
		s.keyMatched++
		if s.keyMatched == len(usageKeyLiteral) {
			s.keyMatched = 0
			s.state = usageScanAwaitColon
		}
		return
	}
	// Restart the match; a quote can begin the next key.
	if b == '"' {
		s.keyMatched = 1
	} else {
		s.keyMatched = 0
	}
}

// captureByte accumulates one byte of the usage object, honoring JSON string
// escaping so that `{`/`}`/`"` inside a string cannot unbalance the capture.
func (s *usageScanner) captureByte(b byte) {
	if len(s.buf) >= maxUsageObjectBytes {
		// Implausibly large for a usage object; abandon this capture (a
		// truncated prefix would not parse) and hunt for the next one.
		s.reset()
		return
	}
	s.buf = append(s.buf, b)

	if s.inString {
		switch {
		case s.escaped:
			s.escaped = false
		case b == '\\':
			s.escaped = true
		case b == '"':
			s.inString = false
		}
		return
	}

	switch b {
	case '"':
		s.inString = true
	case '{':
		s.depth++
	case '}':
		s.depth--
		if s.depth == 0 {
			// Keep the newest complete object: an SSE stream repeats `usage`
			// and only the final frame carries the settled totals.
			s.captured = append(s.captured[:0], s.buf...)
			s.reset()
		}
	}
}

// reset returns the scanner to key-hunting without discarding what it captured.
func (s *usageScanner) reset() {
	s.buf = s.buf[:0]
	s.depth = 0
	s.inString = false
	s.escaped = false
	s.keyMatched = 0
	s.state = usageScanSearchKey
}

// Usage returns the normalized usage captured from the stream, or (zero, false)
// if the stream carried none.
func (s *usageScanner) Usage() (mediaUsage, bool) {
	if len(s.captured) == 0 {
		return mediaUsage{}, false
	}
	// Re-wrap the bare object so the shared payload parser applies.
	wrapped := make([]byte, 0, len(s.captured)+len(usageKeyLiteral)+3)
	wrapped = append(wrapped, '{')
	wrapped = append(wrapped, usageKeyLiteral...)
	wrapped = append(wrapped, ':')
	wrapped = append(wrapped, s.captured...)
	wrapped = append(wrapped, '}')
	return parseMediaUsage(wrapped)
}

func isJSONSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
