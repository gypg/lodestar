package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const dbDumpVersion = 1
const maxRelayLogsExport = 500_000
const maxAuditLogsExport = 500_000

func ExportAll(ctx context.Context, includeLogs, includeStats bool) (*model.DBDump, error) {
	conn := db.GetDB().WithContext(ctx)

	d := &model.DBDump{
		Version:      dbDumpVersion,
		ExportedAt:   time.Now().UTC(),
		IncludeLogs:  includeLogs,
		IncludeStats: includeStats,
	}

	// Core tables
	if err := conn.Find(&d.Channels).Error; err != nil {
		return nil, fmt.Errorf("export channels: %w", err)
	}
	if err := conn.Find(&d.ChannelKeys).Error; err != nil {
		return nil, fmt.Errorf("export channel_keys: %w", err)
	}
	if err := conn.Find(&d.ChannelGroups).Error; err != nil {
		return nil, fmt.Errorf("export channel_groups: %w", err)
	}
	if err := conn.Find(&d.Groups).Error; err != nil {
		return nil, fmt.Errorf("export groups: %w", err)
	}
	if err := conn.Find(&d.GroupItems).Error; err != nil {
		return nil, fmt.Errorf("export group_items: %w", err)
	}
	if err := conn.Find(&d.LLMInfos).Error; err != nil {
		return nil, fmt.Errorf("export llm_infos: %w", err)
	}
	if err := conn.Find(&d.APIKeys).Error; err != nil {
		return nil, fmt.Errorf("export api_keys: %w", err)
	}
	if err := conn.Find(&d.Users).Error; err != nil {
		return nil, fmt.Errorf("export users: %w", err)
	}
	if err := conn.Find(&d.Settings).Error; err != nil {
		return nil, fmt.Errorf("export settings: %w", err)
	}

	// Alert tables
	if err := conn.Find(&d.AlertRules).Error; err != nil {
		return nil, fmt.Errorf("export alert_rules: %w", err)
	}
	if err := conn.Find(&d.AlertNotifChannels).Error; err != nil {
		return nil, fmt.Errorf("export alert_notif_channels: %w", err)
	}
	if err := conn.Find(&d.AlertStateRecords).Error; err != nil {
		return nil, fmt.Errorf("export alert_state_records: %w", err)
	}
	if err := conn.Find(&d.AlertHistory).Error; err != nil {
		return nil, fmt.Errorf("export alert_history: %w", err)
	}

	// Runtime
	if err := conn.Find(&d.RuntimeStates).Error; err != nil {
		return nil, fmt.Errorf("export runtime_states: %w", err)
	}
	if err := conn.Find(&d.CircuitBreakerStates).Error; err != nil {
		return nil, fmt.Errorf("export circuit_breaker_states: %w", err)
	}

	if includeStats {
		if err := conn.Find(&d.StatsTotal).Error; err != nil {
			return nil, fmt.Errorf("export stats_total: %w", err)
		}
		if err := conn.Find(&d.StatsDaily).Error; err != nil {
			return nil, fmt.Errorf("export stats_daily: %w", err)
		}
		if err := conn.Find(&d.StatsHourly).Error; err != nil {
			return nil, fmt.Errorf("export stats_hourly: %w", err)
		}
		if err := conn.Find(&d.StatsModel).Error; err != nil {
			return nil, fmt.Errorf("export stats_model: %w", err)
		}
		if err := conn.Find(&d.StatsChannel).Error; err != nil {
			return nil, fmt.Errorf("export stats_channel: %w", err)
		}
		if err := conn.Find(&d.StatsAPIKey).Error; err != nil {
			return nil, fmt.Errorf("export stats_api_key: %w", err)
		}
	}

	if includeLogs {
		if err := conn.Order("id DESC").Limit(maxAuditLogsExport).Find(&d.AuditLogs).Error; err != nil {
			return nil, fmt.Errorf("export audit_logs: %w", err)
		}
		// relay_logs 可能位于独立日志库，从日志库连接读取（共用主库时 GetLogDB
		// 返回主库连接，行为不变）。强制导出：无论「保留历史日志」开关是否开启，
		// 都导出日志库的实际内容；若独立日志库此前被 CloseLogDB 断开，先重连再读。
		if db.IsLogDBSeparate() {
			if err := db.ReopenLogDB(); err != nil {
				return nil, fmt.Errorf("reopen log db before export: %w", err)
			}
		}
		if logConn := db.GetLogDB(); logConn != nil {
			if err := logConn.WithContext(ctx).Order("id DESC").Limit(maxRelayLogsExport).Find(&d.RelayLogs).Error; err != nil {
				return nil, fmt.Errorf("export relay_logs: %w", err)
			}
		}
	}

	// Hub tables
	if err := conn.Find(&d.RemoteSites).Error; err != nil {
		return nil, fmt.Errorf("export remote_sites: %w", err)
	}
	if err := conn.Find(&d.BalanceSnapshots).Error; err != nil {
		return nil, fmt.Errorf("export balance_snapshots: %w", err)
	}
	if err := conn.Find(&d.CheckInRecords).Error; err != nil {
		return nil, fmt.Errorf("export check_in_records: %w", err)
	}
	if err := conn.Find(&d.APICredentialProfiles).Error; err != nil {
		return nil, fmt.Errorf("export api_credential_profiles: %w", err)
	}
	if err := conn.Find(&d.SiteAnnouncements).Error; err != nil {
		return nil, fmt.Errorf("export site_announcements: %w", err)
	}
	if err := conn.Find(&d.RemoteSiteTokens).Error; err != nil {
		return nil, fmt.Errorf("export remote_site_tokens: %w", err)
	}

	return d, nil
}

