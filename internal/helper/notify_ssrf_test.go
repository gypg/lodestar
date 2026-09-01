package helper

import (
	"encoding/json"
	"strings"
	"testing"

	appmodel "github.com/gypg/lodestar/internal/model"
)

// ssrfRefusalMarker 是 AssertSafeURL/AssertSafeHost 拒绝时给业务侧拼出的错误关键词。
// 工单 T1 的核心断言：失败必须是因为 SSRF 校验拒绝，而不是因为真的拨号了。
// 真拨号失败的错误里会带 "connection refused" / "no route to host" /
// "context deadline exceeded" —— 出现这些就说明预言机仍在。
const ssrfRefusalMarker = "url is not allowed"

// dialFailureMarkers 是真的把请求发出去了才会出现的拨号错误特征。
// T1 断言这条：修复后这些字样绝不能出现在三种风险类型的内网 URL 错误里。
// 含跨平台变体（Linux "connection refused" / Windows "connectex: ... actively refused"）。
var dialFailureMarkers = []string{
	"connection refused",
	"actively refused", // Windows connectex 变体
	"no route to host",
	"context deadline exceeded",
	"i/o timeout",
	"connectex", // Windows 拨号失败前缀
	"dialed",    // "dial tcp ..." 拨号动作痕迹
	"dial tcp",  // 显式拨号错误
}

// isSSRFRefusal 报告 err 是否为 SSRF 校验拒绝（而非拨号失败）。
func isSSRFRefusal(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, ssrfRefusalMarker)
}

// containsDialFailure 报告 err 是否带有真拨号失败的痕迹 —— 出现意味着请求确实发出去了，
// 即预言机仍在。这是缺陷存在的证据（修复前 T1 应观察到）。
func containsDialFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, m := range dialFailureMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// payloadForTest 构造一个最小的告警载荷，够 Send* 函数组装请求即可。
func payloadForTest() AlertWebhookPayload {
	return AlertWebhookPayload{
		RuleName: "test-rule",
		State:    "test",
		Message:  "test message",
		Time:     "2026-09-01T00:00:00Z",
	}
}

// TestT1WebhookInternalURLRejected 钉死 WO-025 T1：webhook 指向内网时，
// 必须返回 SSRF 校验拒绝，且错误文案里不能出现拨号失败字样。
func TestT1WebhookInternalURLRejected(t *testing.T) {
	internalURLs := []string{
		"http://127.0.0.1:9999/hook",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1:8080/hook",
	}
	for _, u := range internalURLs {
		ch := &appmodel.AlertNotifChannel{
			Name: "t1-webhook", Type: string(appmodel.AlertNotifWebhook), URL: u,
		}
		err := SendWebhook(ch, payloadForTest())
		if err == nil {
			t.Fatalf("webhook %s: expected SSRF refusal error, got nil (request went out)", u)
		}
		if !isSSRFRefusal(err) {
			t.Fatalf("webhook %s: error must be SSRF refusal, got: %v", u, err)
		}
		if containsDialFailure(err) {
			t.Fatalf("webhook %s: error contains dial-failure marker, oracle still alive: %v", u, err)
		}
	}
}

// TestT1GotifyInternalURLRejected 同上，gotify 路径。通过 cfg.ServerURL 直接给内网地址。
func TestT1GotifyInternalURLRejected(t *testing.T) {
	internalURLs := []string{
		"http://127.0.0.1:9999",
		"http://169.254.169.254",
		"http://10.0.0.1:8080",
	}
	for _, u := range internalURLs {
		cfg := appmodel.GotifyConfig{ServerURL: u, Token: "tok"}
		ch := &appmodel.AlertNotifChannel{
			Name: "t1-gotify", Type: string(appmodel.AlertNotifGotify),
		}
		ch.Config = mustMarshal(t, cfg)
		err := SendGotify(ch, payloadForTest())
		if err == nil {
			t.Fatalf("gotify %s: expected SSRF refusal error, got nil", u)
		}
		if !isSSRFRefusal(err) {
			t.Fatalf("gotify %s: error must be SSRF refusal, got: %v", u, err)
		}
		if containsDialFailure(err) {
			t.Fatalf("gotify %s: error contains dial-failure marker, oracle still alive: %v", u, err)
		}
	}
}

