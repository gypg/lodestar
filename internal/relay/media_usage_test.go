package relay

/*
P1 #11（扫描文档 §10.1）—— 媒体真实 usage 采集的单元测试。

背景：10 条 /v1 媒体路由的响应体原本被 io.Copy 直接透传，上游报的 usage
从未被读取，relay_logs 的 input_tokens/output_tokens 恒 0。本文件锁住
usageScanner（不缓冲全body 的流式扫描器）与 mediaUsage 的归一化/映射规则。

★ 硬规则（SPEC 写死，测试必须锁住）：上游没报 usage 时**禁止**伪造 token。
所以 parseMediaUsage 对"无 usage / 全零 usage"必须返回 ok=false，
让计费回落到原有的纯 param() 路径。
*/

import (
	"fmt"
	"strings"
	"testing"
)

// TestParseMediaUsage_imagesShape 锁住 gpt-image-1 的 input_tokens/output_tokens
// + input_tokens_details 形状，并验证 P 是"文本腿"（已扣掉单独计价的图片 token）。
func TestParseMediaUsage_imagesShape(t *testing.T) {
	body := []byte(`{"created":1,"data":[{"b64_json":"aGk="}],
		"usage":{"input_tokens":100,"output_tokens":40,
		"input_tokens_details":{"text_tokens":30,"image_tokens":70}}}`)

	usage, ok := parseMediaUsage(body)
	if !ok {
		t.Fatalf("parseMediaUsage: want ok=true for a real images usage object")
	}
	if usage.InputTokens != 100 {
		t.Errorf("InputTokens: want 100, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 40 {
		t.Errorf("OutputTokens: want 40, got %d", usage.OutputTokens)
	}
	if usage.ImageTokens != 70 {
		t.Errorf("ImageTokens: want 70, got %d", usage.ImageTokens)
	}
	if usage.TextTokens != 30 {
		t.Errorf("TextTokens: want 30, got %d", usage.TextTokens)
	}

	p := usage.TokenParams()
	// P 是文本腿：总数 100 减去单独计价的图片 70（这里无缓存），= 30。
	// 不能直接用 input_tokens 总数，否则同时按 P 和 Img 计价的表达式会把图片算两遍。
	if p.P != 30 {
		t.Errorf("TokenParams.P: want 30 (100 total - 70 image), got %v", p.P)
	}
	if p.Img != 70 {
		t.Errorf("TokenParams.Img: want 70, got %v", p.Img)
	}
	if p.C != 40 {
		t.Errorf("TokenParams.C: want 40, got %v", p.C)
	}
	// Len 是分层条件用的输入总长，必须是上游报的总数。
	if p.Len != 100 {
		t.Errorf("TokenParams.Len: want 100 (reported total), got %v", p.Len)
	}
}

// TestParseMediaUsage_chatShape 锁住 prompt_tokens/completion_tokens 拼写
// （rerank/moderation/MiMo TTS 这类走 chat 形状上游的端点）。
func TestParseMediaUsage_chatShape(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":11,"completion_tokens":7,
		"prompt_tokens_details":{"cached_tokens":4}}}`)

	usage, ok := parseMediaUsage(body)
	if !ok {
		t.Fatalf("parseMediaUsage: want ok=true for chat-shaped usage")
	}
	if usage.InputTokens != 11 || usage.OutputTokens != 7 {
		t.Errorf("tokens: want in=11 out=7, got in=%d out=%d", usage.InputTokens, usage.OutputTokens)
	}
	if usage.CachedTokens != 4 {
		t.Errorf("CachedTokens: want 4, got %d", usage.CachedTokens)
	}
	p := usage.TokenParams()
	// 无 text_tokens 明细 → 从总数里扣掉缓存读（11-4=7）。
	if p.P != 7 {
		t.Errorf("TokenParams.P: want 7 (11 total - 4 cached), got %v", p.P)
	}
	if p.CR != 4 {
		t.Errorf("TokenParams.CR: want 4, got %v", p.CR)
	}
}

// TestParseMediaUsage_noFabrication ★ 这是 SPEC 的硬规则守卫：
// 上游没给 usage、或给了全零 usage，都必须返回 ok=false。
// 若这里退化成 ok=true，计费会用全零 TokenParams 覆盖 param() 定价，
// 把"按 size 收费"的媒体模型静默改成 $0（BUG-003 回归）。
func TestParseMediaUsage_noFabrication(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"nil body", ""},
		{"no usage key", `{"created":1,"data":[{"b64_json":"aGk="}]}`},
		{"usage null", `{"usage":null}`},
		{"all-zero usage", `{"usage":{"input_tokens":0,"output_tokens":0}}`},
		{"zero details", `{"usage":{"input_tokens":0,"input_tokens_details":{"image_tokens":0}}}`},
		{"not json", `this is not json at all`},
		{"binary-ish", "\x00\x01\x02\x03"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			usage, ok := parseMediaUsage([]byte(tc.body))
			if ok {
				t.Errorf("parseMediaUsage(%q): want ok=false (never fabricate tokens), got ok=true %+v", tc.body, usage)
			}
			if usage.Valid() {
				t.Errorf("usage.Valid(): want false, got true for %q", tc.body)
			}
		})
	}
}

// TestParseMediaUsage_detailsOnlyDerivesTotal 上游只给明细不给总数时，
// 总数要能从明细推出来（否则 relay_logs 的 input_tokens 仍是 0）。
func TestParseMediaUsage_detailsOnlyDerivesTotal(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens_details":{"text_tokens":5,"image_tokens":15},
		"output_tokens_details":{"image_tokens":8}}}`)

	usage, ok := parseMediaUsage(body)
	if !ok {
		t.Fatalf("parseMediaUsage: want ok=true when only details are present")
	}
	if usage.InputTokens != 20 {
		t.Errorf("InputTokens: want 20 (5+15 derived from details), got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 8 {
		t.Errorf("OutputTokens: want 8 (derived), got %d", usage.OutputTokens)
	}
	if p := usage.TokenParams(); p.ImgO != 8 {
		t.Errorf("TokenParams.ImgO: want 8, got %v", p.ImgO)
	}
}