type importConfig struct {
	conn    *gorm.DB
	res     *model.DBImportResult
	version int // dump version, 0 for old backward-compat
	isFull  bool
}

func appendStep(res *model.DBImportResult, table, mode string, rows int64, err error) {
	step := model.DBImportStep{Table: table, Mode: mode, RowsAffected: rows, OK: err == nil}
	if err != nil {
		step.Error = err.Error()
	}
	res.Progress = append(res.Progress, step)
}

func (c *importConfig) doNothing(table string, rows any, count int) error {
	if count == 0 {
		return nil
	}
	result := c.conn.Table(table).Clauses(clause.OnConflict{DoNothing: true}).Create(rows)
	appendStep(c.res, table, "insert", result.RowsAffected, result.Error)
	if result.Error != nil {
		return fmt.Errorf("%s: %w", table, result.Error)
	}
	return nil
}

func (c *importConfig) upsertAll(table string, rows any, count int, conflictColumns []clause.Column) error {
	if count == 0 {
		return nil
	}
	result := c.conn.Table(table).Clauses(clause.OnConflict{
		Columns:   conflictColumns,
		UpdateAll: true,
	}).Create(rows)
	appendStep(c.res, table, "upsert", result.RowsAffected, result.Error)
	if result.Error != nil {
		return fmt.Errorf("%s: %w", table, result.Error)
	}
	return nil
}

func (c *importConfig) upsertSettings(rows []model.Setting) error {
	if len(rows) == 0 {
		return nil
	}
	result := c.conn.Table("settings").Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&rows)
	appendStep(c.res, "settings", "upsert", result.RowsAffected, result.Error)
	if result.Error != nil {
		return fmt.Errorf("settings: %w", result.Error)
	}
	return nil
}

func (c *importConfig) deleteAll(table string) error {
	result := c.conn.Exec(fmt.Sprintf("DELETE FROM %s", table))
	appendStep(c.res, table, "delete", result.RowsAffected, result.Error)
	return result.Error
}

func ImportWithMode(ctx context.Context, dump *model.DBDump, mode string) (*model.DBImportResult, error) {
	return ImportWithModeToDB(ctx, db.GetDB(), dump, mode)
}

