package analytics

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/relaylog"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/op/stats"
	"gorm.io/gorm"
)

// loadAnalyticsSummary aggregates request count and fallback count from DB + in-memory cache.
func loadAnalyticsSummary(ctx context.Context, r model.AnalyticsRange) (*analyticsSummaryRow, error) {
	startUnix := analyticsRangeStartUnix(r, stats.Now())
	row := &analyticsSummaryRow{}

	keepEnabled, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return nil, err
	}

	if keepEnabled {
		query := db.GetDB().WithContext(ctx).
			Model(&model.RelayLog{}).
			Select(`
				COUNT(*) AS request_count,
				COALESCE(SUM(CASE WHEN total_attempts > 1 THEN 1 ELSE 0 END), 0) AS fallback_count
			`)
		if startUnix != nil {
			query = query.Where("time >= ?", *startUnix)
		}
		if err := query.Scan(row).Error; err != nil {
			return nil, err
		}
	}

	cache, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	for _, logItem := range cache {
		if startUnix != nil && logItem.Time < *startUnix {
			continue
		}
		row.RequestCount++
		if logItem.TotalAttempts > 1 {
			row.FallbackCount++
		}
	}
	lock.Unlock()

	return row, nil
}

// loadAnalyticsChannelModelRows aggregates (channel, model) dimension stats.
// Success/failure uses attempt-level (relay_log_attempts) when available.
func loadAnalyticsChannelModelRows(ctx context.Context, r model.AnalyticsRange, scope map[string]struct{}) (map[string]*analyticsChannelModelAggregateRow, error) {
	startUnix := analyticsRangeStartUnix(r, stats.Now())
	rows := make(map[string]*analyticsChannelModelAggregateRow)

	keepEnabled, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return nil, err
	}

	inScope := func(channelID int, modelName string) bool {
		if len(scope) == 0 {
			return true
		}
		_, ok := scope[strconv.Itoa(channelID)+"\x00"+modelName]
		return ok
	}

	if keepEnabled {
		var attemptsConn *gorm.DB
		conn := db.GetLogDB()
		if conn != nil && connHasRelayLogAttempts(conn) {
			attemptsConn = conn
		} else if mainConn := db.GetDB(); mainConn != nil && connHasRelayLogAttempts(mainConn) {
			attemptsConn = mainConn
		}

		if attemptsConn != nil {
			type attRow struct {
				ChannelID    int     `gorm:"column:channel_id"`
				ModelName    string  `gorm:"column:model_name"`
				Success      int64   `gorm:"column:request_success"`
				Failed       int64   `gorm:"column:request_failed"`
				InputTokens  int64   `gorm:"column:input_tokens"`
				OutputTokens int64   `gorm:"column:output_tokens"`
				TotalCost    float64 `gorm:"column:total_cost"`
			}
			var aRows []attRow
			query := attemptsConn.WithContext(ctx).
				Table("relay_log_attempts AS a").
				Select(`
					a.channel_id,
					COALESCE(NULLIF(a.model_name, ''), l.request_model_name) AS model_name,
					COALESCE(SUM(CASE WHEN a.status = ? THEN 1 ELSE 0 END), 0) AS request_success,
					COALESCE(SUM(CASE WHEN a.status = ? THEN 1 ELSE 0 END), 0) AS request_failed,
					COALESCE(SUM(CASE WHEN a.status = ? THEN l.input_tokens ELSE 0 END), 0) AS input_tokens,
					COALESCE(SUM(CASE WHEN a.status = ? THEN l.output_tokens ELSE 0 END), 0) AS output_tokens,
					COALESCE(SUM(CASE WHEN a.status = ? THEN l.cost ELSE 0 END), 0) AS total_cost
				`, string(model.AttemptSuccess), string(model.AttemptFailed),
					string(model.AttemptSuccess), string(model.AttemptSuccess), string(model.AttemptSuccess)).
				Joins("JOIN relay_logs AS l ON l.id = a.relay_log_id").
				Group("a.channel_id, COALESCE(NULLIF(a.model_name, ''), l.request_model_name)")
			if startUnix != nil {
				query = query.Where("a.time >= ?", *startUnix)
			}
			if err := query.Scan(&aRows).Error; err != nil {
				return nil, err
			}
			for _, ar := range aRows {
				if !inScope(ar.ChannelID, ar.ModelName) {
					continue
				}
				key := strconv.Itoa(ar.ChannelID) + "\x00" + ar.ModelName
				rows[key] = &analyticsChannelModelAggregateRow{
					ChannelID: ar.ChannelID,
					ModelName: ar.ModelName,
					analyticsAggregateMetrics: analyticsAggregateMetrics{
						InputTokens:    ar.InputTokens,
						OutputTokens:   ar.OutputTokens,
						TotalCost:      ar.TotalCost,
						RequestSuccess: ar.Success,
						RequestFailed:  ar.Failed,
					},
				}
			}
		} else {
			mainConn := db.GetDB()
			if mainConn != nil {
				var dbRows []analyticsChannelModelAggregateRow
				modelExpr := "COALESCE(NULLIF(actual_model_name, ''), request_model_name)"
				query := mainConn.WithContext(ctx).
					Model(&model.RelayLog{}).
					Select(`
						channel_id,
						channel_name,
						` + modelExpr + ` AS model_name,
						COALESCE(SUM(input_tokens), 0) AS input_tokens,
						COALESCE(SUM(output_tokens), 0) AS output_tokens,
						COALESCE(SUM(cost), 0) AS total_cost,
						COALESCE(SUM(CASE WHEN error = '' THEN 1 ELSE 0 END), 0) AS request_success,
						COALESCE(SUM(CASE WHEN error <> '' THEN 1 ELSE 0 END), 0) AS request_failed
					`).
					Group("channel_id, channel_name, " + modelExpr)
				if startUnix != nil {
					query = query.Where("time >= ?", *startUnix)
				}
				if err := query.Scan(&dbRows).Error; err != nil {
					return nil, err
				}
				for _, row := range dbRows {
					modelName := strings.TrimSpace(row.ModelName)
					if modelName == "" || !inScope(row.ChannelID, modelName) {
						continue
					}
					key := strconv.Itoa(row.ChannelID) + "\x00" + modelName
					rowCopy := row
					rowCopy.ModelName = modelName
					rows[key] = &rowCopy
				}
			}
		}
	}

	cache, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	for _, logItem := range cache {
		if startUnix != nil && logItem.Time < *startUnix {
			continue
		}
		success := logItem.Error == ""
		for _, a := range logItem.Attempts {
			if a.ChannelID == 0 {
				continue
			}
			modelName := strings.TrimSpace(a.ModelName)
			if modelName == "" {
				modelName = strings.TrimSpace(logItem.ActualModelName)
			}
			if modelName == "" {
				modelName = strings.TrimSpace(logItem.RequestModelName)
			}
			if !inScope(a.ChannelID, modelName) {
				continue
			}
			key := strconv.Itoa(a.ChannelID) + "\x00" + modelName
			row, ok := rows[key]
			if !ok {
				row = &analyticsChannelModelAggregateRow{ChannelID: a.ChannelID, ModelName: modelName}
				rows[key] = row
			}
			if a.Status == model.AttemptFailed {
				row.RequestFailed++
				continue
			}
			if a.Status == model.AttemptSuccess {
				row.RequestSuccess++
				if success {
					row.InputTokens += int64(logItem.InputTokens)
					row.OutputTokens += int64(logItem.OutputTokens)
					row.TotalCost += logItem.Cost
				}
			}
			if row.ChannelName == "" {
				row.ChannelName = a.ChannelName
			}
		}
	}
	lock.Unlock()

	return rows, nil
}

