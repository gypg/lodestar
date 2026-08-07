package backup

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	internaldb "github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/transformer/outbound"
)

func loadBackupSource(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	src, err := os.ReadFile(filepath.Clean(filepath.Join(filepath.Dir(file), "backup.go")))
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	return string(src)
}

func TestFullImportDeleteOrderUsesChannelGroupsTable(t *testing.T) {
	text := loadBackupSource(t)
	if strings.Contains(text, `"group_items", "group_channel_items", "groups"`) {
		t.Fatal("delete order still references legacy group_channel_items table")
	}
	if !strings.Contains(text, `"group_items", "channel_groups", "groups"`) {
		t.Fatal("delete order does not include channel_groups between group_items and groups")
	}
}

func TestBackupIncludesCircuitBreakerStates(t *testing.T) {
	text := loadBackupSource(t)
	if !strings.Contains(text, `Find(&d.CircuitBreakerStates)`) {
		t.Fatal("ExportAll does not export circuit_breaker_states")
	}
	if !strings.Contains(text, `"audit_logs", "auto_strategy_states", "circuit_breaker_states"`) {
		t.Fatal("full import delete order does not clear runtime or circuit_breaker_states")
	}
	if !strings.Contains(text, `doNothing("circuit_breaker_states", &dump.CircuitBreakerStates, len(dump.CircuitBreakerStates))`) {
		t.Fatal("ImportWithMode does not restore circuit_breaker_states")
	}
}

func TestBackupIncludesHubTables(t *testing.T) {
	text := loadBackupSource(t)
	for _, table := range []string{
		"RemoteSites", "BalanceSnapshots", "CheckInRecords",
		"APICredentialProfiles", "SiteAnnouncements", "RemoteSiteTokens",
	} {
		if !strings.Contains(text, "Find(&d."+table+")") {
			t.Fatalf("ExportAll does not export %s", table)
		}
	}
	for _, table := range []string{
		"remote_sites", "balance_snapshots", "check_in_records",
		"api_credential_profiles", "site_announcements", "remote_site_tokens",
	} {
		if !strings.Contains(text, `"remote_site_tokens", "site_announcements"`) &&
			!strings.Contains(text, table) {
			t.Fatalf("full import delete order does not include %s", table)
		}
	}
}

func TestImportWithModeFullClearsExistingRowsUsingActualTableNames(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "backup.db")
	if err := internaldb.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		_ = internaldb.Close()
	})

	dbConn := internaldb.GetDB()
	legacyChannel := model.Channel{ID: 1, Name: "legacy-channel", Type: outbound.OutboundTypeOpenAIChat, BaseUrls: []model.BaseUrl{{URL: "https://legacy.example.com"}}}
	legacyGroup := model.Group{ID: 1, Name: "legacy-group", Mode: model.GroupModeRoundRobin, EndpointType: model.EndpointTypeChat}
	legacyAlert := model.AlertHistory{ID: 1, RuleID: 1, RuleName: "legacy", Message: "legacy", Time: 1}
	legacyRuntime := model.AutoStrategyState{Key: "legacy", ChannelID: 1, ModelName: "gpt-4o", UpdatedAt: 1}
	legacyStats := model.StatsTotal{ID: 1}

	for _, row := range []any{&legacyChannel, &legacyGroup, &legacyAlert, &legacyRuntime, &legacyStats} {
		if err := dbConn.Create(row).Error; err != nil {
			t.Fatalf("seed legacy row: %v", err)
		}
	}

	dump := &model.DBDump{
		Version:       1,
		Channels:      []model.Channel{{ID: 2, Name: "new-channel", Type: outbound.OutboundTypeOpenAIChat, BaseUrls: []model.BaseUrl{{URL: "https://new.example.com"}}}},
		Groups:        []model.Group{{ID: 2, Name: "new-group", Mode: model.GroupModeRandom, EndpointType: model.EndpointTypeChat}},
		AlertHistory:  []model.AlertHistory{{ID: 2, RuleID: 2, RuleName: "new", Message: "new", Time: 2}},
		RuntimeStates: []model.AutoStrategyState{{Key: "new", ChannelID: 2, ModelName: "gpt-4.1", UpdatedAt: 2}},
		IncludeStats:  true,
		StatsTotal:    []model.StatsTotal{{ID: 2}},
		RemoteSites:   []model.RemoteSite{{ID: 2, Name: "new-site", BaseURL: "https://new.example.com", SiteType: model.SiteTypeNewAPI, AuthType: model.AuthTypeAccessToken}},
	}

	if _, err := ImportWithMode(context.Background(), dump, model.ImportModeFull); err != nil {
		t.Fatalf("full import: %v", err)
	}

	assertCount := func(modelValue any, expected int64, where string, args ...any) {
		t.Helper()
		var count int64
		query := dbConn.Model(modelValue)
		if where != "" {
			query = query.Where(where, args...)
		}
		if err := query.Count(&count).Error; err != nil {
			t.Fatalf("count %T: %v", modelValue, err)
		}
		if count != expected {
			t.Fatalf("count %T = %d, want %d", modelValue, count, expected)
		}
	}

	assertCount(&model.Channel{}, 0, "id = ?", 1)
	assertCount(&model.Channel{}, 1, "id = ?", 2)
	assertCount(&model.Group{}, 0, "id = ?", 1)
	assertCount(&model.Group{}, 1, "id = ?", 2)
	assertCount(&model.AlertHistory{}, 0, "id = ?", 1)
	assertCount(&model.AlertHistory{}, 1, "id = ?", 2)
	assertCount(&model.AutoStrategyState{}, 0, "key = ?", "legacy")
	assertCount(&model.AutoStrategyState{}, 1, "key = ?", "new")
	assertCount(&model.StatsTotal{}, 0, "id = ?", 1)
	assertCount(&model.StatsTotal{}, 1, "id = ?", 2)
	assertCount(&model.RemoteSite{}, 1, "id = ?", 2)
}

