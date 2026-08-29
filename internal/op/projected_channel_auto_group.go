package op

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/utils/log"
	"github.com/gypg/lodestar/internal/utils/xregexp"
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

			re, err := xregexp.CompileECMAScript(group.MatchRegex)
			if err != nil {
				log.Warnf("compile regex failed (channel=%d group=%d regex=%q): %v", channel.ID, group.ID, group.MatchRegex, err)
				continue
			}
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

// ProjectedChannelJoinGroups puts a projected channel into groups so the relay
// can actually reach it. Two passes:
//
//  1. ChannelAutoGroupWithMode -- honours the operator's chosen match mode
//     against groups that already exist.
//  2. group.EnsureCanonicalGroupsForChannel -- creates a canonical group for any
//     model the first pass left with no GroupItem.
//
// The second pass is why a fresh install can route site models at all: pass 1
// only ever *matches* existing groups, so on a system whose groups were never
// hand-built there is nothing to match and every projected model stays
// unroutable ("group not found" at relay time).
//
// Non-destructive by construction -- it never deletes a group, unlike the manual
// AutoGroupModels rebuild.
func ProjectedChannelJoinGroups(channel *model.Channel, autoGroup model.AutoGroupType, ctx context.Context) {
	if channel == nil || autoGroup == model.AutoGroupTypeNone {
		return
	}
	ChannelAutoGroupWithMode(channel, autoGroup, ctx)
	if _, err := group.EnsureCanonicalGroupsForChannel(*channel, ctx); err != nil {
		log.Warnf("failed to ensure canonical groups for projected channel %d: %v", channel.ID, err)
	}
}

// AutoGroupAllProjectedChannels retroactively puts every already-projected
// channel into groups. Needed because ProjectedChannelJoinGroups only fires
// during projection: channels projected while the global switch was off stay
// groupless (and therefore unroutable) until re-synced one account at a time.
//
// Returns the number of channels processed and groups created.
//
// Deliberately does NOT delete anything. The previous implementation called
// deleteAllNonDefaultGroups first, which wiped hand-made groups and any group
// belonging to another site -- far too destructive for an operator-triggered
// "fill in the gaps" action.
func AutoGroupAllProjectedChannels(ctx context.Context) (int, int, error) {
	mode := ProjectedChannelGlobalAutoGroupMode()
	if mode == model.AutoGroupTypeNone {
		return 0, 0, fmt.Errorf("projected channel auto group is disabled")
	}
	channels := channelCache.GetAll()
	if len(channels) == 0 {
		return 0, 0, nil
	}

	channelIDs := make([]int, 0, len(channels))
	for id := range channels {
		channelIDs = append(channelIDs, id)
	}
	bindingMap, err := SiteChannelBindingMapByChannelIDs(channelIDs, ctx)
	if err != nil {
		return 0, 0, err
	}

	processed := 0
	createdGroups := 0
	for id, channel := range channels {
		if _, ok := bindingMap[id]; !ok {
			continue
		}
		ChannelAutoGroupWithMode(&channel, mode, ctx)
		created, err := group.EnsureCanonicalGroupsForChannel(channel, ctx)
		if err != nil {
			log.Warnf("failed to ensure canonical groups for projected channel %d: %v", id, err)
			continue
		}
		processed++
		createdGroups += created
	}
	log.Infof("projected channel regroup finished: processed=%d created_groups=%d", processed, createdGroups)
	return processed, createdGroups, nil
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
