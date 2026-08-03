package migrate

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gypg/lodestar/internal/model"
	"gorm.io/gorm"
)

func siteNameRebrandTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "rebrand.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestMigrateSiteNameRebrandUpgradesOldDefault(t *testing.T) {
	db := siteNameRebrandTestDB(t)
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	// Seed the pre-rebrand default site_name.
	if err := db.Create(&model.Setting{Key: model.SettingKeySiteName, Value: "GGZERO"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := migrateSiteNameRebrand(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var got model.Setting
	if err := db.Where("key = ?", string(model.SettingKeySiteName)).First(&got).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if got.Value != "Lodestar" {
		t.Fatalf("site_name = %q, want Lodestar", got.Value)
	}
}

func TestMigrateSiteNameRebrandPreservesCustomName(t *testing.T) {
	db := siteNameRebrandTestDB(t)
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	// A user-customized site name must NOT be touched.
	if err := db.Create(&model.Setting{Key: model.SettingKeySiteName, Value: "我的私人站"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := migrateSiteNameRebrand(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var got model.Setting
	if err := db.Where("key = ?", string(model.SettingKeySiteName)).First(&got).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if got.Value != "我的私人站" {
		t.Fatalf("site_name = %q, want custom name preserved", got.Value)
	}
}

func TestMigrateSiteNameRebrandIdempotent(t *testing.T) {
	db := siteNameRebrandTestDB(t)
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	if err := db.Create(&model.Setting{Key: model.SettingKeySiteName, Value: "GGZERO"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Run twice — second run should be a no-op (nothing matches "GGZERO" anymore).
	if err := migrateSiteNameRebrand(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := migrateSiteNameRebrand(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	var got model.Setting
	if err := db.Where("key = ?", string(model.SettingKeySiteName)).First(&got).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if got.Value != "Lodestar" {
		t.Fatalf("site_name = %q, want Lodestar after idempotent runs", got.Value)
	}
}

func TestMigrateSiteNameRebrandNoopWhenSettingAbsent(t *testing.T) {
	db := siteNameRebrandTestDB(t)
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	// No site_name row at all — migration must not error.
	if err := migrateSiteNameRebrand(db); err != nil {
		t.Fatalf("migrate on empty settings: %v", err)
	}
}