// TestParseMediaUsage_cachedTokensAreSubsetNotAddend
// ★ 这条是写测试时被死期望值逼出来的真实缺陷（第一版把 cached 当成额外的桶）：
// cached_tokens 是 input_tokens 的**子集**（其中有多少命中了缓存），不是另加的一桶。
// 把它加进总数 → 100 token 的请求会记成 109，relay_logs 和看板系统性偏高。
// 同理 P 必须扣掉 cached（由 CR 单独计价），否则缓存 token 被算两遍。
func TestParseMediaUsage_cachedTokensAreSubsetNotAddend(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":100,"output_tokens":40,
		"input_tokens_details":{"text_tokens":30,"image_tokens":70,"cached_tokens":9}}}`)

	usage, ok := parseMediaUsage(body)
	if !ok {
		t.Fatalf("parseMediaUsage: want ok=true")
	}
	if usage.InputTokens != 100 {
		t.Errorf("InputTokens: want 100 (as reported; cached is a subset), got %d — "+
			"109 means cached_tokens was summed into the total", usage.InputTokens)
	}
	p := usage.TokenParams()
	if p.P != 21 {
		t.Errorf("TokenParams.P: want 21 (100 - 70 image - 9 cached), got %v — "+
			"30 means cached was left inside P and is double-counted against CR", p.P)
	}
	if p.CR != 9 {
		t.Errorf("TokenParams.CR: want 9, got %v", p.CR)
	}
	// P + Img + CR 必须正好等于上游报的输入总数，不多不少。
	if total := p.P + p.Img + p.CR; total != 100 {
		t.Errorf("P+Img+CR: want exactly 100 (partition of the reported total), got %v", total)
	}
}

// TestParseMediaUsage_negativeSubCategoryCannotInflateBilling
// ★ 这条是 M5 变异存活后用探针实测挖出来的**真实可达缺陷**（不是变异本身的产物）：
// 上游报 input_tokens=100 + audio_tokens=-50 时，文本腿由"总数减去各子类"算出，
// 减一个负数 → P=150 > 上游报的 100，**按 token 计价的表达式会超额扣费**。
// Valid() 挡不住它（input_tokens=100 使其为真）。修法是在解析边界把所有字段夹到 >=0。
// 断言写死"P 恰好等于 100"，而不是"P 不大于某值"。
func TestParseMediaUsage_negativeSubCategoryCannotInflateBilling(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":100,"output_tokens":10,
		"input_tokens_details":{"audio_tokens":-50,"image_tokens":-20,"cached_tokens":-5}}}`)

	usage, ok := parseMediaUsage(body)
	if !ok {
		t.Fatalf("parseMediaUsage: want ok=true (input_tokens=100 is valid)")
	}
	if usage.AudioInputTokens != 0 || usage.ImageTokens != 0 || usage.CachedTokens != 0 {
		t.Errorf("negative sub-categories must be clamped to 0, got audio=%d image=%d cached=%d",
			usage.AudioInputTokens, usage.ImageTokens, usage.CachedTokens)
	}

	p := usage.TokenParams()
	if p.P != 100 {
		t.Errorf("TokenParams.P: want exactly 100 (the reported total), got %v — "+
			"150 means negative sub-categories were subtracted and the request is overbilled", p.P)
	}
	if p.C != 10 {
		t.Errorf("TokenParams.C: want exactly 10, got %v", p.C)
	}
}

