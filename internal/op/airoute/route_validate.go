package airoute

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/utils/log"
)

// ---------- Validation & normalization ----------

func selectAIRouteForGroup(group model.Group, routes []model.AIRouteEntry) (model.AIRouteEntry, error) {
	for _, route := range routes {
		if strings.EqualFold(strings.TrimSpace(route.RequestedModel), strings.TrimSpace(group.Name)) {
			return route, nil
		}
	}
	return model.AIRouteEntry{}, fmt.Errorf("AI 返回结果未包含目标分组对应路由")
}

func validateAIRouteItems(route model.AIRouteEntry, inputModelSet map[int]map[string]struct{}) ([]model.GroupItem, error) {
	if strings.TrimSpace(route.RequestedModel) == "" {
		return nil, fmt.Errorf("AI返回结果缺少 requested_model")
	}
	if len(route.Items) == 0 {
		return nil, fmt.Errorf("AI返回结果为空")
	}

	seen := make(map[string]struct{})
	groupItems := make([]model.GroupItem, 0, len(route.Items))
	nextPriority := 1

	for _, item := range route.Items {
		if item.ChannelID <= 0 {
			return nil, fmt.Errorf("AI返回了不存在的channel_id: %d", item.ChannelID)
		}

		channelModels, ok := inputModelSet[item.ChannelID]
		if !ok {
			return nil, fmt.Errorf("AI返回了不存在的channel_id: %d", item.ChannelID)
		}

		upstreamModel := strings.TrimSpace(item.UpstreamModel)
		if upstreamModel == "" {
			return nil, fmt.Errorf("AI返回结果缺少 upstream_model")
		}
		if _, ok := channelModels[strings.ToLower(upstreamModel)]; !ok {
			return nil, fmt.Errorf("AI返回了不存在的upstream_model: channel_id=%d model=%q", item.ChannelID, upstreamModel)
		}

		key := fmt.Sprintf("%d\x00%s", item.ChannelID, strings.ToLower(upstreamModel))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		priority := item.Priority
		if priority <= 0 {
			priority = nextPriority
		}
		weight := item.Weight
		if weight <= 0 {
			weight = 100
		}

		groupItems = append(groupItems, model.GroupItem{
			ChannelID: item.ChannelID,
			ModelName: upstreamModel,
			Priority:  priority,
			Weight:    weight,
		})
		nextPriority++
	}

	if len(groupItems) == 0 {
		return nil, fmt.Errorf("AI返回结果为空")
	}

	sort.SliceStable(groupItems, func(i, j int) bool {
		if groupItems[i].Priority != groupItems[j].Priority {
			return groupItems[i].Priority < groupItems[j].Priority
		}
		if groupItems[i].ChannelID != groupItems[j].ChannelID {
			return groupItems[i].ChannelID < groupItems[j].ChannelID
		}
		return groupItems[i].ModelName < groupItems[j].ModelName
	})

	for i := range groupItems {
		groupItems[i].Priority = i + 1
	}

	return groupItems, nil
}

func normalizeAIRouteEntries(routes []model.AIRouteEntry) []model.AIRouteEntry {
	merged := make(map[string]*model.AIRouteEntry, len(routes))
	order := make([]string, 0, len(routes))

	for _, route := range routes {
		requestedModel := normalizeAIRouteRequestedModel(route)
		if requestedModel == "" {
			continue
		}
		endpointType := normalizeAIRouteGroupEndpointType(route.EndpointType)

		key := endpointType + "\x00" + strings.ToLower(requestedModel)
		entry, ok := merged[key]
		if !ok {
			entry = &model.AIRouteEntry{
				EndpointType:   endpointType,
				RequestedModel: requestedModel,
				Items:          make([]model.AIRouteItemSpec, 0, len(route.Items)),
			}
			merged[key] = entry
			order = append(order, key)
		}

		for _, item := range route.Items {
			upstreamModel := strings.TrimSpace(item.UpstreamModel)
			if item.ChannelID <= 0 || upstreamModel == "" {
				continue
			}
			entry.Items = append(entry.Items, model.AIRouteItemSpec{
				ChannelID:     item.ChannelID,
				UpstreamModel: upstreamModel,
				Priority:      item.Priority,
				Weight:        item.Weight,
			})
		}
	}

	result := make([]model.AIRouteEntry, 0, len(order))
	for _, key := range order {
		entry := merged[key]
		if entry == nil {
			continue
		}
		entry.Items = dedupeAIRouteItems(entry.Items)
		if len(entry.Items) == 0 {
			continue
		}
		result = append(result, *entry)
	}

	return result
}

