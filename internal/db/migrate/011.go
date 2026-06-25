package migrate

import (
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 11,
		Up:      migrateNavOrderCleanup,
		Down:    stubDown(11),
	})
}

// validNavItems is the canonical set of top-level navigation items.
// Items like "checkin", "credential", "announcement" have been merged
// into the Hub module as tabs and should no longer appear as standalone
// nav entries.
var validNavItems = map[string]struct{}{
	"home":      {},
	"hub":       {},
	"channel":   {},
	"group":     {},
	"model":     {},
	"analytics": {},
	"log":       {},
	"alert":     {},
	"ops":       {},
	"apikey":    {},
	"setting":   {},
	"user":      {},
}

// defaultNavOrder is the canonical default order for top-level nav items.
var defaultNavOrder = []string{
	"home", "hub", "channel", "group", "model",
	"analytics", "log", "alert", "ops", "apikey", "setting", "user",
}

func migrateNavOrderCleanup(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable("settings") {
		return nil
	}

	keys := []string{"nav_order", "nav_visible"}
	for _, key := range keys {
		if err := cleanupNavSetting(db, key); err != nil {
			return fmt.Errorf("cleanup %s: %w", key, err)
		}
	}
	return nil
}

func cleanupNavSetting(db *gorm.DB, key string) error {
	type Setting struct {
		Key   string `gorm:"primaryKey"`
		Value string
	}

	var setting Setting
	result := db.Where("`key` = ?", key).First(&setting)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil
		}
		return result.Error
	}

	raw := strings.TrimSpace(setting.Value)
	if raw == "" {
		return nil
	}

	var items []string
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		// not valid JSON — overwrite with default
		return setNavDefault(db, key)
	}

	// Filter to only valid items, preserving input order
	filtered := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if _, ok := validNavItems[item]; !ok {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		filtered = append(filtered, item)
	}

	// Append any missing defaults
	for _, item := range defaultNavOrder {
		if _, ok := seen[item]; ok {
			continue
		}
		filtered = append(filtered, item)
	}

	// Check if anything changed
	newJSON, err := json.Marshal(filtered)
	if err != nil {
		return err
	}

	if string(newJSON) == raw {
		return nil // nothing to update
	}

	return db.Model(&Setting{}).Where("`key` = ?", key).Update("value", string(newJSON)).Error
}

func setNavDefault(db *gorm.DB, key string) error {
	defaultJSON, _ := json.Marshal(defaultNavOrder)
	type Setting struct {
		Key   string `gorm:"primaryKey"`
		Value string
	}
	return db.Model(&Setting{}).Where("`key` = ?", key).Update("value", string(defaultJSON)).Error
}
