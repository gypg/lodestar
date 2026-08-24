package helper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appmodel "github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/transformer/outbound"
)

func seedRatelimitCooldownSetting(t *testing.T, value string) {
	t.Helper()

	cache := setting.GetCache()
	prev, had := cache.Get(appmodel.SettingKeyRatelimitCooldown)
	cache.Set(appmodel.SettingKeyRatelimitCooldown, value)
	t.Cleanup(func() {
		if had {
			cache.Set(appmodel.SettingKeyRatelimitCooldown, prev)
		}
	})
}

// TestFetchOpenAIModelsHonorsRatelimitCooldownSetting 钉死 ratelimit_cooldown 这个
// 旋钮对模型拉取真的生效。
//
// 冷却判据在 Channel.GetChannelKeyExcludingWithCooldownForModel 里，秒数只能由调用方
// 传入（internal/op/setting 依赖 internal/model，model 反向读设置会成环）。fetch.go
// 曾用写死 300 的 Channel.GetChannelKey()，于是把 ratelimit_cooldown 调成 0（关闭）
// 在这条路径上完全无效，刚吃过 429 的 key 仍被跳过 300 秒。
//
// 这条测试从**真实调用点**观测 —— 看 fetchOpenAIModels 实际发出的 Authorization 头里
// 是哪把 key。刻意不直接调 ratelimitCooldownSeconds()：只测那个读取函数会绕过调用点，
// 而"调用点忘了传设置"正是历史缺陷本身，绕过去就等于没守。
//
// 两个分支都断言，是为了证明这条测试真的在区分行为：只断言 cooldown=0 的话，一个
// 恒选最便宜 key 的实现也能过。
func TestFetchOpenAIModelsHonorsRatelimitCooldownSetting(t *testing.T) {
	// cooledKey 刚吃过 429 且成本最低；freshKey 没冷却但更贵。
	// 选取逻辑取成本最低者，所以"冷却是否生效"唯一决定选中谁。
	const cooledKey = "sk-cooled"
	const freshKey = "sk-fresh"

	tests := []struct {
		name     string
		cooldown string
		wantKey  string
	}{
		{name: "cooldown off keeps the cooled-down cheapest key", cooldown: "0", wantKey: cooledKey},
		{name: "cooldown on skips the cooled-down key", cooldown: "300", wantKey: freshKey},
		// 兜底分支：设置读不出来时必须退回 300（保守，保持冷却），不能退回 0
		// —— 退回 0 等于让一次配置写坏静默关掉整个冷却保护。
		{name: "unparseable setting falls back to the default cooldown", cooldown: "not-a-number", wantKey: freshKey},
		{name: "negative setting falls back to the default cooldown", cooldown: "-1", wantKey: freshKey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seedRatelimitCooldownSetting(t, tt.cooldown)

			gotAuth := ""
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"m1"}]}`))
			}))
			defer upstream.Close()

			channel := appmodel.Channel{
				ID:       1,
				Name:     "ratelimit-cooldown-fixture",
				Type:     outbound.OutboundTypeOpenAIChat,
				Enabled:  true,
				BaseUrls: []appmodel.BaseUrl{{URL: upstream.URL}},
				Keys: []appmodel.ChannelKey{
					{
						ID:               1,
						Enabled:          true,
						ChannelKey:       cooledKey,
						StatusCode:       http.StatusTooManyRequests,
						LastUseTimeStamp: time.Now().Unix(),
						TotalCost:        0,
					},
					{
						ID:         2,
						Enabled:    true,
						ChannelKey: freshKey,
						TotalCost:  100,
					},
				},
			}

			models, err := fetchOpenAIModels(upstream.Client(), context.Background(), channel)
			if err != nil {
				t.Fatalf("fetchOpenAIModels() error = %v", err)
			}
			if len(models) != 1 || models[0] != "m1" {
				t.Fatalf("models = %v, want [m1]", models)
			}
			if want := "Bearer " + tt.wantKey; gotAuth != want {
				t.Fatalf("Authorization = %q, want %q (ratelimit_cooldown=%s)", gotAuth, want, tt.cooldown)
			}
		})
	}
}