// TestT1GotifyFallbackInternalURLRejected 钉死 gotify 的回落路径：
// cfg.ServerURL 为空时回落到 channel.URL —— 这条回落后的 URL 也必须被校验。
func TestT1GotifyFallbackInternalURLRejected(t *testing.T) {
	ch := &appmodel.AlertNotifChannel{
		Name:   "t1-gotify-fallback",
		Type:   string(appmodel.AlertNotifGotify),
		URL:    "http://127.0.0.1:9999",
		Secret: "tok", // 回落 token
	}
	// cfg 留空 → ServerURL 回落 channel.URL，Token 回落 channel.Secret
	ch.Config = mustMarshal(t, appmodel.GotifyConfig{})
	err := SendGotify(ch, payloadForTest())
	if err == nil {
		t.Fatalf("gotify fallback: expected SSRF refusal, got nil")
	}
	if !isSSRFRefusal(err) {
		t.Fatalf("gotify fallback: error must be SSRF refusal, got: %v", err)
	}
	if containsDialFailure(err) {
		t.Fatalf("gotify fallback: oracle still alive: %v", err)
	}
}

// TestT1NtfyInternalURLRejected ntfy 路径。给完整 http(s):// 内网 URL，不触发补 scheme。
func TestT1NtfyInternalURLRejected(t *testing.T) {
	internalURLs := []string{
		"http://127.0.0.1:9999/mytopic",
		"http://169.254.169.254/test",
		"http://10.0.0.1:8080/topic",
	}
	for _, u := range internalURLs {
		cfg := appmodel.NtfyConfig{TopicURL: u}
		ch := &appmodel.AlertNotifChannel{
			Name: "t1-ntfy", Type: string(appmodel.AlertNotifNtfy),
		}
		ch.Config = mustMarshal(t, cfg)
		err := SendNtfy(ch, payloadForTest())
		if err == nil {
			t.Fatalf("ntfy %s: expected SSRF refusal, got nil", u)
		}
		if !isSSRFRefusal(err) {
			t.Fatalf("ntfy %s: error must be SSRF refusal, got: %v", u, err)
		}
		if containsDialFailure(err) {
			t.Fatalf("ntfy %s: oracle still alive: %v", u, err)
		}
	}
}

// TestT2PublicURLNotRefusedBySSRF 钉死 T2：公网合法域名不能被 SSRF 校验拒绝。
// 用 example.com（能解析、且其 IP 不在 IsDisallowedIP 名单内）。
// 允许后续因对端不是真 webhook 而失败 —— 只断言“不是校验拒绝”。
func TestT2PublicURLNotRefusedBySSRF(t *testing.T) {
	ch := &appmodel.AlertNotifChannel{
		Name: "t2-webhook", Type: string(appmodel.AlertNotifWebhook),
		URL: "https://example.com/hook",
	}
	err := SendWebhook(ch, payloadForTest())
	if err != nil && isSSRFRefusal(err) {
		t.Fatalf("public URL example.com must NOT be refused by SSRF check, got: %v", err)
	}
	// err 为 nil（对端恰好 2xx）或非 SSRF 拒绝（如 webhook responded 404）都算 T2 通过。
}

// TestT2NtfyPublicURLNotRefusedBySSRF ntfy 公网域名同样不能被拒。
func TestT2NtfyPublicURLNotRefusedBySSRF(t *testing.T) {
	cfg := appmodel.NtfyConfig{TopicURL: "https://example.com/topic"}
	ch := &appmodel.AlertNotifChannel{
		Name: "t2-ntfy", Type: string(appmodel.AlertNotifNtfy),
	}
	ch.Config = mustMarshal(t, cfg)
	err := SendNtfy(ch, payloadForTest())
	if err != nil && isSSRFRefusal(err) {
		t.Fatalf("public ntfy URL example.com must NOT be refused by SSRF, got: %v", err)
	}
}

// TestT3NtfySchemeCompletionThenReject 钉死 T3（顺序守卫）：
// cfg.TopicURL = "127.0.0.1:9999/mytopic"（无 scheme、含 . 与 /）→
// 补成 https://127.0.0.1:9999/mytopic → 必须被拒。
// 若校验放在补 scheme 之前的原始字段上，则会漏（原始字段无 scheme，Parse 失败或被放行）。
func TestT3NtfySchemeCompletionThenReject(t *testing.T) {
	cfg := appmodel.NtfyConfig{TopicURL: "127.0.0.1:9999/mytopic"}
	ch := &appmodel.AlertNotifChannel{
		Name: "t3-ntfy", Type: string(appmodel.AlertNotifNtfy),
	}
	ch.Config = mustMarshal(t, cfg)
	err := SendNtfy(ch, payloadForTest())
	if err == nil {
		t.Fatalf("ntfy scheme-completed internal URL: expected SSRF refusal, got nil")
	}
	if !isSSRFRefusal(err) {
		t.Fatalf("ntfy scheme-completed internal URL: error must be SSRF refusal, got: %v", err)
	}
	if containsDialFailure(err) {
		t.Fatalf("ntfy scheme-completed internal URL: oracle still alive: %v", err)
	}
}

