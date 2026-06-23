package op

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/utils/log"
)

func ProjectedChannelGlobalAutoGroupMode() model.AutoGroupType {
	value, err := SettingGetString(model.SettingKeyProjectedChannelAutoGroupEnabled)
	if err != nil {
		return model.AutoGroupTypeNone
	}
	mode, ok := model.ParseAutoGroupSettingValue(value)
	if !ok {
		return model.AutoGroupTypeNone
	}
	return mode
}

func ProjectedChannelGlobalAutoGroupEnabled() bool {
	return ProjectedChannelGlobalAutoGroupMode() != model.AutoGroupTypeNone
}

func EffectiveProjectedChannelAutoGroup(channel model.Channel) model.AutoGroupType {
	if mode := ProjectedChannelGlobalAutoGroupMode(); mode != model.AutoGroupTypeNone {
		return mode
	}
	return channel.AutoGroup
}

func ChannelAutoGroupWithMode(channel *model.Channel, autoGroup model.AutoGroupType, ctx context.Context) {
	if channel == nil || autoGroup == model.AutoGroupTypeNone {
		return
	}
	groups, err := GroupList(ctx)
	if err != nil {
		log.Warnf("get group list failed: %v", err)
		return
	}

	channelModelNames := splitChannelModelNames(channel.Model, channel.CustomModel)
	if len(channelModelNames) == 0 {
		return
	}

	for _, group := range groups {
		matchedModelNames := make([]string, 0, len(channelModelNames))

		switch autoGroup {
		case model.AutoGroupTypeExact:
			for _, modelName := range channelModelNames {
				if strings.EqualFold(modelName, group.Name) {
					matchedModelNames = append(matchedModelNames, modelName)
				}
			}
		case model.AutoGroupTypeFuzzy:
			groupNameLower := strings.ToLower(strings.TrimSpace(group.Name))
			if groupNameLower == "" {
				continue
			}
			for _, modelName := range channelModelNames {
				if strings.Contains(strings.ToLower(modelName), groupNameLower) {
					matchedModelNames = append(matchedModelNames, modelName)
				}
			}
		case model.AutoGroupTypeRegex:
			if group.MatchRegex == "" {
				for _, modelName := range channelModelNames {
					if strings.EqualFold(modelName, group.Name) {
						matchedModelNames = append(matchedModelNames, modelName)
					}
				}
				break
			}

			re, err := regexp2.Compile(group.MatchRegex, regexp2.ECMAScript)
			if err != nil {
				log.Warnf("compile regex failed (channel=%d group=%d regex=%q): %v", channel.ID, group.ID, group.MatchRegex, err)
				continue
			}
			re.MatchTimeout = 200 * time.Millisecond
			for _, modelName := range channelModelNames {
				matched, err := re.MatchString(modelName)
				if err != nil {
					log.Warnf("match regex failed (channel=%d group=%d regex=%q model=%q): %v", channel.ID, group.ID, group.MatchRegex, modelName, err)
					continue
				}
				if matched {
					matchedModelNames = append(matchedModelNames, modelName)
				}
			}
		}

		if len(matchedModelNames) == 0 {
			continue
		}
		items := make([]model.GroupIDAndLLMName, 0, len(matchedModelNames))
		for _, modelName := range matchedModelNames {
			items = append(items, model.GroupIDAndLLMName{ChannelID: channel.ID, ModelName: modelName})
		}
		if err := GroupItemBatchAdd(group.ID, items, ctx); err != nil {
			log.Warnf("group item batch add failed (channel=%d group=%d): %v", channel.ID, group.ID, err)
		}
	}
}

func ChannelAutoGroup(channel *model.Channel, ctx context.Context) {
	if channel == nil {
		return
	}
	ChannelAutoGroupWithMode(channel, channel.AutoGroup, ctx)
}

func AutoGroupAllProjectedChannels(ctx context.Context) error {
	mode := ProjectedChannelGlobalAutoGroupMode()
	if mode == model.AutoGroupTypeNone {
		return nil
	}
	channels := channelCache.GetAll()
	if len(channels) == 0 {
		return nil
	}

	// 新逻辑：先清空所有非默认分组，再重新分组
	// 这样每次分组都是全新的，不会被旧分组干扰
	if err := deleteAllNonDefaultGroups(ctx); err != nil {
		log.Warnf("failed to delete non-default groups before auto-group: %v", err)
		// 继续执行，不阻塞
	}

	channelIDs := make([]int, 0, len(channels))
	for id := range channels {
		channelIDs = append(channelIDs, id)
	}
	bindingMap, err := SiteChannelBindingMapByChannelIDs(channelIDs, ctx)
	if err != nil {
		return err
	}
	for id, channel := range channels {
		if _, ok := bindingMap[id]; !ok {
			continue
		}
		ChannelAutoGroupWithMode(&channel, mode, ctx)
	}
	return nil
}

// deleteAllNonDefaultGroups 删除所有非默认分组及其关联的 group_items。
// 用于自动分组前清空旧分组，确保每次分组都是全新的。
func deleteAllNonDefaultGroups(ctx context.Context) error {
	tx := db.GetDB().WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Errorf("panic recovered in deleteAllNonDefaultGroups: %v", r)
		}
	}()

	// 获取所有非默认分组 ID
	var nonDefaultGroupIDs []int
	if err := tx.Model(&model.ChannelGroup{}).
		Where("is_default = ?", false).
		Pluck("id", &nonDefaultGroupIDs).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to query non-default groups: %w", err)
	}

	if len(nonDefaultGroupIDs) == 0 {
		tx.Rollback()
		return nil
	}

	// 删除这些分组的 group_items
	if err := tx.Where("group_id IN ?", nonDefaultGroupIDs).Delete(&model.GroupItem{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete group items: %w", err)
	}

	// 删除非默认分组
	if err := tx.Where("is_default = ?", false).Delete(&model.ChannelGroup{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to delete non-default groups: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// 刷新缓存
	if err := channelGroupRefreshCache(ctx); err != nil {
		log.Warnf("failed to refresh channel group cache: %v", err)
	}

	log.Infof("deleted %d non-default groups for auto-group reset", len(nonDefaultGroupIDs))
	return nil
}

func splitChannelModelNames(values ...string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			result = append(result, name)
		}
	}
	return result
}

func ValidateJSONOverrideObject(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return err
	}
	if _, ok := decoded.(map[string]any); !ok {
		return fmt.Errorf("param_override must be a JSON object")
	}
	return nil
}
