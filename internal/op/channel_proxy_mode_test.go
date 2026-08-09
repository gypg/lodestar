package op

import (
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

// R-10: ChannelUpdate 的 selectFields 白名单里原本没有 proxy_mode / proxy_config_id，
// 而 ChannelUpdateRequest 声明了这两个字段 —— 客户端传了会拿到 200，但什么都没存。
// 现在两个字段接入白名单，并由 helper.resolveChannelProxy 真正决定代理来源。

// 基线：更新 proxy_mode 必须真的落库，并同步派生的 proxy 布尔值。
func TestChannelUpdatePersistsProxyMode(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	ch := &model.Channel{Name: "proxy-mode-ch", Type: 0, Enabled: true, Model: "m"}
	if err := ChannelCreate(ch, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	mode := model.ProxyUsageModeSystem
	updated, err := ChannelUpdate(&model.ChannelUpdateRequest{ID: ch.ID, ProxyMode: &mode}, ctx)
	if err != nil {
		t.Fatalf("ChannelUpdate: %v", err)
	}
	if updated.ProxyMode != model.ProxyUsageModeSystem {
		t.Fatalf("returned ProxyMode = %q, want %q", updated.ProxyMode, model.ProxyUsageModeSystem)
	}
	// proxy 是给 resolveChannelProxy 的派生值，必须跟随模式。
	if !updated.Proxy {
		t.Errorf("Proxy = false, want true (system 模式应把派生 proxy 置真)")
	}

	// 落库校验：绕开缓存直接读 DB，确认不是只改了缓存。
	var fromDB model.Channel
	if err := db.GetDB().First(&fromDB, ch.ID).Error; err != nil {
		t.Fatalf("reload channel: %v", err)
	}
	if fromDB.ProxyMode != model.ProxyUsageModeSystem {
		t.Fatalf("DB ProxyMode = %q, want %q —— 白名单没带上 proxy_mode（R-10 回归）", fromDB.ProxyMode, model.ProxyUsageModeSystem)
	}
}

// pool 模式必须带一个存在且启用的代理池配置，否则保存就该被拒。
func TestChannelUpdatePoolModeRequiresValidConfig(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	ch := &model.Channel{Name: "proxy-pool-ch", Type: 0, Enabled: true, Model: "m"}
	if err := ChannelCreate(ch, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	pool := model.ProxyUsageModePool

	// 缺 proxy_config_id。
	if _, err := ChannelUpdate(&model.ChannelUpdateRequest{ID: ch.ID, ProxyMode: &pool}, ctx); err == nil {
		t.Fatal("pool 模式缺 proxy_config_id 时 ChannelUpdate 返回 nil error，want error")
	}

	// 指向一个不存在的配置。
	missing := 999999
	_, err := ChannelUpdate(&model.ChannelUpdateRequest{
		ID: ch.ID, ProxyMode: &pool, ProxyConfigID: &missing, ProxyConfigIDSet: true,
	}, ctx)
	if err == nil {
		t.Fatal("pool 模式指向不存在的配置时返回 nil error，want error")
	}

	// 校验失败不得留下部分写入。
	var fromDB model.Channel
	if err := db.GetDB().First(&fromDB, ch.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if fromDB.ProxyMode == model.ProxyUsageModePool {
		t.Fatalf("DB ProxyMode = pool，但校验本应失败并整体回滚")
	}
}

// pool 模式配了合法的启用配置 → 落库，且 proxy_config_id 一起存。
func TestChannelUpdatePoolModePersistsConfigID(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	cfg := &model.ProxyConfiguration{Name: "p1", URL: "http://127.0.0.1:8888", Enabled: true}
	if err := ProxyConfigurationCreate(cfg, ctx); err != nil {
		t.Fatalf("create proxy config: %v", err)
	}

	ch := &model.Channel{Name: "proxy-pool-ok", Type: 0, Enabled: true, Model: "m"}
	if err := ChannelCreate(ch, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	pool := model.ProxyUsageModePool
	updated, err := ChannelUpdate(&model.ChannelUpdateRequest{
		ID: ch.ID, ProxyMode: &pool, ProxyConfigID: &cfg.ID, ProxyConfigIDSet: true,
	}, ctx)
	if err != nil {
		t.Fatalf("ChannelUpdate: %v", err)
	}
	if updated.ProxyMode != model.ProxyUsageModePool {
		t.Fatalf("ProxyMode = %q, want pool", updated.ProxyMode)
	}
	if updated.ProxyConfigID == nil || *updated.ProxyConfigID != cfg.ID {
		t.Fatalf("ProxyConfigID = %v, want %d", updated.ProxyConfigID, cfg.ID)
	}

	var fromDB model.Channel
	if err := db.GetDB().First(&fromDB, ch.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if fromDB.ProxyConfigID == nil || *fromDB.ProxyConfigID != cfg.ID {
		t.Fatalf("DB ProxyConfigID = %v, want %d —— 白名单没带上 proxy_config_id（R-10 回归）", fromDB.ProxyConfigID, cfg.ID)
	}
}

// 切出 pool 模式必须清掉 proxy_config_id，留个不生效的 ID 只会误导排查。
func TestChannelUpdateLeavingPoolClearsConfigID(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	cfg := &model.ProxyConfiguration{Name: "p2", URL: "http://127.0.0.1:8889", Enabled: true}
	if err := ProxyConfigurationCreate(cfg, ctx); err != nil {
		t.Fatalf("create proxy config: %v", err)
	}

	ch := &model.Channel{Name: "proxy-leave-pool", Type: 0, Enabled: true, Model: "m"}
	if err := ChannelCreate(ch, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	pool := model.ProxyUsageModePool
	if _, err := ChannelUpdate(&model.ChannelUpdateRequest{
		ID: ch.ID, ProxyMode: &pool, ProxyConfigID: &cfg.ID, ProxyConfigIDSet: true,
	}, ctx); err != nil {
		t.Fatalf("set pool: %v", err)
	}

	direct := model.ProxyUsageModeDirect
	updated, err := ChannelUpdate(&model.ChannelUpdateRequest{ID: ch.ID, ProxyMode: &direct}, ctx)
	if err != nil {
		t.Fatalf("switch to direct: %v", err)
	}
	if updated.ProxyConfigID != nil {
		t.Fatalf("ProxyConfigID = %v, want nil after leaving pool", *updated.ProxyConfigID)
	}
	if updated.Proxy {
		t.Errorf("Proxy = true, want false (direct 模式派生 proxy 应为假)")
	}

	var fromDB model.Channel
	if err := db.GetDB().First(&fromDB, ch.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if fromDB.ProxyConfigID != nil {
		t.Fatalf("DB ProxyConfigID = %v, want nil", *fromDB.ProxyConfigID)
	}
}

// 非法模式要被拒；渠道没有父级，故不接受 inherit。
func TestChannelUpdateRejectsInvalidProxyMode(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	ch := &model.Channel{Name: "proxy-invalid", Type: 0, Enabled: true, Model: "m"}
	if err := ChannelCreate(ch, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	for _, mode := range []model.ProxyUsageMode{model.ProxyUsageModeInherit, "bogus"} {
		m := mode
		if _, err := ChannelUpdate(&model.ChannelUpdateRequest{ID: ch.ID, ProxyMode: &m}, ctx); err == nil {
			t.Errorf("ProxyMode=%q accepted, want error", mode)
		}
	}
}

// 不传这两个字段时，既有值不能被动过（patch 语义）。
func TestChannelUpdateWithoutProxyFieldsPreservesThem(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	cfg := &model.ProxyConfiguration{Name: "p3", URL: "http://127.0.0.1:8890", Enabled: true}
	if err := ProxyConfigurationCreate(cfg, ctx); err != nil {
		t.Fatalf("create proxy config: %v", err)
	}

	ch := &model.Channel{Name: "proxy-preserve", Type: 0, Enabled: true, Model: "m"}
	if err := ChannelCreate(ch, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	pool := model.ProxyUsageModePool
	if _, err := ChannelUpdate(&model.ChannelUpdateRequest{
		ID: ch.ID, ProxyMode: &pool, ProxyConfigID: &cfg.ID, ProxyConfigIDSet: true,
	}, ctx); err != nil {
		t.Fatalf("set pool: %v", err)
	}

	// 只改名字。
	newName := "proxy-preserve-renamed"
	updated, err := ChannelUpdate(&model.ChannelUpdateRequest{ID: ch.ID, Name: &newName}, ctx)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if updated.ProxyMode != model.ProxyUsageModePool {
		t.Errorf("ProxyMode = %q, want pool (未传该字段不应改动)", updated.ProxyMode)
	}
	if updated.ProxyConfigID == nil || *updated.ProxyConfigID != cfg.ID {
		t.Errorf("ProxyConfigID = %v, want %d (未传该字段不应改动)", updated.ProxyConfigID, cfg.ID)
	}
}
