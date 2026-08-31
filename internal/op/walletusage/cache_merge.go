package walletusage

import (
	"context"
	"sort"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/apikey"
	"github.com/gypg/lodestar/internal/op/relaylog"
	"github.com/gypg/lodestar/internal/op/setting"
)

// loadUserLogsMerged returns the relay logs that belong to a user's API keys
// within [cutoff, now], merging two sources so that low-traffic customers see
// their just-spent money reflected immediately in "by model"/"by day" views:
//
//  1. The in-memory relay log cache (relaylog.GetCacheAndLock) — logs written
//     since the last flush, which the wallet views previously missed because
//     RelayLogAdd only flushes at 200 rows / process exit / the 10-min
//     TaskRelayLogSave tick. This is what total_cost/per_key (read from memory
//     stats) already counted, but per_model/daily_series (read straight from
//     DB) did NOT.
//  2. The relay_logs table — already-flushed history.
//
// Dedup by id (T2's core risk): a log may sit in both the cache and the DB
// only in the narrow window after relayLogFlushToDB has run its Create but
// before the cache prefix is truncated (a conflict replay, or a test that
// seeds both). We exclude cache ids from the DB query, so the same log is
// never counted twice. This is stronger than analytics' append-then-pray
// (which relies on the flush always truncating) — wallet views aggregate by
// cost/tokens, where a double-count silently misstates a customer's bill.
//
// chartAvailable mirrors the prior semantics: when relay_log_keep_enabled is
// off OR the log DB is unavailable, returns ok=false and the caller short-
// circuits to an empty/zero result without touching the DB.
func loadUserLogsMerged(uid uint, cutoff int64, ctx context.Context) (logs []model.RelayLog, chartAvailable bool, err error) {
	enabled, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return nil, false, err
	}
	if !enabled {
		return nil, false, nil
	}
	keys, err := apikey.ListByUser(uid, ctx)
	if err != nil {
		return nil, false, err
	}
	if len(keys) == 0 {
		return []model.RelayLog{}, true, nil
	}
	idSet := make(map[int]struct{}, len(keys))
	for _, k := range keys {
		idSet[k.ID] = struct{}{}
	}

	// 1) Cache snapshot: the not-yet-flushed tail. These are the rows total_cost
	//    already counted but per_model/daily_series could not yet see.
	cache, lock := relaylog.GetCacheAndLock()
	cacheIDs := make([]int64, 0, len(cache))
	cacheHits := make([]model.RelayLog, 0, len(cache))
	lock.Lock()
	for _, l := range cache {
		if l.Time < cutoff {
			continue
		}
		if _, ok := idSet[l.RequestAPIKeyID]; !ok {
			continue
		}
		cacheHits = append(cacheHits, l)
		cacheIDs = append(cacheIDs, l.ID)
	}
	lock.Unlock()

	// 2) DB: already-flushed rows. Exclude any id present in the cache so a log
	//    that lives in both places (flush-race window / test seed) is counted
	//    exactly once from the cache side.
	conn := db.GetLogDB()
	if conn == nil {
		// Log DB disconnected (e.g. log keeping turned off mid-flight). The
		// cache is still authoritative for the very-recent window.
		return cacheHits, true, nil
	}
	var dbLogs []model.RelayLog
	q := conn.WithContext(ctx).
		Model(&model.RelayLog{}).
		Where("request_api_key_id IN ?", keyIDsFromSet(idSet)).
		Where("time >= ?", cutoff)
	if len(cacheIDs) > 0 {
		q = q.Where("id NOT IN ?", cacheIDs)
	}
	if err := q.Find(&dbLogs).Error; err != nil {
		return nil, false, err
	}

	merged := make([]model.RelayLog, 0, len(cacheHits)+len(dbLogs))
	merged = append(merged, cacheHits...)
	merged = append(merged, dbLogs...)
	return merged, true, nil
}

func keyIDsFromSet(idSet map[int]struct{}) []int {
	ids := make([]int, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}
