package modelmapping

import (
	"context"
	"path/filepath"
	"testing"

	dbpkg "github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

// Create 落库走 gorm struct Create。ModelMapping.Enabled 带 default:true 标签，
// gorm 的 create 回调会用默认值替换零值字段——操作者显式给的 enabled=false
// 会落库成 true。这两条测试钉住显式 false / 显式 true 都必须原样落库。

func setupMappingCreateTestDB(t *testing.T) context.Context {
	t.Helper()

	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}

	dbPath := filepath.Join(t.TempDir(), "lodestar-modelmapping-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = dbpkg.Close()
	})

	return context.Background()
}

func loadMappingByID(t *testing.T, ctx context.Context, id int) model.ModelMapping {
	t.Helper()
	var saved model.ModelMapping
	if err := dbpkg.GetDB().WithContext(ctx).First(&saved, id).Error; err != nil {
		t.Fatalf("load model mapping %d failed: %v", id, err)
	}
	return saved
}

func TestCreatePersistsExplicitDisabled(t *testing.T) {
	ctx := setupMappingCreateTestDB(t)

	enabled := false
	mapping, err := Create(ctx, &model.ModelMappingCreateRequest{
		Name:        "disabled-mapping",
		Pattern:     "gpt-*",
		MatchType:   model.MatchWildcard,
		TargetModel: "target-model",
		Priority:    3,
		Enabled:     &enabled,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	// create 回调会把结构体里的 false 回写成 true；返回给调用方的值必须如实。
	if mapping.Enabled {
		t.Fatalf("Create returned mapping with enabled=true; caller asked for false")
	}

	saved := loadMappingByID(t, ctx, mapping.ID)
	if saved.Enabled {
		t.Fatalf("user asked for enabled=false; stored enabled=true")
	}
	if saved.Pattern != "gpt-*" || saved.TargetModel != "target-model" || saved.Priority != 3 {
		t.Fatalf("expected other columns to be persisted as given, got %+v", saved)
	}
}

func TestCreatePersistsExplicitEnabled(t *testing.T) {
	ctx := setupMappingCreateTestDB(t)

	enabled := true
	mapping, err := Create(ctx, &model.ModelMappingCreateRequest{
		Name:        "enabled-mapping",
		Pattern:     "claude-*",
		MatchType:   model.MatchWildcard,
		TargetModel: "target-model",
		Priority:    1,
		Enabled:     &enabled,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	saved := loadMappingByID(t, ctx, mapping.ID)
	if !saved.Enabled {
		t.Fatalf("user asked for enabled=true; stored enabled=false")
	}
}