// TestT3bNtfyLegitUnschemeNotRejected 是 M-d 守卫的补强用例。
//
// 工单 T3 用 127.0.0.1:9999/mytopic 作输入 —— 但这个输入在 M-d（校验补 scheme 前的
// 原始字段）下也会被拒（url.Parse 因 "first path segment contains colon" 失败），
// 拒绝理由虽错但错误经 fmt.Errorf("ntfy url is not allowed: %w", err) 拼接后仍带
// "url is not allowed" 前缀，isSSRFRefusal 判不出区别 → 工单原 T3 测不死 M-d。
//
// M-d 的真正危害是“误伤合法的无 scheme ntfy 配置”：ntfy 设计上允许 TopicURL 写
// "example.com/mytopic"（补成 https://example.com/mytopic）或 "mytopic"
// （补成 https://ntfy.sh/mytopic）。校验若挪到补 scheme 之前，这些合法配置会被
// AssertSafeURL 以 scheme 缺失拒绝，整条 ntfy 通道无法使用。
//
// 本用例用合法公网无 scheme 输入 example.com/mytopic：
//   - 修复态：补成 https://example.com/mytopic → 公网放行 → err 不是校验拒绝（拨号失败也行）→ 过
//   - M-d 态：校验原始 example.com/mytopic → AssertSafeURL 拒（scheme 缺失）→
//     err 带前缀 "ntfy url is not allowed" → isSSRFRefusal=true → 本用例“合法配置
//     不应被 SSRF 拒绝”的断言失败 → 红
func TestT3bNtfyLegitUnschemeNotRejected(t *testing.T) {
	cfg := appmodel.NtfyConfig{TopicURL: "example.com/mytopic"} // 合法公网无 scheme
	ch := &appmodel.AlertNotifChannel{
		Name: "t3b-ntfy", Type: string(appmodel.AlertNotifNtfy),
	}
	ch.Config = mustMarshal(t, cfg)
	err := SendNtfy(ch, payloadForTest())
	if err != nil && isSSRFRefusal(err) {
		t.Fatalf("legit unscheme ntfy config example.com/mytopic must not be SSRF-refused "+
			"(refusal means SSRF check ran before scheme completion — M-d variant), got: %v", err)
	}
}

// TestT4HardcodedEndpointNotBroken 钉死 T4：硬编码官方域名类型（取 telegram 代表）
// 在合法配置下，不能因为新加的逻辑而失败。telegram host 固定 api.telegram.org，
// 不应走 SSRF 校验路径 —— 校验只加在 webhook/gotify/ntfy 三种。
//
// 这里不真发请求到 telegram（会失败且与工单无关），用一个无效 token 触发“创建请求”
// 之前的必填校验链路：BotToken 非空、ChatID 非空即通过参数校验，进入请求构造。
// 真发会因 token 无效被 telegram 拒（返回 API error），那不是 SSRF 拒绝、也不是 panic。
// 为避免依赖外网，本测试只断言：合法配置下函数不因新增校验逻辑返回 SSRF 拒绝错误。
func TestT4HardcodedEndpointNotBroken(t *testing.T) {
	cfg := appmodel.TelegramConfig{BotToken: "000:fake", ChatID: "123"}
	ch := &appmodel.AlertNotifChannel{
		Name: "t4-telegram", Type: string(appmodel.AlertNotifTelegram),
	}
	ch.Config = mustMarshal(t, cfg)
	err := SendTelegram(ch, payloadForTest())
	// 允许任何错误（外网不通/token 无效），但绝不能是 SSRF 拒绝 ——
	// telegram 是硬编码端点，不应被 SSRF 校验碰。
	if err != nil && isSSRFRefusal(err) {
		t.Fatalf("telegram hardcoded endpoint must not be hit by SSRF check, got: %v", err)
	}
}

// mustMarshal 是测试辅助：把 v 序列化成 channel.Config 用的 JSON 字符串。
func mustMarshal(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return string(b)
}