func TestExportImportSeparateLogDBRoundTrip(t *testing.T) {
	mainPath := filepath.Join(t.TempDir(), "main.db")
	if err := internaldb.InitDB("sqlite", mainPath, false); err != nil {
		t.Fatalf("init main db: %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "logs.db")
	if err := internaldb.InitLogDB("sqlite", logPath, false); err != nil {
		t.Fatalf("init log db: %v", err)
	}
	t.Cleanup(func() { _ = internaldb.Close() })

	// Seed relay logs into the separate log DB (not the main DB).
	logConn := internaldb.GetLogDB()
	seed := []model.RelayLog{
		{ID: 1, Time: 1, RequestModelName: "m1"},
		{ID: 2, Time: 2, RequestModelName: "m2"},
	}
	if err := logConn.Create(&seed).Error; err != nil {
		t.Fatalf("seed log db: %v", err)
	}

	// Export must read relay_logs from the log DB.
	dump, err := ExportAll(context.Background(), true, false)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(dump.RelayLogs) != 2 {
		t.Fatalf("exported relay logs = %d, want 2 (must read from log DB)", len(dump.RelayLogs))
	}

	// Clear the log DB, then full-import: logs must be force-written back to log DB.
	if err := logConn.Where("1 = 1").Delete(&model.RelayLog{}).Error; err != nil {
		t.Fatalf("clear log db: %v", err)
	}
	if _, err := ImportWithMode(context.Background(), dump, model.ImportModeFull); err != nil {
		t.Fatalf("full import: %v", err)
	}

	var logCount int64
	if err := internaldb.GetLogDB().Model(&model.RelayLog{}).Count(&logCount).Error; err != nil {
		t.Fatalf("count log db after import: %v", err)
	}
	if logCount != 2 {
		t.Fatalf("log DB relay log count after import = %d, want 2", logCount)
	}

	// Logs must NOT have leaked into the main DB.
	var mainCount int64
	if err := internaldb.GetDB().Model(&model.RelayLog{}).Count(&mainCount).Error; err != nil {
		t.Fatalf("count main db: %v", err)
	}
	if mainCount != 0 {
		t.Fatalf("main DB relay log count = %d, want 0 (logs must stay in log DB)", mainCount)
	}
}

// seedAdminUser creates a user with a real bcrypt hash and returns the plaintext.
func seedAdminUser(t *testing.T, id uint, username string) string {
	t.Helper()
	const plaintext = "correct-horse-battery-staple"
	u := model.User{ID: id, Username: username, Password: plaintext, Role: model.UserRoleAdmin}
	if err := u.HashPassword(); err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := internaldb.GetDB().Create(&u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return plaintext
}

func countUsers(t *testing.T, where string, args ...any) int64 {
	t.Helper()
	var count int64
	query := internaldb.GetDB().Model(&model.User{})
	if where != "" {
		query = query.Where(where, args...)
	}
	if err := query.Count(&count).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	return count
}

func initTestDB(t *testing.T) {
	t.Helper()
	if err := internaldb.InitDB("sqlite", filepath.Join(t.TempDir(), "backup.db"), false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = internaldb.Close() })
}

// S-1: 官方 JSON 导出把 dump.Users 置为 nil（handlers/setting.go），若 full 导入
// 仍按 deleteOrder 删 users，就是「先删光、再无物可插」→ 管理员永久锁死。
func TestFullImportKeepsExistingUsersWhenDumpHasNone(t *testing.T) {
	initTestDB(t)
	plaintext := seedAdminUser(t, 1, "admin")

	dump := &model.DBDump{
		Version:  1,
		Channels: []model.Channel{{ID: 2, Name: "new-channel", Type: outbound.OutboundTypeOpenAIChat, BaseUrls: []model.BaseUrl{{URL: "https://new.example.com"}}}},
		// Users 有意为空：与官方导出一致。
	}

	res, err := ImportWithMode(context.Background(), dump, model.ImportModeFull)
	if err != nil {
		t.Fatalf("full import: %v", err)
	}

	if got := countUsers(t, ""); got != 1 {
		t.Fatalf("user count after full import = %d, want 1 (existing admin must survive)", got)
	}
	admin, err := loadUser(t, 1)
	if err != nil {
		t.Fatalf("load admin: %v", err)
	}
	if admin.Username != "admin" {
		t.Fatalf("admin username = %q, want %q", admin.Username, "admin")
	}
	if err := admin.ComparePassword(plaintext); err != nil {
		t.Fatalf("admin can no longer log in after full import: %v", err)
	}

	// 非 users 表仍然被 full 模式清空并替换。
	var channelCount int64
	if err := internaldb.GetDB().Model(&model.Channel{}).Where("id = ?", 2).Count(&channelCount).Error; err != nil {
		t.Fatalf("count channels: %v", err)
	}
	if channelCount != 1 {
		t.Fatalf("imported channel count = %d, want 1", channelCount)
	}

	// 不应上报 users 的 delete 步骤——跳过删除，而不是删了再补。
	for _, step := range res.Progress {
		if step.Table == "users" && step.Mode == "delete" {
			t.Fatalf("full import reported a users delete step (rows=%d) although the dump carries no users", step.RowsAffected)
		}
	}
	if rows, ok := res.RowsAffected["users"]; ok && rows != 0 {
		t.Fatalf("rows_affected[users] = %d, want 0 or absent", rows)
	}
}

// S-1 变体：WebDAV 备份（op/backup/scheduler.go）不像 handlers/setting.go 那样把
// Users 置 nil，行会保留但 Password 因 json:"-" 变成空串。插入这种账户既无法登录，
// 又让 BootstrapCreate 因「已有用户」拒绝运行 → 比锁死更糟，不可恢复。
func TestFullImportRejectsUsersWithoutPasswordHash(t *testing.T) {
	initTestDB(t)
	plaintext := seedAdminUser(t, 1, "admin")

	dump := &model.DBDump{
		Version: 1,
		Users: []model.User{
			{ID: 2, Username: "webdav-admin", Password: "", Role: model.UserRoleAdmin},
			{ID: 3, Username: "webdav-user", Password: "", Role: model.UserRoleUser},
		},
	}

	if _, err := ImportWithMode(context.Background(), dump, model.ImportModeFull); err != nil {
		t.Fatalf("full import: %v", err)
	}

	if got := countUsers(t, ""); got != 1 {
		t.Fatalf("user count = %d, want 1 (credential-less dump users must not be inserted)", got)
	}
	if got := countUsers(t, "password = ?", ""); got != 0 {
		t.Fatalf("users with empty password hash = %d, want 0", got)
	}
	admin, err := loadUser(t, 1)
	if err != nil {
		t.Fatalf("load admin: %v", err)
	}
	if err := admin.ComparePassword(plaintext); err != nil {
		t.Fatalf("admin can no longer log in after full import: %v", err)
	}
}

// 反向锁：dump 真的带回可登录账户时，full 模式必须照旧替换 users，
// 否则「跳过删除」会退化成「永远不恢复用户」。
func TestFullImportReplacesUsersWhenDumpCarriesHashes(t *testing.T) {
	initTestDB(t)
	seedAdminUser(t, 1, "stale-admin")

	const restoredPlaintext = "restored-admin-password"
	restored := model.User{ID: 2, Username: "restored-admin", Password: restoredPlaintext, Role: model.UserRoleAdmin}
	if err := restored.HashPassword(); err != nil {
		t.Fatalf("hash password: %v", err)
	}

	dump := &model.DBDump{Version: 1, Users: []model.User{restored}}

	if _, err := ImportWithMode(context.Background(), dump, model.ImportModeFull); err != nil {
		t.Fatalf("full import: %v", err)
	}

	if got := countUsers(t, "id = ?", 1); got != 0 {
		t.Fatalf("stale user count = %d, want 0 (dump has users, so users must be replaced)", got)
	}
	if got := countUsers(t, ""); got != 1 {
		t.Fatalf("user count = %d, want 1", got)
	}
	loaded, err := loadUser(t, 2)
	if err != nil {
		t.Fatalf("load restored user: %v", err)
	}
	if loaded.Username != "restored-admin" {
		t.Fatalf("restored username = %q, want %q", loaded.Username, "restored-admin")
	}
	if err := loaded.ComparePassword(restoredPlaintext); err != nil {
		t.Fatalf("restored admin cannot log in: %v", err)
	}
}

// 增量模式同样不得插入无密码账户（WebDAV 恢复走的就是 incremental）。
func TestIncrementalImportRejectsUsersWithoutPasswordHash(t *testing.T) {
	initTestDB(t)

	dump := &model.DBDump{
		Version: 1,
		Users:   []model.User{{ID: 1, Username: "webdav-admin", Password: "", Role: model.UserRoleAdmin}},
	}

	if _, err := ImportIncremental(context.Background(), dump); err != nil {
		t.Fatalf("incremental import: %v", err)
	}

	if got := countUsers(t, ""); got != 0 {
		t.Fatalf("user count = %d, want 0 (bootstrap must stay available)", got)
	}
}

func loadUser(t *testing.T, id uint) (model.User, error) {
	t.Helper()
	var u model.User
	err := internaldb.GetDB().First(&u, id).Error
	return u, err
}

func TestImportForceReopensClosedLogDB(t *testing.T) {
	mainPath := filepath.Join(t.TempDir(), "main.db")
	if err := internaldb.InitDB("sqlite", mainPath, false); err != nil {
		t.Fatalf("init main db: %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "logs.db")
	if err := internaldb.InitLogDB("sqlite", logPath, false); err != nil {
		t.Fatalf("init log db: %v", err)
	}
	t.Cleanup(func() { _ = internaldb.Close() })

	// Simulate logs disabled: log DB disconnected.
	if err := internaldb.CloseLogDB(); err != nil {
		t.Fatalf("close log db: %v", err)
	}
	if internaldb.GetLogDB() != nil {
		t.Fatalf("precondition: log DB should be disconnected")
	}

	dump := &model.DBDump{
		Version:     1,
		IncludeLogs: true,
		RelayLogs:   []model.RelayLog{{ID: 9, Time: 9, RequestModelName: "forced"}},
	}
	if _, err := ImportWithMode(context.Background(), dump, model.ImportModeFull); err != nil {
		t.Fatalf("full import: %v", err)
	}

	// Import must have force-reopened the log DB and written the row.
	logConn := internaldb.GetLogDB()
	if logConn == nil {
		t.Fatalf("log DB should be reconnected after import")
	}
	var count int64
	if err := logConn.Model(&model.RelayLog{}).Where("id = ?", 9).Count(&count).Error; err != nil {
		t.Fatalf("count log db: %v", err)
	}
	if count != 1 {
		t.Fatalf("forced relay log count = %d, want 1", count)
	}
}
