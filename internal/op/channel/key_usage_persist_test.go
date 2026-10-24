package channel_test

import (
	"testing"
	"time"

	dbpkg "github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/channel"
)

// KeyUpdate 的唯一调用方是 relay 的用量/状态回写（成功与失败路径）。
// 它把缓存里的整只 key 标脏，KeySaveDB 用 OnConflict DoUpdates 落库——
// 如果落库列包含 channel_key/enabled/remark，一次普通请求的用量回写就会
// 把并发落库的手工换绑整个覆盖掉（生产事故 2026-10-25：两次换绑被静默
// 还原成旧 key，直到 stop 容器、改库、start 才成功）。
// 用量回写只许碰用量列。
func TestKeySaveDBDoesNotClobberManualKeyEdits(t *testing.T) {
	ctx := setupChannelKeyTestDB(t)
	ch := createTestChannel(t, ctx, "usage-writeback-channel")

	// 建立初始 key（旧值），让缓存装载旧快照。
	updateChannelWithKeyJSON(t, ctx, ch.ID, `{"channel_key":"sk-old","remark":"original"}`)
	oldKey := loadChannelKeyByValue(t, ctx, ch.ID, "sk-old")

	// 模拟运维在运行中的实例外直接换绑（SQL / 另一连接）：
	// DB 已是新值，进程内缓存仍是旧值。
	if err := dbpkg.GetDB().WithContext(ctx).
		Model(&model.ChannelKey{}).
		Where("id = ?", oldKey.ID).
		Updates(map[string]any{
			"channel_key": "sk-rotated-by-ops",
			"enabled":     false,
			"remark":      "rotated",
		}).Error; err != nil {
		t.Fatalf("rotate key in db: %v", err)
	}

	// relay 用量回写：拿缓存里的旧 key 更新用量三列。
	cached := model.ChannelKey{
		ID:               oldKey.ID,
		ChannelID:        ch.ID,
		Enabled:          true,
		ChannelKey:       "sk-old",
		StatusCode:       200,
		LastUseTimeStamp: time.Now().Unix(),
		TotalCost:        0.5,
		Remark:           "original",
	}
	if err := channel.KeyUpdate(cached); err != nil {
		t.Fatalf("KeyUpdate failed: %v", err)
	}
	if err := channel.KeySaveDB(ctx); err != nil {
		t.Fatalf("KeySaveDB failed: %v", err)
	}

	var saved model.ChannelKey
	if err := dbpkg.GetDB().WithContext(ctx).First(&saved, oldKey.ID).Error; err != nil {
		t.Fatalf("reload key: %v", err)
	}
	if saved.ChannelKey != "sk-rotated-by-ops" {
		t.Fatalf("usage writeback clobbered manual key rotation: channel_key=%q, want %q",
			saved.ChannelKey, "sk-rotated-by-ops")
	}
	if saved.Enabled {
		t.Fatal("usage writeback clobbered manual enabled=false")
	}
	if saved.Remark != "rotated" {
		t.Fatalf("usage writeback clobbered manual remark: %q, want %q", saved.Remark, "rotated")
	}
	// 用量三列必须确实落库。
	if saved.StatusCode != 200 || saved.TotalCost != 0.5 || saved.LastUseTimeStamp == 0 {
		t.Fatalf("usage columns not persisted: status=%d cost=%v last_use=%d",
			saved.StatusCode, saved.TotalCost, saved.LastUseTimeStamp)
	}
}
