package op

import (
	"testing"

	"github.com/gypg/lodestar/internal/model"
)

// WO-051 回归：渠道删除必须把指向它的 items 从内存组缓存（groupCache/groupMap）
// 里剔除。此前 getAffectedGroupIDs 是 return nil 的占位实现，ChannelDel 的组缓存
// 刷新是死代码；删除后 relay 每次请求都会从 stale 索引捞出已删渠道当无效候选
// （ch.Get not found → Skip），日志噪音且永不自愈（组缓存无 TTL）。
func TestChannelDelRemovesChannelItemsFromGroupCache(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	keep := seedReclaimChannel(t, ctx, "cache-keep-channel", "keep-model")
	victim := seedReclaimChannel(t, ctx, "cache-victim-channel", "victim-model")

	group := &model.Group{
		Name:         "cache-cleanup-group",
		EndpointType: model.EndpointTypeChat,
		Mode:         model.GroupModeRoundRobin,
		Items: []model.GroupItem{
			{ChannelID: keep.ID, ModelName: "keep-model", Priority: 1, Weight: 1},
			{ChannelID: victim.ID, ModelName: "victim-model", Priority: 2, Weight: 1},
		},
	}
	if err := GroupCreate(group, ctx); err != nil {
		t.Fatalf("create group: %v", err)
	}

	// 前置：缓存里确实能看到两个 items（GroupCreate 落库并刷新缓存）。
	cached, ok := groupCache.Get(group.ID)
	if !ok {
		t.Fatalf("group %d not in cache after creation", group.ID)
	}
	if len(cached.Items) != 2 {
		t.Fatalf("precondition: cached group has %d items, want 2", len(cached.Items))
	}

	if err := ChannelDel(victim.ID, ctx); err != nil {
		t.Fatalf("ChannelDel: %v", err)
	}

	// 删除后：缓存组里只剩 keep 渠道的 item。
	cached, ok = groupCache.Get(group.ID)
	if !ok {
		t.Fatalf("group %d disappeared from cache after channel deletion", group.ID)
	}
	if len(cached.Items) != 1 {
		t.Fatalf("cached group has %d items after deletion, want 1 (victim items must be evicted)", len(cached.Items))
	}
	if cached.Items[0].ChannelID != keep.ID {
		t.Fatalf("remaining item points to channel %d, want %d (keep)", cached.Items[0].ChannelID, keep.ID)
	}

	// name → group 索引（relay 的实际查询路径）也必须已经反映剔除。
	routed, err := GroupGetEnabledMapByEndpoint(model.EndpointTypeChat, "victim-model", ctx)
	if err == nil {
		var stillHasVictim bool
		for _, item := range routed.Items {
			if item.ChannelID == victim.ID {
				stillHasVictim = true
			}
		}
		if stillHasVictim {
			t.Fatal("routed group still contains the deleted channel's item")
		}
	}
}

// WO-051 回归：OnChannelDeletedHooks（balancer 熔断/Auto 统计清理）在 ChannelDel
// 里执行。此前 UI 删除路径走内层 ch.Delete 完全绕过 hooks，全局表只增不减。
func TestChannelDelRunsOnChannelDeletedHooks(t *testing.T) {
	ctx := initChannelGroupTestDB(t)

	ch := seedReclaimChannel(t, ctx, "hook-probe-channel", "hook-model")

	ran := false
	sawID := 0
	hook := func(channelID int) {
		ran = true
		sawID = channelID
	}
	OnChannelDeletedHooks = append(OnChannelDeletedHooks, hook)
	t.Cleanup(func() {
		// 截断本测试追加的钩子，避免污染其他测试。
		OnChannelDeletedHooks = OnChannelDeletedHooks[:len(OnChannelDeletedHooks)-1]
	})

	if err := ChannelDel(ch.ID, ctx); err != nil {
		t.Fatalf("ChannelDel: %v", err)
	}

	if !ran {
		t.Fatal("OnChannelDeletedHooks did not run during ChannelDel")
	}
	if sawID != ch.ID {
		t.Fatalf("hook saw channel %d, want %d", sawID, ch.ID)
	}
}
