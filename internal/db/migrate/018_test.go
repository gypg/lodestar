package migrate

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gypg/lodestar/internal/model"
	"gorm.io/gorm"
)

// R-10 迁移：proxy_mode 从"死列"变成权威字段前，必须把既有的
// proxy / channel_proxy 语义回填进去，否则所有既有渠道都会带着建表默认值
// 'direct' 被当成不走代理，线上正在用代理的渠道会**静默失去代理**。
//
// 映射（与 helper.ChannelHttpClient 改动前的分支逐一对应）：
//   proxy=false                      → direct
//   proxy=true 且 channel_proxy 空   → system
//   proxy=true 且 channel_proxy 非空 → direct（自定义 URL 仍由 channel_proxy 承载）

func openProxyModeLegacyDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "proxy-mode-legacy.db")
	legacyDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	// 必须关掉底层 sql.DB，否则 Windows 上 TempDir 清理会因文件仍被占用而报错
	// （与 003_test.go / 016_test.go 同一处理）。
	t.Cleanup(func() {
		if sqlDB, err := legacyDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	// 建表时 proxy_mode 一律是建表默认值 'direct'，模拟升级前的库。
	if err := legacyDB.Exec(`
CREATE TABLE channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    proxy NUMERIC DEFAULT 0,
    channel_proxy TEXT,
    proxy_mode TEXT NOT NULL DEFAULT 'direct',
    proxy_config_id INTEGER
)`).Error; err != nil {
		t.Fatalf("create channels table: %v", err)
	}
	return legacyDB
}

func TestMigrateChannelProxyModeBackfillsLegacySemantics(t *testing.T) {
	legacyDB := openProxyModeLegacyDB(t)

	seed := []struct {
		name         string
		proxy        bool
		channelProxy any
	}{
		{"no-proxy", false, nil},
		{"system-proxy", true, nil},
		{"system-proxy-blank", true, "   "},
		{"custom-proxy", true, "http://127.0.0.1:1080"},
		// proxy=false 但配了自定义 URL：迁移前 !channel.Proxy 直接短路成直连，
		// 所以语义是 direct，不能因为有 URL 就当成走代理。
		{"custom-url-but-proxy-off", false, "http://127.0.0.1:1080"},
	}
	for _, s := range seed {
		if err := legacyDB.Exec(
			`INSERT INTO channels (name, proxy, channel_proxy) VALUES (?, ?, ?)`,
			s.name, s.proxy, s.channelProxy,
		).Error; err != nil {
			t.Fatalf("seed %s: %v", s.name, err)
		}
	}

	if err := migrateChannelProxyMode(legacyDB); err != nil {
		t.Fatalf("migrateChannelProxyMode: %v", err)
	}

	want := map[string]string{
		"no-proxy":                 string(model.ProxyUsageModeDirect),
		"system-proxy":             string(model.ProxyUsageModeSystem),
		"system-proxy-blank":       string(model.ProxyUsageModeSystem),
		"custom-proxy":             string(model.ProxyUsageModeDirect),
		"custom-url-but-proxy-off": string(model.ProxyUsageModeDirect),
	}
	for name, wantMode := range want {
		var got string
		if err := legacyDB.Raw(`SELECT proxy_mode FROM channels WHERE name = ?`, name).Scan(&got).Error; err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if got != wantMode {
			t.Errorf("channel %q proxy_mode = %q, want %q", name, got, wantMode)
		}
	}

	// 自定义 URL 不得被迁移动过 —— 它仍是这些渠道唯一的代理来源。
	var customURL string
	if err := legacyDB.Raw(`SELECT channel_proxy FROM channels WHERE name = 'custom-proxy'`).Scan(&customURL).Error; err != nil {
		t.Fatalf("read custom proxy: %v", err)
	}
	if customURL != "http://127.0.0.1:1080" {
		t.Errorf("channel_proxy = %q, want unchanged", customURL)
	}
}

// 幂等：重跑不得改变结果，且不得覆盖用户后来真正配过的模式。
func TestMigrateChannelProxyModeIsIdempotentAndPreservesUserValues(t *testing.T) {
	legacyDB := openProxyModeLegacyDB(t)

	if err := legacyDB.Exec(
		`INSERT INTO channels (name, proxy, channel_proxy) VALUES ('sys', ?, NULL)`, true,
	).Error; err != nil {
		t.Fatalf("seed sys: %v", err)
	}
	// 用户已显式配成 pool 的行：proxy=true 且无自定义 URL，正好命中回填条件的
	// 前两个 where，靠 "proxy_mode 仍是 direct" 这条守卫才不会被改掉。
	if err := legacyDB.Exec(
		`INSERT INTO channels (name, proxy, channel_proxy, proxy_mode, proxy_config_id)
		 VALUES ('user-pool', ?, NULL, 'pool', 5)`, true,
	).Error; err != nil {
		t.Fatalf("seed user-pool: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := migrateChannelProxyMode(legacyDB); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}

	var sysMode, poolMode string
	var poolCfg *int
	if err := legacyDB.Raw(`SELECT proxy_mode FROM channels WHERE name='sys'`).Scan(&sysMode).Error; err != nil {
		t.Fatalf("read sys: %v", err)
	}
	if err := legacyDB.Raw(`SELECT proxy_mode FROM channels WHERE name='user-pool'`).Scan(&poolMode).Error; err != nil {
		t.Fatalf("read user-pool: %v", err)
	}
	if err := legacyDB.Raw(`SELECT proxy_config_id FROM channels WHERE name='user-pool'`).Scan(&poolCfg).Error; err != nil {
		t.Fatalf("read user-pool cfg: %v", err)
	}

	if sysMode != string(model.ProxyUsageModeSystem) {
		t.Errorf("sys proxy_mode = %q, want system", sysMode)
	}
	if poolMode != string(model.ProxyUsageModePool) {
		t.Errorf("user-pool proxy_mode = %q, want pool (迁移覆盖了用户配置)", poolMode)
	}
	if poolCfg == nil || *poolCfg != 5 {
		t.Errorf("user-pool proxy_config_id = %v, want 5", poolCfg)
	}
}

// 空串/NULL 的 proxy_mode 要被规整成显式 'direct'，让列值始终合法。
func TestMigrateChannelProxyModeNormalizesEmptyValues(t *testing.T) {
	legacyDB := openProxyModeLegacyDB(t)

	if err := legacyDB.Exec(`INSERT INTO channels (name, proxy, proxy_mode) VALUES ('empty', 0, '')`).Error; err != nil {
		t.Fatalf("seed empty: %v", err)
	}

	if err := migrateChannelProxyMode(legacyDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var got string
	if err := legacyDB.Raw(`SELECT proxy_mode FROM channels WHERE name='empty'`).Scan(&got).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != string(model.ProxyUsageModeDirect) {
		t.Errorf("proxy_mode = %q, want direct", got)
	}
}

// 表不存在时静默跳过（全新库先跑 AutoMigrate 的场景），不得报错。
func TestMigrateChannelProxyModeSkipsWhenTableMissing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "empty.db")
	emptyDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := emptyDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := migrateChannelProxyMode(emptyDB); err != nil {
		t.Errorf("migrateChannelProxyMode on empty db = %v, want nil", err)
	}
	if err := migrateChannelProxyMode(nil); err == nil {
		t.Error("migrateChannelProxyMode(nil) = nil, want error")
	}
}