func normalizeAIRouteRequestedModel(route model.AIRouteEntry) string {
	requestedModel := strings.TrimSpace(route.RequestedModel)
	if requestedModel == "" {
		return ""
	}

	if !aiRouteItemsContainFreeTier(route.Items) {
		return requestedModel
	}

	if aiRouteHasTierSuffix(requestedModel) {
		return requestedModel
	}

	return requestedModel + "-free"
}

func aiRouteItemsContainFreeTier(items []model.AIRouteItemSpec) bool {
	for _, item := range items {
		if aiRouteLooksLikeFreeTierModel(item.UpstreamModel) {
			return true
		}
	}
	return false
}

func aiRouteLooksLikeFreeTierModel(modelName string) bool {
	name := strings.ToLower(strings.TrimSpace(modelName))
	if name == "" {
		return false
	}

	return strings.Contains(name, "free") || strings.Contains(name, "公益")
}

func aiRouteHasTierSuffix(requestedModel string) bool {
	name := strings.ToLower(strings.TrimSpace(requestedModel))
	return strings.HasSuffix(name, "-free") || strings.HasSuffix(name, "-公益")
}

func dedupeAIRouteItems(items []model.AIRouteItemSpec) []model.AIRouteItemSpec {
	seen := make(map[string]struct{}, len(items))
	result := make([]model.AIRouteItemSpec, 0, len(items))

	for _, item := range items {
		upstreamModel := strings.TrimSpace(item.UpstreamModel)
		if item.ChannelID <= 0 || upstreamModel == "" {
			continue
		}

		key := fmt.Sprintf("%d\x00%s", item.ChannelID, strings.ToLower(upstreamModel))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		item.UpstreamModel = upstreamModel
		result = append(result, item)
	}

	return result
}

func validateAIRouteTableRoutes(routes []model.AIRouteEntry) error {
	seen := make(map[string]struct{}, len(routes))

	for _, route := range routes {
		requestedModel := strings.TrimSpace(route.RequestedModel)
		if requestedModel == "" {
			continue
		}

		nameKey := strings.ToLower(requestedModel)
		if _, ok := seen[nameKey]; ok {
			return fmt.Errorf("AI 自动修正后仍存在重复路由名: %q", requestedModel)
		}
		seen[nameKey] = struct{}{}
	}

	return nil
}

// ---------- Table route correction ----------

