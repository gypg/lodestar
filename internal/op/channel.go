package op

import (
	"context"
	"strings"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/channel"
	"github.com/gypg/lodestar/internal/op/llm"
	"github.com/gypg/lodestar/internal/op/stats"
	"github.com/gypg/lodestar/internal/utils/log"
)

var channelCache = channel.GetCache()
var channelKeyCache = channel.GetKeyCache()
var channelKeyCacheNeedUpdate, channelKeyCacheNeedUpdateLock = channel.GetKeyCacheNeedUpdate()

// OnChannelDeletedHooks holds optional callbacks invoked after a channel is deleted.
// External packages (e.g. relay/balancer) register cleanup hooks here at init time.
var OnChannelDeletedHooks []func(channelID int)

func init() {
	channel.GroupDefaultID = func(ctx context.Context) (int, error) {
		return ChannelGroupDefaultID(ctx)
	}
	channel.GroupGet = func(id int, ctx context.Context) (*model.ChannelGroup, error) {
		return ChannelGroupGet(id, ctx)
	}
	channel.ProxyURLForConfig = func(id int, ctx context.Context) (string, error) {
		return ProxyURLForConfig(id, ctx)
	}
}

// ChannelKeysForceDisabled 把创建渠道时被 default:true 吞掉显式 false 的
// 级联 Key 改回停用态，并刷新该渠道的缓存。见 channel.KeysForceDisabled。
func ChannelKeysForceDisabled(channelID int, keyIDs []int, ctx context.Context) error {
	return channel.KeysForceDisabled(channelID, keyIDs, ctx)
}

// Deprecated: Use channel.List from internal/op/channel instead.
func ChannelList(ctx context.Context) ([]model.Channel, error) { return channel.List(ctx) }

// Deprecated: Use channel.Create from internal/op/channel instead.
func ChannelCreate(ch *model.Channel, ctx context.Context) error { return channel.Create(ch, ctx) }

// Deprecated: Use channel.KeyUpdate from internal/op/channel instead.
func ChannelKeyUpdate(key model.ChannelKey) error { return channel.KeyUpdate(key) }

// Deprecated: Use channel.BaseUrlUpdate from internal/op/channel instead.
func ChannelBaseUrlUpdate(channelID int, baseUrl []model.BaseUrl) error {
	return channel.BaseUrlUpdate(channelID, baseUrl)
}

// Deprecated: Use channel.KeySaveDB from internal/op/channel instead.
func ChannelKeySaveDB(ctx context.Context) error { return channel.KeySaveDB(ctx) }

// Deprecated: Use channel.Update from internal/op/channel instead.
func ChannelUpdate(req *model.ChannelUpdateRequest, ctx context.Context) (*model.Channel, error) {
	return channel.Update(req, ctx)
}

// Deprecated: Use channel.Enabled from internal/op/channel instead.
func ChannelEnabled(id int, enabled bool, ctx context.Context) error {
	return channel.Enabled(id, enabled, ctx)
}

// ChannelDel handles deletion with cross-package stats/group cache cleanup.
func ChannelDel(id int, ctx context.Context) error {
	ch, err := channel.Get(id, ctx)
	if err != nil {
		return err
	}

	// 记录渠道所属分组，以便删除后检查是否需要清理空分组
	affectedGroupID := ch.GroupID

	// 必须在删除前取：删除后渠道已从缓存和库里消失，拿不到它声明过哪些模型。
	deletedModelNames := splitChannelModelNames(ch.Model, ch.CustomModel)

	if err := channel.Delete(id, ctx); err != nil {
		return err
	}

	stats.OnChannelDeleted(id)

	// Invoke registered cleanup hooks (e.g. balancer circuit breaker / auto stats)
	for _, hook := range OnChannelDeletedHooks {
		hook(id)
	}

	// Refresh affected group caches (in op package, from group.go)
	for _, groupID := range getAffectedGroupIDs(id, ctx) {
		if err := groupRefreshCacheByID(groupID, ctx); err != nil {
			log.Warnf("failed to refresh group cache for group %d: %v", groupID, err)
		}
	}

	// Clean up channel key cache
	for _, k := range ch.Keys {
		if k.ID != 0 {
			channelKeyCache.Del(k.ID)
		}
	}

	// 清理空分组：删除渠道后，若分组内无其他渠道且非默认分组，则自动删除
	if affectedGroupID > 0 {
		if err := cleanupEmptyGroup(affectedGroupID, ctx); err != nil {
			log.Warnf("failed to cleanup empty group %d after channel %d deletion: %v", affectedGroupID, id, err)
		}
	}

	// 回收只由本渠道提供的模型：否则删掉渠道后，其模型仍留在 llm 注册表里，
	// 在模型广场显示为「渠道 0 / Key 0」的空壳，且不会被同步任务回收
	// （回收条件是四个价格列全零，有价格的模型永远留着）。
	if err := reclaimOrphanedModels(deletedModelNames, ctx); err != nil {
		log.Warnf("failed to reclaim models after channel %d deletion: %v", id, err)
	}

	return nil
}