func ImportWithModeToDB(ctx context.Context, target *gorm.DB, dump *model.DBDump, mode string) (*model.DBImportResult, error) {
	if dump == nil {
		return nil, fmt.Errorf("empty dump")
	}
	if target == nil {
		return nil, fmt.Errorf("target database is nil")
	}
	isFull := mode == model.ImportModeFull
	res := &model.DBImportResult{RowsAffected: map[string]int64{}}
	cfg := &importConfig{conn: target.WithContext(ctx), res: res, isFull: isFull, version: dump.Version}

	// relay_logs 是否需要路由到独立日志库：仅在「live 导入」（target 即主库）
	// 且配置了独立日志库时成立。此时 relay_logs 落在另一个数据库，无法纳入主库
	// 事务，需在事务外单独处理（日志可丢，跨库非原子可接受）。
	// 迁移路径（target 为另开的库）不走这里，relay_logs 跟随 target 一起迁移，
	// 行为与旧版一致。
	logToSeparateDB := target == db.GetDB() && db.IsLogDBSeparate()

	err := cfg.conn.Transaction(func(tx *gorm.DB) error {
		cfg.conn = tx

		if isFull {
			// Delete in reverse dependency order to avoid FK violations
			deleteOrder := []string{
				"relay_logs", "stats_api_keys", "stats_channels", "stats_models",
				"stats_hourlies", "stats_dailies", "stats_totals",
				"remote_site_tokens", "site_announcements",
				"check_in_records", "balance_snapshots",
				"api_credential_profiles", "remote_sites",
				"group_items", "channel_groups", "groups",
				"alert_histories", "alert_state_records", "alert_rules", "alert_notif_channels",
				"audit_logs", "auto_strategy_states", "circuit_breaker_states",
				"api_keys", "users", "channel_keys", "channels",
				"llm_infos", "settings",
			}
			for i, table := range deleteOrder {
				switch table {
				case "stats_totals":
					deleteOrder[i] = cfg.conn.NamingStrategy.TableName("stats_total")
				case "stats_dailies":
					deleteOrder[i] = cfg.conn.NamingStrategy.TableName("stats_daily")
				case "stats_hourlies":
					deleteOrder[i] = cfg.conn.NamingStrategy.TableName("stats_hourly")
				case "stats_models":
					deleteOrder[i] = cfg.conn.NamingStrategy.TableName("stats_model")
				case "stats_channels":
					deleteOrder[i] = cfg.conn.NamingStrategy.TableName("stats_channel")
				case "stats_api_keys":
					deleteOrder[i] = cfg.conn.NamingStrategy.TableName("stats_api_key")
				case "alert_histories":
					deleteOrder[i] = cfg.conn.NamingStrategy.TableName("alert_history")
				case "auto_strategy_states":
					deleteOrder[i] = "auto_strategy_states"
				}
			}
			for _, table := range deleteOrder {
				// relay_logs 路由到独立日志库时，由事务外的 importRelayLogsToLogDB
				// 负责清空与写入，主事务（主库）跳过它。
				if table == "relay_logs" && logToSeparateDB {
					continue
				}
				if err := cfg.deleteAll(table); err != nil {
					return fmt.Errorf("full import: delete %s: %w", table, err)
				}
			}
		}

		// Import channels / keys / groups / items — skip existing
		if err := cfg.doNothing("channels", &dump.Channels, len(dump.Channels)); err != nil {
			return err
		}
		if err := cfg.doNothing("channel_keys", &dump.ChannelKeys, len(dump.ChannelKeys)); err != nil {
			return err
		}
		if err := cfg.doNothing("channel_groups", &dump.ChannelGroups, len(dump.ChannelGroups)); err != nil {
			return err
		}
		if err := cfg.doNothing("groups", &dump.Groups, len(dump.Groups)); err != nil {
			return err
		}
		if err := cfg.doNothing("group_items", &dump.GroupItems, len(dump.GroupItems)); err != nil {
			return err
		}

		// LLM prices — upsert by name
		if err := cfg.upsertAll("llm_infos", &dump.LLMInfos, len(dump.LLMInfos), []clause.Column{{Name: "name"}}); err != nil {
			return err
		}

		// API keys — skip existing
		if err := cfg.doNothing("api_keys", &dump.APIKeys, len(dump.APIKeys)); err != nil {
			return err
		}

		// Users — skip existing (backward compat: might be nil in old dumps)
		if len(dump.Users) > 0 {
			if err := cfg.doNothing("users", &dump.Users, len(dump.Users)); err != nil {
				return err
			}
		}

		// Settings — upsert by key
		if err := cfg.upsertSettings(dump.Settings); err != nil {
			return err
		}

		// Alerts — skip existing
		if len(dump.AlertRules) > 0 {
			if err := cfg.doNothing("alert_rules", &dump.AlertRules, len(dump.AlertRules)); err != nil {
				return err
			}
		}
		if len(dump.AlertNotifChannels) > 0 {
			if err := cfg.doNothing("alert_notif_channels", &dump.AlertNotifChannels, len(dump.AlertNotifChannels)); err != nil {
				return err
			}
		}
		if len(dump.AlertStateRecords) > 0 {
			if err := cfg.doNothing("alert_state_records", &dump.AlertStateRecords, len(dump.AlertStateRecords)); err != nil {
				return err
			}
		}
		if len(dump.AlertHistory) > 0 {
			if err := cfg.doNothing("alert_histories", &dump.AlertHistory, len(dump.AlertHistory)); err != nil {
				return err
			}
		}

		// Audit & runtime — skip existing
		if len(dump.AuditLogs) > 0 {
			if err := cfg.doNothing("audit_logs", &dump.AuditLogs, len(dump.AuditLogs)); err != nil {
				return err
			}
		}
		if len(dump.RuntimeStates) > 0 {
			if err := cfg.doNothing("auto_strategy_states", &dump.RuntimeStates, len(dump.RuntimeStates)); err != nil {
				return err
			}
		}
		if len(dump.CircuitBreakerStates) > 0 {
			if err := cfg.doNothing("circuit_breaker_states", &dump.CircuitBreakerStates, len(dump.CircuitBreakerStates)); err != nil {
				return err
			}
		}

		// Stats
		if dump.IncludeStats {
			if err := cfg.upsertAll("stats_totals", &dump.StatsTotal, len(dump.StatsTotal), []clause.Column{{Name: "id"}}); err != nil {
				return err
			}
			if err := cfg.upsertAll("stats_dailies", &dump.StatsDaily, len(dump.StatsDaily), []clause.Column{{Name: "date"}}); err != nil {
				return err
			}
			if err := cfg.upsertAll("stats_hourlies", &dump.StatsHourly, len(dump.StatsHourly), []clause.Column{{Name: "hour"}, {Name: "date"}}); err != nil {
				return err
			}
			if err := cfg.upsertAll("stats_models", &dump.StatsModel, len(dump.StatsModel), []clause.Column{{Name: "id"}}); err != nil {
				return err
			}
			if err := cfg.upsertAll("stats_channels", &dump.StatsChannel, len(dump.StatsChannel), []clause.Column{{Name: "channel_id"}}); err != nil {
				return err
			}
			if err := cfg.upsertAll("stats_api_keys", &dump.StatsAPIKey, len(dump.StatsAPIKey), []clause.Column{{Name: "api_key_id"}}); err != nil {
				return err
			}
		}

		// Relay logs
		// 独立日志库 live 模式下，relay_logs 在主事务外单独写入日志库（见下方），
		// 此处跳过；其余情况（共用主库、迁移）仍内联在主事务中，行为不变。
		if dump.IncludeLogs && !logToSeparateDB {
			if err := cfg.doNothing("relay_logs", &dump.RelayLogs, len(dump.RelayLogs)); err != nil {
				return err
			}
		}

		// Hub tables — skip existing
		if len(dump.RemoteSites) > 0 {
			if err := cfg.doNothing("remote_sites", &dump.RemoteSites, len(dump.RemoteSites)); err != nil {
				return err
			}
		}
		if len(dump.BalanceSnapshots) > 0 {
			if err := cfg.doNothing("balance_snapshots", &dump.BalanceSnapshots, len(dump.BalanceSnapshots)); err != nil {
				return err
			}
		}
		if len(dump.CheckInRecords) > 0 {
			if err := cfg.doNothing("check_in_records", &dump.CheckInRecords, len(dump.CheckInRecords)); err != nil {
				return err
			}
		}
		if len(dump.APICredentialProfiles) > 0 {
			if err := cfg.doNothing("api_credential_profiles", &dump.APICredentialProfiles, len(dump.APICredentialProfiles)); err != nil {
				return err
			}
		}
		if len(dump.SiteAnnouncements) > 0 {
			if err := cfg.doNothing("site_announcements", &dump.SiteAnnouncements, len(dump.SiteAnnouncements)); err != nil {
				return err
			}
		}
		if len(dump.RemoteSiteTokens) > 0 {
			if err := cfg.doNothing("remote_site_tokens", &dump.RemoteSiteTokens, len(dump.RemoteSiteTokens)); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// 独立日志库 live 模式：relay_logs 在主事务外单独写入日志库。
	// 跨库非原子——主库数据已提交，日志单独导入；日志可丢，失败仅记录在结果中
	// 不回滚主库。
	//
	// 强制导入：无论「保留历史日志」开关是否开启，都把日志写入日志库。若日志库
	// 此前被 CloseLogDB 断开（用户关闭了后台日志），先 ReopenLogDB 重连——导入
	// 完成后日志库即处于开启（已连接）状态。
	if logToSeparateDB && dump.IncludeLogs {
		if err := db.ReopenLogDB(); err != nil {
			return nil, fmt.Errorf("reopen log db before import: %w", err)
		}
		if logConn := db.GetLogDB(); logConn != nil {
			logCfg := &importConfig{conn: logConn.WithContext(ctx), res: res, isFull: isFull, version: dump.Version}
			if isFull {
				if err := logCfg.deleteAll("relay_logs"); err != nil {
					return nil, fmt.Errorf("full import: delete relay_logs (log db): %w", err)
				}
			}
			if err := logCfg.doNothing("relay_logs", &dump.RelayLogs, len(dump.RelayLogs)); err != nil {
				return nil, err
			}
		}
	}

	// Summarize rows affected from progress
	for _, step := range res.Progress {
		res.RowsAffected[step.Table] += step.RowsAffected
	}
	return res, nil
}

// ImportIncremental is the backward-compatible wrapper.
func ImportIncremental(ctx context.Context, dump *model.DBDump) (*model.DBImportResult, error) {
	return ImportWithMode(ctx, dump, model.ImportModeIncremental)
}
