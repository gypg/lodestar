package helper

import (
	"context"
	"errors"
	"testing"

	"github.com/gypg/lodestar/internal/model"
	ch "github.com/gypg/lodestar/internal/op/channel"
)

// R-10 消费端：渠道代理来源此前完全由 proxy + channel_proxy 决定，proxy_mode 那一列
// 从没被读过。现在 proxy_mode 成为权威字段，但自定义 URL（channel_proxy）优先 ——
// 迁移 018 把"proxy=true 且配了自定义 URL"的行回填成 direct（自定义 URL 不属于
// 代理池），若不让 channel_proxy 继续优先，这些渠道会静默失去代理。

func strPtr(s string) *string { return &s }
func intPtr2(i int) *int      { return &i }

func TestResolveChannelProxyModes(t *testing.T) {
	cases := []struct {
		name          string
		channel       model.Channel
		wantCustomURL string
		wantSystem    bool
	}{
		{
			name:    "directNoProxy",
			channel: model.Channel{ID: 1, ProxyMode: model.ProxyUsageModeDirect},
		},
		{
			name:       "systemMode",
			channel:    model.Channel{ID: 2, ProxyMode: model.ProxyUsageModeSystem},
			wantSystem: true,
		},
		{
			// 自定义 URL 优先于模式。
			name: "customURLBeatsSystemMode",
			channel: model.Channel{
				ID: 3, ProxyMode: model.ProxyUsageModeSystem,
				ChannelProxy: strPtr("http://127.0.0.1:1080"),
			},
			wantCustomURL: "http://127.0.0.1:1080",
		},
		{
			// 空白 channel_proxy 不算配了自定义 URL。
			name: "blankCustomURLIgnored",
			channel: model.Channel{
				ID: 4, ProxyMode: model.ProxyUsageModeSystem,
				ChannelProxy: strPtr("   "),
			},
			wantSystem: true,
		},
		{
			// 老库尚未跑迁移：proxy_mode 空 + proxy=true 仍应走系统代理。
			name:       "emptyModeWithLegacyProxyTrue",
			channel:    model.Channel{ID: 5, ProxyMode: "", Proxy: true},
			wantSystem: true,
		},
		{
			name:    "emptyModeWithoutLegacyProxy",
			channel: model.Channel{ID: 6, ProxyMode: ""},
		},
		{
			// direct 但 proxy=true（迁移前"用系统代理"的语义）也要尊重。
			name:       "directModeWithLegacyProxyTrue",
			channel:    model.Channel{ID: 7, ProxyMode: model.ProxyUsageModeDirect, Proxy: true},
			wantSystem: true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotSystem, err := resolveChannelProxy(context.Background(), &tt.channel)
			if err != nil {
				t.Fatalf("resolveChannelProxy err = %v", err)
			}
			if gotURL != tt.wantCustomURL {
				t.Errorf("customURL = %q, want %q", gotURL, tt.wantCustomURL)
			}
			if gotSystem != tt.wantSystem {
				t.Errorf("useSystem = %v, want %v", gotSystem, tt.wantSystem)
			}
		})
	}
}

// pool 模式必须去代理池取 URL，并把取不到的情况报成错误而不是静默直连 ——
// 静默直连会让本该走代理的流量裸奔。
func TestResolveChannelProxyPoolMode(t *testing.T) {
	prev := ch.ProxyURLForConfig
	t.Cleanup(func() { ch.ProxyURLForConfig = prev })

	var gotID int
	ch.ProxyURLForConfig = func(id int, _ context.Context) (string, error) {
		gotID = id
		return "http://pool-proxy:3128", nil
	}

	channel := model.Channel{ID: 8, ProxyMode: model.ProxyUsageModePool, ProxyConfigID: intPtr2(42)}
	gotURL, gotSystem, err := resolveChannelProxy(context.Background(), &channel)
	if err != nil {
		t.Fatalf("resolveChannelProxy err = %v", err)
	}
	if gotID != 42 {
		t.Errorf("ProxyURLForConfig called with id = %d, want 42", gotID)
	}
	if gotURL != "http://pool-proxy:3128" {
		t.Errorf("customURL = %q, want the pool URL", gotURL)
	}
	if gotSystem {
		t.Error("useSystem = true, want false (pool 模式走自定义 URL)")
	}
}

func TestResolveChannelProxyPoolModeErrors(t *testing.T) {
	prev := ch.ProxyURLForConfig
	t.Cleanup(func() { ch.ProxyURLForConfig = prev })
	ch.ProxyURLForConfig = func(int, context.Context) (string, error) {
		return "", errors.New("proxy configuration is disabled")
	}

	// 缺 config id。
	missing := model.Channel{ID: 9, ProxyMode: model.ProxyUsageModePool}
	if _, _, err := resolveChannelProxy(context.Background(), &missing); err == nil {
		t.Error("pool 模式缺 proxy_config_id 时返回 nil error，want error")
	}

	// id <= 0 同样非法。
	zero := model.Channel{ID: 10, ProxyMode: model.ProxyUsageModePool, ProxyConfigID: intPtr2(0)}
	if _, _, err := resolveChannelProxy(context.Background(), &zero); err == nil {
		t.Error("proxy_config_id=0 时返回 nil error，want error")
	}

	// 代理池报错要透传（禁用/不存在），不能退化成直连。
	disabled := model.Channel{ID: 11, ProxyMode: model.ProxyUsageModePool, ProxyConfigID: intPtr2(7)}
	url, system, err := resolveChannelProxy(context.Background(), &disabled)
	if err == nil {
		t.Fatal("代理池配置被禁用时返回 nil error，want error")
	}
	if url != "" || system {
		t.Errorf("出错时不得回落成可用代理，got url=%q system=%v", url, system)
	}
}

// 未知模式必须报错，不能静默直连。
func TestResolveChannelProxyRejectsUnknownMode(t *testing.T) {
	channel := model.Channel{ID: 12, ProxyMode: "bogus"}
	if _, _, err := resolveChannelProxy(context.Background(), &channel); err == nil {
		t.Error("未知 proxy_mode 返回 nil error，want error")
	}
	// inherit 在渠道上无意义（没有父级），同样按未知处理。
	inherit := model.Channel{ID: 13, ProxyMode: model.ProxyUsageModeInherit}
	if _, _, err := resolveChannelProxy(context.Background(), &inherit); err == nil {
		t.Error("渠道 proxy_mode=inherit 返回 nil error，want error")
	}
}
