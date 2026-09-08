package relay

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/op/ratelimitstore"
)

// WO-045 阻断 4（octopus #238 收尾）：WO-044 只把桶键小写了，限额查找
// （resolveAPIRateLimit 的 quotas[modelName]）还是客户端原串。大小写轮换时
// 先到的变体 miss 回落 key 级 RPM 建桶，per-model 限额被连累全 key 失效。
// 测试走真实链路：resolveAPIRateLimit 出有效 RPM → CheckRateLimit 消费桶，
// 不是只测 map 查找。
func TestResolveAPIRateLimitCaseInsensitivePerModelQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 逐请求构建 gin 上下文：per-model JSON + key 级 RPM=100。
	quotaJSON, err := json.Marshal(map[string]map[string]int{
		"gpt-4": {"rpm": 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(modelName string) (rpm int, tpm int) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("rate_limit_rpm", 100)
		c.Set("per_model_quota_json", string(quotaJSON))
		return resolveAPIRateLimit(modelName, c)
	}

	// 查找判据：两个大小写变体都必须命中 per-model 的 10。
	if rpm, _ := resolve("GPT-4"); rpm != 10 {
		t.Fatalf("resolveAPIRateLimit(%q).rpm = %d, want 10 (per-model lookup must be case-insensitive)", "GPT-4", rpm)
	}
	if rpm, _ := resolve("gpt-4"); rpm != 10 {
		t.Fatalf("resolveAPIRateLimit(%q).rpm = %d, want 10", "gpt-4", rpm)
	}

	// 桶级判据：两变体共享同一个 10/min 的桶——连打 15 次只允许 ≤10。
	const apiKeyID = 777001
	allowed := 0
	for i := 0; i < 15; i++ {
		model := "GPT-4"
		if i%2 == 1 {
			model = "gpt-4"
		}
		rpm, _ := resolve(model)
		if ok, _, _ := ratelimitstore.CheckRateLimit(apiKeyID, model, rpm, 0, 0); ok {
			allowed++
		}
	}
	if allowed > 10 {
		t.Fatalf("15 rapid requests across case variants allowed %d — effective RPM is the key-level 100, per-model 10 was washed out", allowed)
	}
	if allowed == 0 {
		t.Fatal("sanity: nothing allowed")
	}

	// 隔离：清掉本测试的桶，避免串进别的用例。
	ratelimitstore.RemoveAPIKeyBuckets(apiKeyID)
}

// failureHintKey：429/401 冷却提示键同样按大小写分裂会让已 429 的 key 被换
// 大小写再锤。与 statsKey 对齐（两侧都归一化）。
func TestFailureHintKeyCaseInsensitive(t *testing.T) {
	a := failureHintKey(1, 2, "zen/Qwen3-Max")
	b := failureHintKey(1, 2, "zen/qwen3-max")
	if a != b {
		t.Fatalf("failureHintKey split by case: %q vs %q", a, b)
	}
	if failureHintKey(1, 2, " Qwen3-Max ") != failureHintKey(1, 2, "qwen3-max") {
		t.Fatal("failureHintKey must trim whitespace too")
	}
	// channel/key 维度仍区分。
	if failureHintKey(1, 2, "m") == failureHintKey(2, 2, "m") {
		t.Fatal("channel dimension collapsed")
	}
}
