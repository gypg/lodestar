package migrate

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
	"gorm.io/gorm"
)

func TestMigrateOpenAIChatChannelsToResponses(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	defer sqlDB.Close()

	if err := db.Exec(`
		CREATE TABLE channels (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			type INTEGER NOT NULL
		)
	`).Error; err != nil {
		t.Fatalf("create channels table: %v", err)
	}

	if err := db.Exec(`
		INSERT INTO channels (name, type)
		VALUES ('legacy-openai', ?), ('already-response', ?), ('anthropic', ?)
	`, outbound.OutboundTypeOpenAIChat, outbound.OutboundTypeOpenAIResponse, outbound.OutboundTypeAnthropic).Error; err != nil {
		t.Fatalf("insert channels: %v", err)
	}

	if err := migrateOpenAIChatChannelsToResponses(db); err != nil {
		t.Fatalf("migrateOpenAIChatChannelsToResponses: %v", err)
	}

	var rows []struct {
		Name string
		Type outbound.OutboundType
	}
	if err := db.Table("channels").Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("read channels: %v", err)
	}

	if rows[0].Type != outbound.OutboundTypeOpenAIResponse {
		t.Fatalf("legacy-openai type = %d, want %d", rows[0].Type, outbound.OutboundTypeOpenAIResponse)
	}
	if rows[1].Type != outbound.OutboundTypeOpenAIResponse {
		t.Fatalf("already-response type = %d, want %d", rows[1].Type, outbound.OutboundTypeOpenAIResponse)
	}
	if rows[2].Type != outbound.OutboundTypeAnthropic {
		t.Fatalf("anthropic type = %d, want %d", rows[2].Type, outbound.OutboundTypeAnthropic)
	}
}