func autoCorrectAIRouteTableRoutes(
	routes []model.AIRouteEntry,
	existingGroups []model.Group,
) ([]model.AIRouteEntry, []aiRouteTableRouteCorrection) {
	if len(routes) == 0 {
		return nil, nil
	}

	corrected := make([]model.AIRouteEntry, len(routes))
	copy(corrected, routes)

	indexesByName := make(map[string][]int, len(corrected))
	orderedConflictNames := make([]string, 0)
	conflictNameSeen := make(map[string]struct{})
	conflictingNames := make(map[string]struct{})
	for i, route := range corrected {
		requestedModel := strings.TrimSpace(route.RequestedModel)
		if requestedModel == "" {
			continue
		}
		corrected[i].RequestedModel = requestedModel
		corrected[i].EndpointType = normalizeAIRouteGroupEndpointType(route.EndpointType)

		nameKey := strings.ToLower(requestedModel)
		indexesByName[nameKey] = append(indexesByName[nameKey], i)
	}
	for nameKey, indexes := range indexesByName {
		endpointTypes := make(map[string]struct{}, len(indexes))
		for _, idx := range indexes {
			endpointTypes[corrected[idx].EndpointType] = struct{}{}
		}
		if len(indexes) > 1 && len(endpointTypes) > 1 {
			conflictingNames[nameKey] = struct{}{}
		}
	}
	for _, route := range corrected {
		nameKey := strings.ToLower(route.RequestedModel)
		if _, ok := conflictingNames[nameKey]; !ok {
			continue
		}
		if _, ok := conflictNameSeen[nameKey]; ok {
			continue
		}
		conflictNameSeen[nameKey] = struct{}{}
		orderedConflictNames = append(orderedConflictNames, nameKey)
	}

	usedNames := make(map[string]struct{}, len(existingGroups)+len(corrected))
	existingByName := make(map[string]model.Group, len(existingGroups))
	for _, group := range existingGroups {
		nameKey := strings.ToLower(strings.TrimSpace(group.Name))
		if nameKey == "" {
			continue
		}
		usedNames[nameKey] = struct{}{}
		existingByName[nameKey] = group
	}
	for _, route := range corrected {
		nameKey := strings.ToLower(route.RequestedModel)
		if nameKey == "" {
			continue
		}
		if _, ok := conflictingNames[nameKey]; ok {
			continue
		}
		usedNames[nameKey] = struct{}{}
	}

	corrections := make([]aiRouteTableRouteCorrection, 0)
	for _, nameKey := range orderedConflictNames {
		indexes := indexesByName[nameKey]
		keepIdx := selectAIRouteTablePrimaryRoute(corrected, indexes, existingByName[nameKey])
		for _, idx := range indexes {
			if idx == keepIdx {
				usedNames[strings.ToLower(corrected[idx].RequestedModel)] = struct{}{}
				continue
			}

			originalName := corrected[idx].RequestedModel
			candidate := buildAIRouteScopedRouteName(originalName, corrected[idx].EndpointType)
			candidate = ensureUniqueAIRouteRouteName(candidate, usedNames)

			corrected[idx].RequestedModel = candidate
			if strings.TrimSpace(corrected[idx].MatchRegex) == "" {
				corrected[idx].MatchRegex = buildAIRouteExactMatchRegex(originalName)
			}
			usedNames[strings.ToLower(candidate)] = struct{}{}
			corrections = append(corrections, aiRouteTableRouteCorrection{
				OriginalName:  originalName,
				EndpointType:  corrected[idx].EndpointType,
				CorrectedName: candidate,
			})
		}
	}

	return corrected, corrections
}

func selectAIRouteTablePrimaryRoute(
	routes []model.AIRouteEntry,
	indexes []int,
	existing model.Group,
) int {
	if len(indexes) == 0 {
		return -1
	}

	if strings.TrimSpace(existing.Name) != "" {
		current := model.NormalizeEndpointType(existing.EndpointType)
		if current == "" {
			current = model.EndpointTypeAll
		}
		switch current {
		case model.EndpointTypeAll:
			for _, idx := range indexes {
				if normalizeAIRouteGroupEndpointType(routes[idx].EndpointType) == model.EndpointTypeAll {
					return idx
				}
			}
			return -1
		case model.EndpointTypeChat, model.EndpointTypeMimo, model.EndpointTypeResponses, model.EndpointTypeMessages:
			for _, idx := range indexes {
				if normalizeAIRouteGroupEndpointType(routes[idx].EndpointType) == model.EndpointTypeAll {
					return idx
				}
			}
			return -1
		default:
			for _, idx := range indexes {
				if normalizeAIRouteGroupEndpointType(routes[idx].EndpointType) == current {
					return idx
				}
			}
			return -1
		}
	}

	for _, idx := range indexes {
		if normalizeAIRouteGroupEndpointType(routes[idx].EndpointType) == model.EndpointTypeAll {
			return idx
		}
	}

	return indexes[0]
}

