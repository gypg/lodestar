package errorlog

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

func setupErrorLogTestDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Clear(context.Background()); err != nil {
		t.Fatalf("clear error logs: %v", err)
	}
}

func TestErrorLogAutoMigrateCreatesTable(t *testing.T) {
	setupErrorLogTestDB(t)
	if !db.GetDB().Migrator().HasTable(&model.ErrorLog{}) {
		t.Fatal("AutoMigrate did not create error_logs table")
	}
}

func TestErrorLogAddListGetClear(t *testing.T) {
	setupErrorLogTestDB(t)
	ctx := context.Background()

	if err := Add(ctx, model.ErrorLog{
		Source:  "backend",
		Level:   "panic",
		Message: "boom: nil pointer",
		Stack:   "goroutine 1 [running]:...",
	}); err != nil {
		t.Fatalf("add backend entry: %v", err)
	}
	if err := Add(ctx, model.ErrorLog{
		Source:  "frontend",
		Level:   "error",
		Message: "frontend TypeError",
		PageURL: "https://example.com/settings",
	}); err != nil {
		t.Fatalf("add frontend entry: %v", err)
	}
	// Add 补默认值：Source 缺省 backend。
	if err := Add(ctx, model.ErrorLog{Level: "error", Message: "default source"}); err != nil {
		t.Fatalf("add default-source entry: %v", err)
	}

	entries, err := List(ctx, Filter{}, 1, 20)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("list returned %d entries, want 3", len(entries))
	}
	// 倒序：后插入的（更大雪花 ID）在前。
	if entries[0].Message != "default source" || entries[1].Source != "frontend" || entries[2].Source != "backend" {
		t.Fatalf("list order wrong: %+v", entries)
	}
	if entries[0].Source != "backend" {
		t.Fatalf("missing Source should default to backend, got %q", entries[0].Source)
	}
	if entries[0].Time == 0 {
		t.Fatal("Add did not default Time to now")
	}

	// Source 过滤。
	onlyBackend, err := List(ctx, Filter{Source: "backend"}, 1, 20)
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(onlyBackend) != 2 {
		t.Fatalf("source filter returned %d entries, want 2", len(onlyBackend))
	}

	// Level 过滤。
	onlyPanic, err := List(ctx, Filter{Level: "panic"}, 1, 20)
	if err != nil {
		t.Fatalf("list by level: %v", err)
	}
	if len(onlyPanic) != 1 || onlyPanic[0].Message != "boom: nil pointer" {
		t.Fatalf("level filter returned %+v", onlyPanic)
	}

	// GetByID 命中与未命中。
	got, err := GetByID(ctx, onlyPanic[0].ID)
	if err != nil || got == nil {
		t.Fatalf("GetByID(existing) = (%v, %v)", got, err)
	}
	if got.Stack == "" {
		t.Fatal("GetByID lost stack field")
	}
	missing, err := GetByID(ctx, 1) // 雪花 ID 远大于 1，必不存在
	if err != nil || missing != nil {
		t.Fatalf("GetByID(missing) = (%v, %v), want (nil, nil)", missing, err)
	}

	// Clear。
	if err := Clear(ctx); err != nil {
		t.Fatalf("clear: %v", err)
	}
	entries, err = List(ctx, Filter{}, 1, 20)
	if err != nil {
		t.Fatalf("list after clear: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("clear left %d entries", len(entries))
	}
}

func TestErrorLogCleanupRetention(t *testing.T) {
	setupErrorLogTestDB(t)
	ctx := context.Background()

	// 直接批量插入 6001 行（ID 用递增序列模拟雪花的时间有序性，
	// 避免逐条 Add 触发 6001 次 INSERT + 雪花毫秒自增）。
	const total = 6001
	rows := make([]model.ErrorLog, 0, total)
	for i := 1; i <= total; i++ {
		rows = append(rows, model.ErrorLog{ID: int64(i), Time: int64(i), Source: "backend", Level: "panic", Message: "m"})
	}
	if err := db.GetDB().WithContext(ctx).CreateInBatches(rows, 1000).Error; err != nil {
		t.Fatalf("seed rows: %v", err)
	}

	if err := Cleanup(ctx); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	var remaining int64
	if err := db.GetDB().WithContext(ctx).Model(&model.ErrorLog{}).Count(&remaining).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	// 超限（6001 > 5000）→ 删最旧一半（3000）→ 剩 3001。
	if remaining != total-total/2 {
		t.Fatalf("after cleanup remaining = %d, want %d", remaining, total-total/2)
	}
	// 删掉的必须是最旧的：剩余最小 ID 应为 3001。
	var minID int64
	if err := db.GetDB().WithContext(ctx).Model(&model.ErrorLog{}).
		Select("MIN(id)").Scan(&minID).Error; err != nil {
		t.Fatalf("min id: %v", err)
	}
	if minID != total/2+1 {
		t.Fatalf("oldest surviving id = %d, want %d", minID, total/2+1)
	}

	// 未超限时 Cleanup 不删任何行。
	if err := Cleanup(ctx); err != nil {
		t.Fatalf("second cleanup: %v", err)
	}
	if err := db.GetDB().WithContext(ctx).Model(&model.ErrorLog{}).Count(&remaining).Error; err != nil {
		t.Fatalf("count after second cleanup: %v", err)
	}
	if remaining != total-total/2 {
		t.Fatalf("cleanup below threshold deleted rows: remaining = %d", remaining)
	}
}
