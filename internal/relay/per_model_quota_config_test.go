package relay

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// WO-046：配置侧归一化。运营者在自由文本框写 {"GPT-4":{"rpm":10}} 合法，
// 修复前 quotas 只归一化请求侧 → 配置被静默忽略（rpm 回落 key 级）。
func TestWO046UppercaseConfigKeyResolved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	quotaJSON, _ := json.Marshal(map[string]map[string]int{
		"GPT-4": {"rpm": 10},
	})
	resolve := func(modelName string) int {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("rate_limit_rpm", 100)
		c.Set("per_model_quota_json", string(quotaJSON))
		rpm, _ := resolveAPIRateLimit(modelName, c)
		return rpm
	}
	if got := resolve("gpt-4"); got != 10 {
		t.Fatalf("config \"GPT-4\" + request \"gpt-4\": rpm=%d, want 10", got)
	}
	if got := resolve("GPT-4"); got != 10 {
		t.Fatalf("config \"GPT-4\" + request \"GPT-4\": rpm=%d, want 10", got)
	}
	if got := resolve(" gpt-4 "); got != 10 {
		t.Fatalf("config \"GPT-4\" + request \" gpt-4 \": rpm=%d, want 10 (whitespace must normalize on both sides)", got)
	}
	// 未知模型仍回落 key 级。
	if got := resolve("gpt-4o"); got != 100 {
		t.Fatalf("unknown model: rpm=%d, want 100 (key-level fallback)", got)
	}
}