func buildAIRouteScopedRouteName(baseName string, endpointType string) string {
	baseName = strings.TrimSpace(baseName)
	suffix := airouteScopedRouteNameSuffix(endpointType)
	if baseName == "" {
		return suffix
	}
	return fmt.Sprintf("%s (%s)", baseName, suffix)
}

func airouteScopedRouteNameSuffix(endpointType string) string {
	switch normalizeAIRouteGroupEndpointType(endpointType) {
	case model.EndpointTypeAll, model.EndpointTypeChat, model.EndpointTypeResponses, model.EndpointTypeMessages:
		return "chat"
	default:
		return strings.ReplaceAll(normalizeAIRouteGroupEndpointType(endpointType), "_", "-")
	}
}

func ensureUniqueAIRouteRouteName(candidate string, usedNames map[string]struct{}) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		candidate = "unnamed-route"
	}

	if _, exists := usedNames[strings.ToLower(candidate)]; !exists {
		return candidate
	}

	base := candidate
	for i := 2; ; i++ {
		next := fmt.Sprintf("%s %d", base, i)
		if _, exists := usedNames[strings.ToLower(next)]; !exists {
			return next
		}
	}
}

func buildAIRouteExactMatchRegex(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return "(?i)^" + regexp.QuoteMeta(name) + "$"
}

// ---------- Group sync & creation ----------

func syncGroupItemsWithAIRoute(ctx context.Context, groupID int, routeEndpointType string, items []model.GroupItem) (int, error) {
	g, err := group.GroupGet(groupID, ctx)
	if err != nil {
		return 0, fmt.Errorf("目标分组不存在")
	}
	g, err = ensureAIRouteGroupEndpointType(ctx, g, routeEndpointType)
	if err != nil {
		return 0, err
	}

	existingByKey := make(map[string]model.GroupItem, len(g.Items))
	for _, item := range g.Items {
		key := fmt.Sprintf("%d\x00%s", item.ChannelID, strings.ToLower(strings.TrimSpace(item.ModelName)))
		if _, ok := existingByKey[key]; !ok {
			existingByKey[key] = item
		}
	}

	desiredKeys := make(map[string]struct{}, len(items))
	itemsToUpdate := make([]model.GroupItemUpdateRequest, 0, len(items))
	itemsToAdd := make([]model.GroupItemAddRequest, 0, len(items))
	for idx, item := range items {
		modelName := strings.TrimSpace(item.ModelName)
		if item.ChannelID <= 0 || modelName == "" {
			continue
		}

		priority := idx + 1
		weight := item.Weight
		if weight <= 0 {
			weight = 100
		}

		key := fmt.Sprintf("%d\x00%s", item.ChannelID, strings.ToLower(modelName))
		desiredKeys[key] = struct{}{}
		if existing, ok := existingByKey[key]; ok {
			if existing.Priority != priority || existing.Weight != weight {
				itemsToUpdate = append(itemsToUpdate, model.GroupItemUpdateRequest{
					ID:       existing.ID,
					Priority: priority,
					Weight:   weight,
				})
			}
			continue
		}

		itemsToAdd = append(itemsToAdd, model.GroupItemAddRequest{
			ChannelID: item.ChannelID,
			ModelName: modelName,
			Priority:  priority,
			Weight:    weight,
		})
	}

	itemsToDelete := make([]int, 0)
	for _, item := range g.Items {
		key := fmt.Sprintf("%d\x00%s", item.ChannelID, strings.ToLower(strings.TrimSpace(item.ModelName)))
		if _, ok := desiredKeys[key]; ok {
			continue
		}
		if item.ID > 0 {
			itemsToDelete = append(itemsToDelete, item.ID)
		}
	}

	if len(itemsToAdd) == 0 && len(itemsToUpdate) == 0 && len(itemsToDelete) == 0 {
		return 0, nil
	}

	if _, err := group.GroupUpdate(&model.GroupUpdateRequest{
		ID:            groupID,
		ItemsToAdd:    itemsToAdd,
		ItemsToUpdate: itemsToUpdate,
		ItemsToDelete: itemsToDelete,
	}, ctx); err != nil {
		return 0, fmt.Errorf("写入路由表失败")
	}
	return len(itemsToAdd) + len(itemsToUpdate), nil
}

