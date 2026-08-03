package alert

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

var stateCache sync.Map // int(ruleID) -> model.AlertStateRecord
var stateMu sync.Mutex  // protects read-modify-write in StateSet

var timeNow = func() int64 { return time.Now().UnixMilli() }

// In-memory caches for alert rules and notification channels.
// These are configuration data that rarely change. Caching them avoids
// repeated database queries on every API poll (every 30 s per client).

var (
	rulesCacheMu sync.RWMutex
	rulesCache   []model.AlertRule
	rulesCached  bool

	notifCacheMu sync.RWMutex
	notifCache   []model.AlertNotifChannel
	notifCached  bool
)

func invalidateRulesCache() {
	rulesCacheMu.Lock()
	rulesCached = false
	rulesCache = nil
	rulesCacheMu.Unlock()
}

func invalidateNotifCache() {
	notifCacheMu.Lock()
	notifCached = false
	notifCache = nil
	notifCacheMu.Unlock()
}

func RuleList(ctx context.Context) ([]model.AlertRule, error) {
	rulesCacheMu.RLock()
	if rulesCached {
		cached := rulesCache
		rulesCacheMu.RUnlock()
		// Return a copy so callers cannot mutate the cache.
		out := make([]model.AlertRule, len(cached))
		copy(out, cached)
		return out, nil
	}
	rulesCacheMu.RUnlock()

	rules := make([]model.AlertRule, 0)
	if err := db.GetDB().WithContext(ctx).Find(&rules).Error; err != nil {
		return nil, err
	}

	rulesCacheMu.Lock()
	rulesCache = make([]model.AlertRule, len(rules))
	copy(rulesCache, rules)
	rulesCached = true
	rulesCacheMu.Unlock()

	return rules, nil
}

func RuleCreate(ctx context.Context, rule *model.AlertRule) error {
	if err := db.GetDB().WithContext(ctx).Create(rule).Error; err != nil {
		return err
	}
	invalidateRulesCache()
	return nil
}

func RuleUpdate(ctx context.Context, rule *model.AlertRule) error {
	if rule == nil || rule.ID == 0 {
		return fmt.Errorf("alert rule not found")
	}
	var count int64
	if err := db.GetDB().WithContext(ctx).Model(&model.AlertRule{}).Where("id = ?", rule.ID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("alert rule not found")
	}
	if err := db.GetDB().WithContext(ctx).Save(rule).Error; err != nil {
		return err
	}
	invalidateRulesCache()
	return nil
}

func RuleDelete(ctx context.Context, id int) error {
	res := db.GetDB().WithContext(ctx).Delete(&model.AlertRule{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("alert rule not found")
	}
	invalidateRulesCache()
	return nil
}

func NotifChannelList(ctx context.Context) ([]model.AlertNotifChannel, error) {
	notifCacheMu.RLock()
	if notifCached {
		cached := notifCache
		notifCacheMu.RUnlock()
		out := make([]model.AlertNotifChannel, len(cached))
		copy(out, cached)
		return out, nil
	}
	notifCacheMu.RUnlock()

	channels := make([]model.AlertNotifChannel, 0)
	if err := db.GetDB().WithContext(ctx).Find(&channels).Error; err != nil {
		return nil, err
	}

	notifCacheMu.Lock()
	notifCache = make([]model.AlertNotifChannel, len(channels))
	copy(notifCache, channels)
	notifCached = true
	notifCacheMu.Unlock()

	return channels, nil
}

func NotifChannelCreate(ctx context.Context, ch *model.AlertNotifChannel) error {
	if err := db.GetDB().WithContext(ctx).Create(ch).Error; err != nil {
		return err
	}
	invalidateNotifCache()
	return nil
}

func NotifChannelUpdate(ctx context.Context, ch *model.AlertNotifChannel) error {
	if ch == nil || ch.ID == 0 {
		return fmt.Errorf("alert notification channel not found")
	}
	var count int64
	if err := db.GetDB().WithContext(ctx).Model(&model.AlertNotifChannel{}).Where("id = ?", ch.ID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("alert notification channel not found")
	}
	if err := db.GetDB().WithContext(ctx).Save(ch).Error; err != nil {
		return err
	}
	invalidateNotifCache()
	return nil
}

func NotifChannelDelete(ctx context.Context, id int) error {
	res := db.GetDB().WithContext(ctx).Delete(&model.AlertNotifChannel{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("alert notification channel not found")
	}
	invalidateNotifCache()
	return nil
}

func StateGet(ruleID int) model.AlertStateRecord {
	if v, ok := stateCache.Load(ruleID); ok {
		if record, ok := v.(model.AlertStateRecord); ok {
			return record
		}
	}
	return model.AlertStateRecord{RuleID: ruleID, State: model.AlertStateOK}
}

func StateSet(ruleID int, state model.AlertState) {
	stateMu.Lock()
	defer stateMu.Unlock()

	record := StateGet(ruleID)
	record.State = state
	now := timeNow()
	if state == model.AlertStateFiring {
		record.LastFiredAt = now
		record.FiredCount++
	} else if state == model.AlertStateResolved {
		record.LastResolvedAt = now
	}
	record.LastCheckedAt = now
	stateCache.Store(ruleID, record)
}

func HistoryList(ctx context.Context, limit int) ([]model.AlertHistory, error) {
	if limit <= 0 {
		limit = 100
	}
	var history []model.AlertHistory
	if err := db.GetDB().WithContext(ctx).Order("time DESC").Limit(limit).Find(&history).Error; err != nil {
		return nil, err
	}
	return history, nil
}

func HistoryAdd(ctx context.Context, entry *model.AlertHistory) error {
	return db.GetDB().WithContext(ctx).Create(entry).Error
}