// reclaimOrphanedModels 删除注册表中已无任何渠道提供的模型。
//
// 只考虑被删渠道声明过的名字，所以手动添加、本就没有渠道的模型不受影响 ——
// 它们不在 names 里。反之，若某个名字仍被其他渠道声明，则保留：删一个渠道不该
// 影响另一个渠道还在提供的模型。
//
// ★ 有价格的模型也会被删。这是刻意的取舍：用户要的是「删渠道时对应模型自行删除」，
// 而保留价格就等于保留空壳。删掉渠道再建回来需要重新配价格。
func reclaimOrphanedModels(names []string, ctx context.Context) error {
	if len(names) == 0 {
		return nil
	}

	// 缓存在 channel.Delete 返回前已剔除被删渠道，所以此处遍历到的都是仍存在的渠道。
	stillDeclared := make(map[string]struct{})
	for _, ch := range channelCache.GetAll() {
		for _, name := range splitChannelModelNames(ch.Model, ch.CustomModel) {
			stillDeclared[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
		}
	}

	orphaned := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		lowered := strings.ToLower(strings.TrimSpace(name))
		if lowered == "" {
			continue
		}
		if _, ok := stillDeclared[lowered]; ok {
			continue
		}
		if _, ok := seen[lowered]; ok {
			continue
		}
		seen[lowered] = struct{}{}
		orphaned = append(orphaned, lowered)
	}
	if len(orphaned) == 0 {
		return nil
	}

	if err := llm.BatchDelete(orphaned, ctx); err != nil {
		return err
	}
	ModelMarketInvalidateCache()
	log.Infof("reclaimed %d orphaned model(s) from the registry after channel deletion", len(orphaned))
	return nil
}

func getAffectedGroupIDs(id int, ctx context.Context) []int {
	// This is a minimal implementation; the original logic was in ChannelDel's transaction
	return nil
}

// cleanupEmptyGroup 检查分组是否已空，若空且非默认分组则自动删除。
// 解决"删除渠道后残留空分组"的问题。
func cleanupEmptyGroup(groupID int, ctx context.Context) error {
	// 检查是否为默认分组
	group, err := ChannelGroupGet(groupID, ctx)
	if err != nil {
		return err
	}
	if group.IsDefault {
		return nil // 默认分组不删
	}

	// 检查分组内是否还有渠道
	var count int64
	if err := db.GetDB().WithContext(ctx).
		Model(&model.Channel{}).
		Where("group_id = ?", groupID).
		Count(&count).Error; err != nil {
		return err
	}

	if count == 0 {
		// 分组已空，自动删除
		if err := ChannelGroupDelete(groupID, ctx); err != nil {
			return err
		}
		log.Infof("auto-deleted empty channel group %d (%s)", groupID, group.Name)
	}

	return nil
}

// Deprecated: Use channel.LLMList from internal/op/channel instead.
func ChannelLLMList(ctx context.Context) ([]model.LLMChannel, error) { return channel.LLMList(ctx) }

// Deprecated: Use channel.Get from internal/op/channel instead.
func ChannelGet(id int, ctx context.Context) (*model.Channel, error) { return channel.Get(id, ctx) }

// channelRefreshCache is called by cache.go (same package)
func channelRefreshCache(ctx context.Context) error { return channel.RefreshCache(ctx) }

// channelRefreshCacheByID is called by group.go and ChannelDel (same package)
func channelRefreshCacheByID(id int, ctx context.Context) error {
	return channel.RefreshCacheByID(id, ctx)
}

// ChannelGroup functions are still in channel_group.go (not yet extracted)
