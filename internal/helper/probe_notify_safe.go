package helper

/*
Lodestar — 探测通知安全摘要（WO-031）。

外发通知（webhook/Gotify/邮件/Telegram/飞书/钉钉/企业微信/ntfy）不允许携带低层
错误原文：*url.Error 的文案以完整 URL 开头（Gemini 适配器还会把渠道 key 放在
?key= 里），DNS 失败/连接拒绝/TLS 错误都会把上游地址乃至凭据送出站。

设计不是"正则删 key="——未知 query 参数、userinfo、嵌套 URL、换行注入、不同
key 形态都删不干净。是**类别白名单**：从错误文本识别出固定类别（network failure /
timeout / HTTP status / invalid response…），外发只有类别 + 数字 + 固定文案，
任何未识别文本一律折叠为 internal failure。原文保留在受控服务端日志（本文件不
动日志路径）。
*/

import (
	"math"
	"strconv"
	"strings"
)

// safeProbeNotifyCategory 把一条（可能不可信的）探测错误文本映射为固定的安全类别。
// 返回值是枚举文案，直接进外发通知；绝不包含输入原文的任何片段。
func safeProbeNotifyCategory(msg string) string {
	m := strings.ToLower(strings.TrimSpace(msg))
	switch {
	case m == "":
		return "no error detail available"
	case strings.Contains(m, "fake 200"), strings.Contains(m, "unparseable body"), strings.Contains(m, "no choices, no embedding data"):
		return "invalid response (fake 200: upstream returned HTTP 200 with an empty payload)"
	case strings.Contains(m, "context deadline exceeded"), strings.Contains(m, "timeout"),
		strings.Contains(m, "timed out"), strings.Contains(m, "deadline exceeded"):
		return "timeout"
	case strings.Contains(m, "no such host"), strings.Contains(m, "connection refused"),
		strings.Contains(m, "connection reset"), strings.Contains(m, "network is unreachable"),
		strings.Contains(m, "i/o timeout"), strings.Contains(m, "connection canceled"),
		strings.Contains(m, "broken pipe"), strings.Contains(m, "no route to host"),
		strings.Contains(m, "tls:"), strings.Contains(m, "x509"), strings.Contains(m, "certificate"),
		strings.Contains(m, "dial tcp"), strings.Contains(m, "dial udp"), strings.Contains(m, "dial unix"):
		return "network failure"
	case strings.Contains(m, "unsupported channel type"), strings.Contains(m, "channel not found"),
		strings.Contains(m, "channel disabled"), strings.Contains(m, "no available key"):
		return "no available channel"
	}
	// HTTP status：4xx/5xx 数字。只提数字本身，不带 body / URL。
	if idx := strings.LastIndex(m, "status code "); idx >= 0 {
		if code, ok := parseTrailingInt(m[idx+len("status code "):]); ok {
			return "upstream returned HTTP " + strconv.Itoa(code)
		}
	}
	for _, marker := range []string{"http ", "status "} {
		if idx := strings.LastIndex(m, marker); idx >= 0 {
			if code, ok := parseTrailingInt(m[idx+len(marker):]); ok && code >= 400 {
				return "upstream returned HTTP " + strconv.Itoa(code)
			}
		}
	}
	if strings.Contains(m, "unexpected status code") || strings.Contains(m, "status code") {
		return "upstream returned an unexpected HTTP status"
	}
	if strings.Contains(m, "transformresponse") || strings.Contains(m, "transform request") ||
		strings.Contains(m, "invalid character") || strings.Contains(m, "json:") || strings.Contains(m, "parse error") ||
		strings.Contains(m, "embedding data") || strings.Contains(m, "empty data") || strings.Contains(m, "empty payload") ||
		strings.Contains(m, "decode") || strings.Contains(m, "unexpected end of json") {
		return "response parse failure"
	}
	// 未识别 → 完全折叠。外发面宁缺毋滥：原文仍在服务端日志里可查。
	return "internal failure (see server logs for details)"
}

// parseTrailingInt 从 s 开头解析非负整数（后跟非数字即停）。
func parseTrailingInt(s string) (int, bool) {
	s = strings.TrimLeft(s, " :#")
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	v, err := strconv.Atoi(s[:end])
	if err != nil || v <= 0 || v > math.MaxInt32 {
		return 0, false
	}
	return v, true
}

// SanitizeProbeNotifyMessage 把任意错误文本折叠成安全摘要：类别化 + 换行清洗。
// 输出不含输入的任何原文片段（除类别白名单里允许的数字），单行，截断到 200 字符。
// 这是探测通知外发内容的唯一合法入口（WO-031）。
func SanitizeProbeNotifyMessage(msg string) string {
	category := safeProbeNotifyCategory(msg)
	return singleLineTruncated(category, 200)
}

// singleLineTruncated 把文本压成单行（换行/回车全部折叠为空格）并截断。
// 防换行注入：外发摘要不允许输入控制换行伪造后续通知字段。
func singleLineTruncated(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}
