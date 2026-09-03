package helper

import (
	"strings"
	"testing"
)

// WO-031 T4（helper 单元层）：singleLineTruncated 必须压掉一切换行并截断。
// M5 的钉点：类别白名单是主防线，换行清洗是纵深——若未来有人把原文直通本函数
// （例如新增"透传"类别），这里的行为保证不会把换行注入带进外发 payload。
func TestSingleLineTruncatedFoldsNewlines(t *testing.T) {
	in := "line1\nWebhook: forged-success\r\nline3\rtail"
	got := singleLineTruncated(in, 200)
	if strings.ContainsAny(got, "\n\r") {
		t.Fatalf("singleLineTruncated kept newline chars: %q (injection into notification fields)", got)
	}
	if strings.Contains(got, "forged-success") == false || strings.Contains(got, "line1") == false {
		t.Fatalf("unit function must still carry content (it is the depth layer, not the whitelist), got %q", got)
	}

	// 截断：超长输入按字节截到 maxLen。
	long := strings.Repeat("x", 300)
	if got := singleLineTruncated(long, 200); len(got) != 200 {
		t.Fatalf("singleLineTruncated(long, 200) len = %d, want 200", len(got))
	}
}

// T5/T8 的 helper 侧锚点：类别函数对核心签名产出稳定类别（M4 的另一个杀点）。
func TestSafeProbeNotifyCategoryStable(t *testing.T) {
	cases := map[string]string{
		"fake 200: upstream returned HTTP 200 with an unparseable body": "invalid response",
		`Get "https://x.example": dial tcp: no such host`:               "network failure",
		"unexpected status code 429":                                    "upstream returned HTTP 429",
		"TransformResponse failed: invalid character 'h'":               "response parse failure",
		"channel not found":                                             "no available channel",
		`Get "https://x.example": context deadline exceeded`:            "timeout",
	}
	for msg, want := range cases {
		if got := safeProbeNotifyCategory(msg); !strings.Contains(got, want) {
			t.Fatalf("safeProbeNotifyCategory(%q) = %q, want containing %q", msg, got, want)
		}
	}
	if got := safeProbeNotifyCategory("nothing matches this string"); got != "internal failure (see server logs for details)" {
		t.Fatalf("unrecognized text must fold to internal failure, got %q", got)
	}
}