func loadAnalyticsProviderRows(ctx context.Context, r model.AnalyticsRange) (map[int]*analyticsProviderAggregateRow, error) {
	startUnix := analyticsRangeStartUnix(r, stats.Now())
	rows := make(map[int]*analyticsProviderAggregateRow)

	keepEnabled, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return nil, err
	}

	if keepEnabled {
		var dbRows []analyticsProviderAggregateRow
		query := db.GetDB().WithContext(ctx).
			Model(&model.RelayLog{}).
			Select(`
				channel_id,
				channel_name,
				COALESCE(SUM(input_tokens), 0) AS input_tokens,
				COALESCE(SUM(output_tokens), 0) AS output_tokens,
				COALESCE(SUM(cost), 0) AS total_cost,
				COALESCE(SUM(CASE WHEN error = '' THEN 1 ELSE 0 END), 0) AS request_success,
				COALESCE(SUM(CASE WHEN error <> '' THEN 1 ELSE 0 END), 0) AS request_failed
			`).
			Group("channel_id, channel_name")
		if startUnix != nil {
			query = query.Where("time >= ?", *startUnix)
		}
		if err := query.Scan(&dbRows).Error; err != nil {
			return nil, err
		}
		for _, row := range dbRows {
			rowCopy := row
			rows[row.ChannelID] = &rowCopy
		}
	}

	cache, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	for _, logItem := range cache {
		if startUnix != nil && logItem.Time < *startUnix {
			continue
		}
		row, ok := rows[logItem.ChannelId]
		if !ok {
			row = &analyticsProviderAggregateRow{
				ChannelID:   logItem.ChannelId,
				ChannelName: logItem.ChannelName,
			}
			rows[logItem.ChannelId] = row
		}
		row.InputTokens += int64(logItem.InputTokens)
		row.OutputTokens += int64(logItem.OutputTokens)
		row.TotalCost += logItem.Cost
		if logItem.Error == "" {
			row.RequestSuccess++
		} else {
			row.RequestFailed++
		}
		if row.ChannelName == "" {
			row.ChannelName = logItem.ChannelName
		}
	}
	lock.Unlock()

	return rows, nil
}

