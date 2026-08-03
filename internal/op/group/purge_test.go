package group

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/channel"
)

// seedGroupWithItems 直接把分组与分组项写入 DB，并刷新分组缓存，
// 模拟真实运行时 PurgeUnavailableItems 所依赖的 DB + 缓存一致状态。
func seedGroupWithItems(t *testing.T, ctx context.Context, group model.Group) {
	t.Helper()
	if err := db.GetDB().WithContext(ctx).Create(&group).Error; err != nil {
		t.Fatalf("seed group %q: %v", group.Name, err)
	}
	if err := RefreshCacheByID(group.ID, ctx); err != nil {
		t.Fatalf("refresh cache for group %q: %v", group.Name, err)
	}
}

func TestPurgeUnavailableItems(t *testing.T) {
	ctx := context.Background()
	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", testName)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	groupCache.Clear()
	chCache := channel.GetCache()
	chCache.Clear()
	t.Cleanup(func() {
		groupCache.Clear()
		chCache.Clear()
	})

	// 渠道 10：启用，声明 gpt-4o（custom_model 里也声明 gpt-extra）。
	chCache.Set(10, model.Channel{
		ID:          10,
		Name:        "enabled-ch",
		Enabled:     true,
		Model:       "gpt-4o, gpt-4o-mini",
		CustomModel: "gpt-extra",
	})
	// 渠道 20：已禁用。
	chCache.Set(20, model.Channel{
		ID:      20,
		Name:    "disabled-ch",
		Enabled: false,
		Model:   "claude-3",
	})
	// 渠道 30 不放入缓存，模拟已删除渠道。

	// 分组 1 包含四类分组项：
	//  - 有效项（渠道 10 + gpt-4o）应保留
	//  - 模型消失（渠道 10 + ghost-model）应删除
	//  - 渠道禁用（渠道 20 + claude-3）应删除
	//  - 渠道缺失（渠道 30 + whatever）应删除
	seedGroupWithItems(t, ctx, model.Group{
		ID:           1,
		Name:         "mixed-group",
		EndpointType: model.EndpointTypeChat,
		Items: []model.GroupItem{
			{ChannelID: 10, ModelName: "gpt-4o", Priority: 1, Weight: 1},
			{ChannelID: 10, ModelName: "ghost-model", Priority: 1, Weight: 1},
			{ChannelID: 20, ModelName: "claude-3", Priority: 1, Weight: 1},
			{ChannelID: 30, ModelName: "whatever", Priority: 1, Weight: 1},
		},
	})

	// 分组 2 全部有效（custom_model 声明的 gpt-extra 也应识别为可用）。
	seedGroupWithItems(t, ctx, model.Group{
		ID:           2,
		Name:         "healthy-group",
		EndpointType: model.EndpointTypeChat,
		Items: []model.GroupItem{
			{ChannelID: 10, ModelName: "gpt-4o-mini", Priority: 1, Weight: 1},
			{ChannelID: 10, ModelName: "gpt-extra", Priority: 1, Weight: 1},
		},
	})

	result, err := PurgeUnavailableItems(ctx)
	if err != nil {
		t.Fatalf("PurgeUnavailableItems: %v", err)
	}

	if result.DeletedCount != 3 {
		t.Errorf("DeletedCount = %d, want 3", result.DeletedCount)
	}
	if result.ChannelMissing != 1 {
		t.Errorf("ChannelMissing = %d, want 1", result.ChannelMissing)
	}
	if result.ChannelDisabled != 1 {
		t.Errorf("ChannelDisabled = %d, want 1", result.ChannelDisabled)
	}
	if result.ModelMissing != 1 {
		t.Errorf("ModelMissing = %d, want 1", result.ModelMissing)
	}
	if result.AffectedGroups != 1 {
		t.Errorf("AffectedGroups = %d, want 1 (only mixed-group)", result.AffectedGroups)
	}

	// 分组 1 应只剩有效项 gpt-4o。
	g1, ok := groupCache.Get(1)
	if !ok {
		t.Fatal("group 1 missing from cache after purge")
	}
	if len(g1.Items) != 1 {
		t.Fatalf("group 1 items = %d, want 1: %+v", len(g1.Items), g1.Items)
	}
	if g1.Items[0].ModelName != "gpt-4o" {
		t.Errorf("group 1 surviving item = %q, want gpt-4o", g1.Items[0].ModelName)
	}

	// 分组 2 应原封不动。
	g2, ok := groupCache.Get(2)
	if !ok {
		t.Fatal("group 2 missing from cache after purge")
	}
	if len(g2.Items) != 2 {
		t.Errorf("group 2 items = %d, want 2 (untouched)", len(g2.Items))
	}

	// 再次运行应为幂等：没有可删项。
	result2, err := PurgeUnavailableItems(ctx)
	if err != nil {
		t.Fatalf("second PurgeUnavailableItems: %v", err)
	}
	if result2.DeletedCount != 0 {
		t.Errorf("second run DeletedCount = %d, want 0 (idempotent)", result2.DeletedCount)
	}
}
