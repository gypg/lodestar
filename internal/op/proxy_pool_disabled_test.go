package op

import (
	"context"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

// ProxyConfiguration.Enabled 带 gorm default:true 标签，struct Create 落不进
// 显式的 false（create 回调把零值替换成默认值）。这两条测试钉住
// ProxyConfigurationCreate 对显式 false / 显式 true 都必须原样落库。

func loadProxyByID(t *testing.T, ctx context.Context, id int) model.ProxyConfiguration {
	t.Helper()
	var saved model.ProxyConfiguration
	if err := db.GetDB().WithContext(ctx).First(&saved, id).Error; err != nil {
		t.Fatalf("load proxy configuration %d failed: %v", id, err)
	}
	return saved
}

func TestProxyConfigurationCreatePersistsExplicitDisabled(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	item := &model.ProxyConfiguration{
		Name:    "disabled-proxy",
		URL:     "http://disabled.example.com:8080",
		Enabled: false,
		Remark:  "operator disabled at creation",
	}
	if err := ProxyConfigurationCreate(item, ctx); err != nil {
		t.Fatalf("ProxyConfigurationCreate returned error: %v", err)
	}
	// create 回调会把结构体里的 false 回写成 true；缓存与调用方手里的值必须如实。
	if item.Enabled {
		t.Fatalf("ProxyConfigurationCreate left item.Enabled=true; caller asked for false")
	}

	saved := loadProxyByID(t, ctx, item.ID)
	if saved.Enabled {
		t.Fatalf("user asked for enabled=false; stored enabled=true")
	}
	if saved.URL != "http://disabled.example.com:8080" || saved.Remark != "operator disabled at creation" {
		t.Fatalf("expected other columns to be persisted as given, got %+v", saved)
	}
	// 缓存里那份也必须是 false：缓存与 DB 不一致时，"缓存热就不查库"的 List
	// 会把停用配置当启用的用。
	cached, ok := proxyConfigurationCache.Get(item.ID)
	if !ok {
		t.Fatalf("expected the created proxy to be seeded into the cache, got cache miss")
	}
	if cached.Enabled {
		t.Fatalf("cached copy reports enabled=true; DB says enabled=false")
	}
}

func TestProxyConfigurationCreateReturnsEnabledAsGiven(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	// 缺陷 1 的靶子：启用态创建后，调用方手里的结构体被 create 回调篡改成
	// enabled=false，并经 resp.Success(c, item) 直接返回给前端。
	item := &model.ProxyConfiguration{
		Name:    "returns-enabled-proxy",
		URL:     "http://returns.example.com:8080",
		Enabled: true,
	}
	if err := ProxyConfigurationCreate(item, ctx); err != nil {
		t.Fatalf("ProxyConfigurationCreate returned error: %v", err)
	}

	if !item.Enabled {
		t.Fatalf("caller asked for enabled=true; item.Enabled=false after create (response would lie)")
	}

	saved := loadProxyByID(t, ctx, item.ID)
	if !saved.Enabled {
		t.Fatalf("user asked for enabled=true; stored enabled=false")
	}
}

func TestProxyConfigurationCreateSeedsCacheWhenWarm(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	// 先把缓存捂热：缓存冷时 List 会回落查库，测不出"漏 Set 缓存"的缺陷。
	if _, err := ProxyConfigurationList(ctx); err != nil {
		t.Fatalf("warm-up ProxyConfigurationList failed: %v", err)
	}

	cases := []struct {
		name    string
		url     string
		enabled bool
	}{
		{name: "warm-enabled-proxy", url: "http://warm-enabled.example.com:8080", enabled: true},
		{name: "warm-disabled-proxy", url: "http://warm-disabled.example.com:8080", enabled: false},
	}
	for _, tc := range cases {
		item := &model.ProxyConfiguration{
			Name:    tc.name,
			URL:     tc.url,
			Enabled: tc.enabled,
		}
		if err := ProxyConfigurationCreate(item, ctx); err != nil {
			t.Fatalf("ProxyConfigurationCreate(%s) returned error: %v", tc.name, err)
		}

		items, err := ProxyConfigurationList(ctx)
		if err != nil {
			t.Fatalf("ProxyConfigurationList failed: %v", err)
		}
		var found *model.ProxyConfiguration
		for i := range items {
			if items[i].ID == item.ID {
				found = &items[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("created proxy %q (id=%d) missing from a warm-cache list; cache was not seeded", tc.name, item.ID)
		}
		if found.Enabled != tc.enabled {
			t.Fatalf("warm-cache list reports enabled=%v for %q, want %v", found.Enabled, tc.name, tc.enabled)
		}
	}
}