func loadAnalyticsModelRows(ctx context.Context, r model.AnalyticsRange) (map[string]*analyticsModelAggregateRow, error) {
	startUnix := analyticsRangeStartUnix(r, stats.Now())
	rows := make(map[string]*analyticsModelAggregateRow)

	keepEnabled, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return nil, err
	}

	if keepEnabled {
		var dbRows []analyticsModelAggregateRow
		modelExpr := "COALESCE(NULLIF(actual_model_name, ''), request_model_name)"
		query := db.GetDB().WithContext(ctx).
			Model(&model.RelayLog{}).
			Select(`
				` + modelExpr + ` AS model_name,
				COALESCE(SUM(input_tokens), 0) AS input_tokens,
				COALESCE(SUM(output_tokens), 0) AS output_tokens,
				COALESCE(SUM(cost), 0) AS total_cost,
				COALESCE(SUM(CASE WHEN error = '' THEN 1 ELSE 0 END), 0) AS request_success,
				COALESCE(SUM(CASE WHEN error <> '' THEN 1 ELSE 0 END), 0) AS request_failed
			`).
			Group(modelExpr)
		if startUnix != nil {
			query = query.Where("time >= ?", *startUnix)
		}
		if err := query.Scan(&dbRows).Error; err != nil {
			return nil, err
		}
		for _, row := range dbRows {
			modelName := strings.TrimSpace(row.ModelName)
			if modelName == "" {
				continue
			}
			rowCopy := row
			rowCopy.ModelName = modelName
			rows[modelName] = &rowCopy
		}
	}

	cache, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	for _, logItem := range cache {
		if startUnix != nil && logItem.Time < *startUnix {
			continue
		}
		modelName := strings.TrimSpace(logItem.ActualModelName)
		if modelName == "" {
			modelName = strings.TrimSpace(logItem.RequestModelName)
		}
		if modelName == "" {
			continue
		}

		row, ok := rows[modelName]
		if !ok {
			row = &analyticsModelAggregateRow{ModelName: modelName}
			rows[modelName] = row
		}
		row.InputTokens += int64(logItem.InputTokens)
		row.OutputTokens += int64(logItem.OutputTokens)
		row.TotalCost += logItem.Cost
		if logItem.Error == "" {
			row.RequestSuccess++
		} else {
			row.RequestFailed++
		}
	}
	lock.Unlock()

	return rows, nil
}