// TestMediaUsage_tokenParamsNeverNegative 明细之和超过上游报的总数时
// （见过 image_tokens 被重复计入的上游），P/C 必须夹到 0 而不是变负数——
// 负维度会把按 token 计价的表达式变成"倒找钱"。
func TestMediaUsage_tokenParamsNeverNegative(t *testing.T) {
	usage := mediaUsage{InputTokens: 10, ImageTokens: 40, OutputTokens: 3, ImageOutputTokens: 9}
	p := usage.TokenParams()
	if p.P != 0 {
		t.Errorf("TokenParams.P: want 0 (clamped, not negative), got %v", p.P)
	}
	if p.C != 0 {
		t.Errorf("TokenParams.C: want 0 (clamped, not negative), got %v", p.C)
	}
}

// TestUsageScanner_jsonStream 扫描器在一次性写入下能抽出 usage。
func TestUsageScanner_jsonStream(t *testing.T) {
	s := &usageScanner{}
	s.Scan([]byte(`{"data":[{"b64_json":"AAAA"}],"usage":{"input_tokens":12,"output_tokens":3}}`))

	usage, ok := s.Usage()
	if !ok {
		t.Fatalf("scanner found no usage in a body that has one")
	}
	if usage.InputTokens != 12 || usage.OutputTokens != 3 {
		t.Errorf("scanned usage: want in=12 out=3, got in=%d out=%d", usage.InputTokens, usage.OutputTokens)
	}
}

// TestUsageScanner_splitAcrossChunks ★ 扫描器是跨 Write 有状态的：
// usage 对象被切成任意小块（SSE 逐行、io.Copy 32KiB 分块都会发生）也必须抽到。
// 逐字节喂是最严的切分。
func TestUsageScanner_splitAcrossChunks(t *testing.T) {
	body := `{"data":[],"usage":{"input_tokens":77,"output_tokens":9}}`
	for _, chunk := range []int{1, 2, 3, 7, 13} {
		t.Run(fmt.Sprintf("chunk=%d", chunk), func(t *testing.T) {
			s := &usageScanner{}
			for i := 0; i < len(body); i += chunk {
				end := i + chunk
				if end > len(body) {
					end = len(body)
				}
				s.Scan([]byte(body[i:end]))
			}
			usage, ok := s.Usage()
			if !ok {
				t.Fatalf("usage lost when split into %d-byte chunks", chunk)
			}
			if usage.InputTokens != 77 || usage.OutputTokens != 9 {
				t.Errorf("split scan: want in=77 out=9, got in=%d out=%d", usage.InputTokens, usage.OutputTokens)
			}
		})
	}
}

// TestUsageScanner_ignoresUsageInsideStrings 扫描器是字节级的，所以必须证明
// 它不会把字符串**值**里的 "usage" 当成 usage 对象，也不会被字符串里的
// 花括号/引号搞乱嵌套计数。否则用户 prompt 里写个 `"usage":{` 就能让计费读到假 token。
func TestUsageScanner_ignoresUsageInsideStrings(t *testing.T) {
	// revised_prompt 里塞了一个假的 usage 片段（含转义引号和花括号）。
	body := `{"data":[{"revised_prompt":"talk about \"usage\":{\"input_tokens\":999} and {braces}"}],` +
		`"usage":{"input_tokens":5,"output_tokens":2}}`

	s := &usageScanner{}
	s.Scan([]byte(body))
	usage, ok := s.Usage()
	if !ok {
		t.Fatalf("scanner found no usage")
	}
	// 999 说明它读了字符串里的假 usage。
	if usage.InputTokens != 5 {
		t.Errorf("InputTokens: want 5 (the real usage object), got %d — scanner was fooled by a string value", usage.InputTokens)
	}
}