func createAIRouteGroup(ctx context.Context, groupName string, endpointType string, matchRegex string, items []model.GroupItem) (*model.Group, int, error) {
	groupName = strings.TrimSpace(groupName)
	if groupName == "" {
		return nil, 0, fmt.Errorf("AI返回结果缺少 requested_model")
	}

	tx := db.GetDB().WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Errorf("panic recovered in createAIRouteGroup transaction: %v", r)
		}
	}()

	newGroup := model.Group{
		Name:              groupName,
		EndpointType:      normalizeAIRouteGroupEndpointType(endpointType),
		Mode:              model.GroupModeRoundRobin,
		MatchRegex:        strings.TrimSpace(matchRegex),
		FirstTokenTimeOut: 0,
		SessionKeepTime:   0,
	}
	if err := tx.Create(&newGroup).Error; err != nil {
		tx.Rollback()
		return nil, 0, fmt.Errorf("创建分组失败: %w", err)
	}

	groupItems := make([]model.GroupItem, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	priority := 1
	for _, item := range items {
		modelName := strings.TrimSpace(item.ModelName)
		if item.ChannelID <= 0 || modelName == "" {
			continue
		}

		key := fmt.Sprintf("%d\x00%s", item.ChannelID, strings.ToLower(modelName))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		weight := item.Weight
		if weight <= 0 {
			weight = 100
		}

		groupItems = append(groupItems, model.GroupItem{
			GroupID:   newGroup.ID,
			ChannelID: item.ChannelID,
			ModelName: modelName,
			Priority:  priority,
			Weight:    weight,
		})
		priority++
	}

	if len(groupItems) == 0 {
		tx.Rollback()
		return nil, 0, fmt.Errorf("AI返回结果为空")
	}

	if err := tx.Create(&groupItems).Error; err != nil {
		tx.Rollback()
		return nil, 0, fmt.Errorf("创建分组项失败: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, 0, fmt.Errorf("提交AI路由分组失败: %w", err)
	}

	if err := group.RefreshCacheByID(newGroup.ID, ctx); err != nil {
		return nil, 0, err
	}

	createdGroup, err := group.GroupGet(newGroup.ID, ctx)
	if err != nil {
		return nil, 0, err
	}
	return createdGroup, len(groupItems), nil
}

func ensureAIRouteGroupEndpointType(ctx context.Context, g *model.Group, routeEndpointType string) (*model.Group, error) {
	if g == nil {
		return nil, fmt.Errorf("目标分组不存在")
	}

	current := model.NormalizeEndpointType(g.EndpointType)
	target := normalizeAIRouteGroupEndpointType(routeEndpointType)
	if current == "" {
		current = model.EndpointTypeAll
	}

	if target == model.EndpointTypeAll {
		switch current {
		case model.EndpointTypeAll, model.EndpointTypeChat, model.EndpointTypeMimo, model.EndpointTypeResponses, model.EndpointTypeMessages:
			return g, nil
		default:
			return nil, fmt.Errorf("分组 %q 的 API 分类为 %s，与 AI 路由结果 %s 冲突", g.Name, current, target)
		}
	}

	if current == target {
		return g, nil
	}
	if current == model.EndpointTypeAll {
		updated, err := group.GroupUpdate(&model.GroupUpdateRequest{
			ID:           g.ID,
			EndpointType: &target,
		}, ctx)
		if err != nil {
			return nil, fmt.Errorf("更新分组 API 分类失败: %w", err)
		}
		return updated, nil
	}

	return nil, fmt.Errorf("分组 %q 的 API 分类为 %s，与 AI 路由结果 %s 冲突", g.Name, current, target)
}
