package channel_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	dbpkg "github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/channel"
	"github.com/gypg/lodestar/internal/transformer/outbound"
)

// ChannelKey.Enabled 带 gorm default:true 标签，渠道编辑加 Key 走的是
// ChannelUpdateRequest 的 struct Create 级联，显式的 enabled=false 会落成 true。
// 三条测试都必须真走 JSON 反序列化（json.Unmarshal 进 model.ChannelUpdateRequest），
// 否则测不到 UnmarshalJSON 的 *Set 标记逻辑：
// 显式 false → 落库 false；显式 true → 落库 true；字段缺失 → 落库 true（默认启用）。

func setupChannelKeyTestDB(t *testing.T) context.Context {
	t.Helper()

	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}

	dbPath := filepath.Join(t.TempDir(), "lodestar-channel-keys-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = dbpkg.Close()
	})

	return context.Background()
}

func createTestChannel(t *testing.T, ctx context.Context, name string) *model.Channel {
	t.Helper()
	ch := &model.Channel{
		Name:    name,
		Type:    outbound.OutboundTypeOpenAIChat,
		Enabled: true,
		Model:   "gpt-4o-mini",
	}
	if err := channel.Create(ch, ctx); err != nil {
		t.Fatalf("channel.Create failed: %v", err)
	}
	return ch
}

func loadChannelKeyByValue(t *testing.T, ctx context.Context, channelID int, keyValue string) model.ChannelKey {
	t.Helper()
	var saved model.ChannelKey
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("channel_id = ? AND channel_key = ?", channelID, keyValue).
		First(&saved).Error; err != nil {
		t.Fatalf("load channel key %q failed: %v", keyValue, err)
	}
	return saved
}

func updateChannelWithKeyJSON(t *testing.T, ctx context.Context, channelID int, keysJSON string) {
	t.Helper()
	body := `{"id":` + itoa(channelID) + `,"keys_to_add":[` + keysJSON + `]}`
	var req model.ChannelUpdateRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal ChannelUpdateRequest failed: %v", err)
	}
	if _, err := channel.Update(&req, ctx); err != nil {
		t.Fatalf("channel.Update failed: %v", err)
	}
}

func itoa(v int) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func TestUpdateAddsExplicitlyDisabledKeyAsDisabled(t *testing.T) {
	ctx := setupChannelKeyTestDB(t)
	ch := createTestChannel(t, ctx, "keys-disabled-channel")

	updateChannelWithKeyJSON(t, ctx, ch.ID, `{"enabled":false,"channel_key":"k-dis","remark":"added disabled"}`)

	saved := loadChannelKeyByValue(t, ctx, ch.ID, "k-dis")
	if saved.Enabled {
		t.Fatalf("user asked for enabled=false; stored enabled=true")
	}
	if saved.Remark != "added disabled" {
		t.Fatalf("expected remark to be persisted, got %q", saved.Remark)
	}
}

func TestUpdateAddsExplicitlyEnabledKeyAsEnabled(t *testing.T) {
	ctx := setupChannelKeyTestDB(t)
	ch := createTestChannel(t, ctx, "keys-enabled-channel")

	updateChannelWithKeyJSON(t, ctx, ch.ID, `{"enabled":true,"channel_key":"k-en"}`)

	saved := loadChannelKeyByValue(t, ctx, ch.ID, "k-en")
	if !saved.Enabled {
		t.Fatalf("user asked for enabled=true; stored enabled=false")
	}
}

func TestUpdateAddsKeyWithoutEnabledFieldAsEnabled(t *testing.T) {
	ctx := setupChannelKeyTestDB(t)
	ch := createTestChannel(t, ctx, "keys-missing-channel")

	// JSON 里没有 "enabled" 键：老客户端的形状，必须保持默认启用。
	updateChannelWithKeyJSON(t, ctx, ch.ID, `{"channel_key":"k-legacy"}`)

	saved := loadChannelKeyByValue(t, ctx, ch.ID, "k-legacy")
	if !saved.Enabled {
		t.Fatalf("expected key without explicit enabled to default to enabled=true, got enabled=false")
	}
}

// 缓存与 DB 必须一致：停用的 Key 不能因为补偿只写了库而继续留在缓存里参与调度。
func TestUpdateAddsExplicitlyDisabledKeyCacheConsistent(t *testing.T) {
	ctx := setupChannelKeyTestDB(t)
	ch := createTestChannel(t, ctx, "keys-cache-channel")

	updateChannelWithKeyJSON(t, ctx, ch.ID, `{"enabled":false,"channel_key":"k-cache"}`)

	cached, err := op.ChannelGet(ch.ID, ctx)
	if err != nil {
		t.Fatalf("ChannelGet failed: %v", err)
	}
	for _, key := range cached.Keys {
		if key.ChannelKey == "k-cache" && key.Enabled {
			t.Fatalf("cache still reports the explicitly disabled key as enabled")
		}
	}
}