func loadAnalyticsAPIKeyRows(ctx context.Context, r model.AnalyticsRange) (map[string]*analyticsAPIKeyAggregateRow, error) {
	startUnix := analyticsRangeStartUnix(r, stats.Now())
	rows := make(map[string]*analyticsAPIKeyAggregateRow)

	keepEnabled, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return nil, err
	}

	if keepEnabled {
		var dbRows []analyticsAPIKeyAggregateRow
		query := db.GetDB().WithContext(ctx).
			Model(&model.RelayLog{}).
			Select(`
				request_api_key_id AS api_key_id,
				request_api_key_name AS name,
				COALESCE(SUM(input_tokens), 0) AS input_tokens,
				COALESCE(SUM(output_tokens), 0) AS output_tokens,
				COALESCE(SUM(cost), 0) AS total_cost,
				COALESCE(SUM(CASE WHEN error = '' THEN 1 ELSE 0 END), 0) AS request_success,
				COALESCE(SUM(CASE WHEN error <> '' THEN 1 ELSE 0 END), 0) AS request_failed
			`).
			Group("request_api_key_id, request_api_key_name")
		if startUnix != nil {
			query = query.Where("time >= ?", *startUnix)
		}
		if err := query.Scan(&dbRows).Error; err != nil {
			return nil, err
		}
		for _, row := range dbRows {
			rowCopy := row
			rowCopy.Name = strings.TrimSpace(row.Name)
			rows[makeAnalyticsAPIKeyAggregateKey(row.APIKeyID, rowCopy.Name)] = &rowCopy
		}
	}

	cache, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	for _, logItem := range cache {
		if startUnix != nil && logItem.Time < *startUnix {
			continue
		}
		apiKeyID := logItem.RequestAPIKeyID
		keyName := strings.TrimSpace(logItem.RequestAPIKeyName)
		aggregateKey := makeAnalyticsAPIKeyAggregateKey(apiKeyID, keyName)
		row, ok := rows[aggregateKey]
		if !ok {
			row = &analyticsAPIKeyAggregateRow{
				APIKeyID: apiKeyID,
				Name:     keyName,
			}
			rows[aggregateKey] = row
		}
		row.InputTokens += int64(logItem.InputTokens)
		row.OutputTokens += int64(logItem.OutputTokens)
		row.TotalCost += logItem.Cost
		if logItem.Error == "" {
			row.RequestSuccess++
		} else {
			row.RequestFailed++
		}
		if row.Name == "" {
			row.Name = keyName
		}
	}
	lock.Unlock()

	return rows, nil
}

func makeAnalyticsAPIKeyAggregateKey(apiKeyID int, name string) string {
	if apiKeyID > 0 {
		return "id:" + strconv.Itoa(apiKeyID)
	}
	return "name:" + strings.TrimSpace(name)
}

