package secretmask

import (
	"strings"
	"testing"
)

// 这是本包存在的理由：曾有两份副本对 <=8 字符的密钥**原样返回**。
// 密钥长度由建渠道/建 key 的人决定，所以短密钥会被探测结果与投影渠道列表整条回显。
func TestNeverEchoesTheSecretVerbatim(t *testing.T) {
	// 含 1..12 个字符，覆盖 <=8 边界两侧。
	secrets := []string{
		"a", "ab", "abc", "abcd", "abcde", "abcdef", "abcdefg", "abcdefgh",
		"abcdefghi", "sk-123456789012", "  padded-secret  ",
	}

	for _, name := range []string{"Ellipsis", "Stars"} {
		mask := Ellipsis
		if name == "Stars" {
			mask = Stars
		}
		t.Run(name, func(t *testing.T) {
			for _, secret := range secrets {
				trimmed := strings.TrimSpace(secret)
				got := mask(secret)
				if got == trimmed {
					t.Fatalf("%s(%q) = %q — returned the secret verbatim", name, secret, got)
				}
				if !strings.Contains(got, "*") && !strings.Contains(got, "...") {
					t.Fatalf("%s(%q) = %q — no masking applied at all", name, secret, got)
				}
			}
		})
	}
}

func TestShortSecretsAreFullyMasked(t *testing.T) {
	for _, secret := range []string{"a", "abcd", "abcdefgh"} {
		for name, mask := range map[string]func(string) string{"Ellipsis": Ellipsis, "Stars": Stars} {
			got := mask(secret)
			if got != strings.Repeat("*", len(secret)) {
				t.Fatalf("%s(%q) = %q, want %d stars", name, secret, got, len(secret))
			}
			// 一个字符都不能漏出来。
			for _, r := range secret {
				if strings.ContainsRune(got, r) {
					t.Fatalf("%s(%q) = %q — leaks the character %q", name, secret, got, r)
				}
			}
		}
	}
}

func TestLongSecretsKeepTheirEstablishedShapes(t *testing.T) {
	const secret = "sk-abcdefghijklmnop"

	if got, want := Ellipsis(secret), "sk-a...mnop"; got != want {
		t.Fatalf("Ellipsis(%q) = %q, want %q", secret, got, want)
	}
	// Stars 的星号段长度 = 被省略的中间部分长度，这是既有行为（会暴露长度），
	// 渠道列表依赖它，本包只是把它记录下来，不做改变。
	if got, want := Stars(secret), "sk-a"+strings.Repeat("*", len(secret)-8)+"mnop"; got != want {
		t.Fatalf("Stars(%q) = %q, want %q", secret, got, want)
	}
	if len(Stars(secret)) != len(secret) {
		t.Fatalf("Stars() changed the rendered length; the channel list relies on it matching")
	}
}

func TestEmptyStaysEmpty(t *testing.T) {
	for name, mask := range map[string]func(string) string{"Ellipsis": Ellipsis, "Stars": Stars} {
		if got := mask(""); got != "" {
			t.Fatalf("%s(\"\") = %q, want empty", name, got)
		}
		if got := mask("   "); got != "" {
			t.Fatalf("%s(\"   \") = %q, want empty", name, got)
		}
	}
}
