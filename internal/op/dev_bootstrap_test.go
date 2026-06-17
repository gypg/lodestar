package op

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/db"
)

func TestEnsureDevBootstrapDataCreatesMockAPIKey(t *testing.T) {
	t.Setenv("OCTOPUS_DEV_MOCK_SUCCESS", "true")

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if err := EnsureDevBootstrapData(context.Background()); err != nil {
		t.Fatalf("EnsureDevBootstrapData() error = %v", err)
	}

	key, err := APIKeyGetByAPIKey(devMockAPIKeyValue, context.Background())
	if err != nil {
		t.Fatalf("APIKeyGetByAPIKey() error = %v", err)
	}
	if !key.Enabled {
		t.Fatal("mock api key is disabled, want enabled")
	}
}
