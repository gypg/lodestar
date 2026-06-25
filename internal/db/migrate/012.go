package migrate

import (
	"fmt"

	"github.com/gypg/lodestar/internal/transformer/outbound"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 12,
		Up:      migrateOpenAIChatChannelsToResponses,
		Down:    stubDown(12),
	})
}

func migrateOpenAIChatChannelsToResponses(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable("channels") {
		return nil
	}
	if !db.Migrator().HasColumn("channels", "type") {
		return nil
	}

	if err := db.Exec(
		"UPDATE channels SET type = ? WHERE type = ?",
		outbound.OutboundTypeOpenAIResponse,
		outbound.OutboundTypeOpenAIChat,
	).Error; err != nil {
		return fmt.Errorf("failed to migrate OpenAI chat channels to responses: %w", err)
	}

	return nil
}
