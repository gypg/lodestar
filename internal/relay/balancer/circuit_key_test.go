package balancer

import "testing"

// WO-044 #238：熔断键的 model 段必须大小写无关。zen/ 路由把客户端原始
// 大小写透传，键不归一化时 Zen/FOO 与 zen/foo 是两个熔断器，轮换大小写
// 即可绕过熔断计数与冷却。
func TestCircuitKeyCaseInsensitiveModel(t *testing.T) {
	a := circuitKey(1, 2, "zen/Foo")
	b := circuitKey(1, 2, "zen/FOO")
	c := circuitKey(1, 2, "zen/foo")
	if a != b || a != c {
		t.Fatalf("case variants produced different breaker keys: %q %q %q", a, b, c)
	}
	// channel/key 维度仍区分。
	if circuitKey(1, 2, "m") == circuitKey(2, 2, "m") || circuitKey(1, 2, "m") == circuitKey(1, 3, "m") {
		t.Fatal("channel/key dimensions collapsed")
	}
}