// TestUsageScanner_usageValueFollowedByObjectIsNotCaptured
// ★ M11 变异（去掉 `"usage"` 后必须紧跟 `:` 的要求）存活后补的。
// 场景：数组里出现字符串 "usage"，紧接着是一个无关对象
// （`{"tags":["usage",{"input_tokens":999}]}`）。没有冒号守卫时，逗号顶替了
// 冒号的位置，下一个 `{` 就被当成 usage 对象捕获 → **凭空造出 999 个 token 并计费**。
// 这正是"禁止伪造 token"硬规则的另一面：不仅不能在无 usage 时填零值假数据，
// 也不能把别的对象误认成 usage。
func TestUsageScanner_usageValueFollowedByObjectIsNotCaptured(t *testing.T) {
	body := `{"tags":["usage",{"input_tokens":999,"output_tokens":888}],"data":[]}`
	s := &usageScanner{}
	s.Scan([]byte(body))

	usage, ok := s.Usage()
	if ok {
		t.Errorf("scanner fabricated usage from a body that has none: in=%d out=%d (captured=%q) — "+
			"the string \"usage\" in an array was treated as a key", usage.InputTokens, usage.OutputTokens, string(s.captured))
	}
}

// TestUsageScanner_braceInsideUsageStringValue
// ★ M10 变异（删掉 captureByte 里的字符串状态跟踪）存活后补的。
// 上面那条 ignoresUsageInsideStrings 守的是"字符串里的假 usage 键不被误认"，
// 它在 M10 下照绿——因为假 usage 在**真 usage 之前**，扫描器最终还是抓到了真的。
// 这条守的是另一半：usage 对象**自己的字符串值**里出现 `}` 时，
// 不能提前结束捕获（实测 M10 下捕获停在 `{"note":"a}`，usage 整个丢失 → 静默记 0 token）。
func TestUsageScanner_braceInsideUsageStringValue(t *testing.T) {
	cases := []struct{ name, body string }{
		{"brace in string value", `{"usage":{"note":"a}b","input_tokens":42,"output_tokens":7}}`},
		{"closing brace only", `{"usage":{"tier":"}","input_tokens":42,"output_tokens":7}}`},
		{"escaped quote then brace", `{"usage":{"tier":"x\"}y","input_tokens":42,"output_tokens":7}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &usageScanner{}
			s.Scan([]byte(tc.body))
			usage, ok := s.Usage()
			if !ok {
				t.Fatalf("usage lost: a brace inside the usage object's own string value truncated the capture (captured=%q)", string(s.captured))
			}
			if usage.InputTokens != 42 || usage.OutputTokens != 7 {
				t.Errorf("want in=42 out=7, got in=%d out=%d", usage.InputTokens, usage.OutputTokens)
			}
		})
	}
}

// TestUsageScanner_sseKeepsLastUsage 流式响应每帧都可能带 usage，
// 只有最后一帧是结算值。扫描器必须保留**最新**的完整对象。
func TestUsageScanner_sseKeepsLastUsage(t *testing.T) {
	s := &usageScanner{}
	s.Scan([]byte("data: {\"type\":\"partial\",\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}\n\n"))
	s.Scan([]byte("data: {\"type\":\"completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":25}}\n\n"))
	s.Scan([]byte("data: [DONE]\n\n"))

	usage, ok := s.Usage()
	if !ok {
		t.Fatalf("scanner found no usage in SSE stream")
	}
	if usage.OutputTokens != 25 {
		t.Errorf("OutputTokens: want 25 (final frame), got %d", usage.OutputTokens)
	}
}

// TestUsageScanner_oversizedUsageAbandoned 上游给一个畸形的巨大 usage 对象时，
// 扫描器不能无界增长。超过上限就放弃该次捕获（截断的 JSON 也解析不了）。
func TestUsageScanner_oversizedUsageAbandoned(t *testing.T) {
	s := &usageScanner{}
	s.Scan([]byte(`{"usage":{"junk":"`))
	s.Scan([]byte(strings.Repeat("x", maxUsageObjectBytes+1024)))
	s.Scan([]byte(`","input_tokens":5}}`))

	if usage, ok := s.Usage(); ok {
		t.Errorf("oversized usage should be abandoned, got ok=true %+v", usage)
	}
}

// TestUsageScanner_writerDoesNotBreakCopy 扫描器作为 io.Writer 挂在
// io.MultiWriter 里，必须永不报错、永不短写——否则 io.Copy 会中断一个本来正常的响应。
func TestUsageScanner_writerDoesNotBreakCopy(t *testing.T) {
	s := &usageScanner{}
	payload := []byte(`{"usage":{"input_tokens":1}}`)
	n, err := s.Write(payload)
	if err != nil {
		t.Fatalf("usageScanner.Write returned error %v — would abort io.Copy mid-response", err)
	}
	if n != len(payload) {
		t.Fatalf("usageScanner.Write short write: want %d, got %d — io.Copy treats this as ErrShortWrite", len(payload), n)
	}
}