func loadAnalyticsFailureRows(ctx context.Context, since time.Time) (map[string]*analyticsFailureAggregateRow, error) {
	startUnix := since.Unix()
	rows := make(map[string]*analyticsFailureAggregateRow)

	keepEnabled, err := setting.GetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return nil, err
	}

	if keepEnabled {
		var attemptsConn *gorm.DB
		conn := db.GetLogDB()
		if conn != nil && connHasRelayLogAttempts(conn) {
			attemptsConn = conn
		} else if mainConn := db.GetDB(); mainConn != nil && connHasRelayLogAttempts(mainConn) {
			attemptsConn = mainConn
		}

		if attemptsConn != nil {
			var dbRows []analyticsFailureAggregateRow
			query := attemptsConn.WithContext(ctx).
				Table("relay_log_attempts AS a").
				Select(`
					a.channel_id,
					l.request_model_name,
					a.model_name AS actual_model_name,
					COUNT(*) AS failure_count,
					MAX(a.time) AS last_failure_at
				`).
				Joins("JOIN relay_logs AS l ON l.id = a.relay_log_id").
				Where("a.status = ?", string(model.AttemptFailed)).
				Where("a.time >= ?", startUnix).
				Group("a.channel_id, l.request_model_name, a.model_name")
			if err := query.Scan(&dbRows).Error; err != nil {
				return nil, err
			}
			for _, row := range dbRows {
				key := makeAnalyticsFailureKey(row.ChannelID, row.ActualModelName, row.RequestModelName)
				rowCopy := row
				rows[key] = &rowCopy
			}
		} else {
			conn := db.GetDB()
			if conn != nil {
				var dbRows []analyticsFailureAggregateRow
				query := conn.WithContext(ctx).
					Model(&model.RelayLog{}).
					Select(`
						channel_id,
						request_model_name,
						actual_model_name,
						COUNT(*) AS failure_count,
						MAX(time) AS last_failure_at
					`).
					Where("error <> ''").
					Where("time >= ?", startUnix).
					Group("channel_id, request_model_name, actual_model_name")
				if err := query.Scan(&dbRows).Error; err != nil {
					return nil, err
				}
				for _, row := range dbRows {
					key := makeAnalyticsFailureKey(row.ChannelID, row.ActualModelName, row.RequestModelName)
					rowCopy := row
					rows[key] = &rowCopy
				}
			}
		}
	}

	cache, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	for _, logItem := range cache {
		if logItem.Time < startUnix {
			continue
		}
		if logItem.Error != "" {
			key := makeAnalyticsFailureKey(logItem.ChannelId, logItem.ActualModelName, logItem.RequestModelName)
			row, ok := rows[key]
			if !ok {
				row = &analyticsFailureAggregateRow{
					ChannelID:        logItem.ChannelId,
					RequestModelName: logItem.RequestModelName,
					ActualModelName:  logItem.ActualModelName,
				}
				rows[key] = row
			}
			row.FailureCount++
			if logItem.Time > row.LastFailureAt {
				row.LastFailureAt = logItem.Time
			}
			continue
		}
		for _, a := range logItem.Attempts {
			if a.Status != model.AttemptFailed || a.ChannelID == 0 {
				continue
			}
			key := makeAnalyticsFailureKey(a.ChannelID, a.ModelName, logItem.RequestModelName)
			row, ok := rows[key]
			if !ok {
				row = &analyticsFailureAggregateRow{
					ChannelID:        a.ChannelID,
					RequestModelName: logItem.RequestModelName,
					ActualModelName:  a.ModelName,
				}
				rows[key] = row
			}
			row.FailureCount++
			if logItem.Time > row.LastFailureAt {
				row.LastFailureAt = logItem.Time
			}
		}
	}
	lock.Unlock()

	return rows, nil
}

// connHasRelayLogAttempts reports whether the connection has relay_log_attempts table.
func connHasRelayLogAttempts(conn *gorm.DB) bool {
	if conn == nil || conn.Migrator() == nil {
		return false
	}
	return conn.Migrator().HasTable(&model.RelayLogAttempt{})
}

func makeAnalyticsFailureKey(channelID int, actualModelName, requestModelName string) string {
	actualModelName = strings.TrimSpace(actualModelName)
	if actualModelName == "" {
		actualModelName = strings.TrimSpace(requestModelName)
	}
	return strings.Join([]string{
		strconv.Itoa(channelID),
		actualModelName,
		strings.TrimSpace(requestModelName),
	}, "\x00")
}
