package op

import (
	"context"
	"strings"

	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/utils/log"
)

const devMockAPIKeyName = "Dev Mock Success Key"
const devMockAPIKeyValue = "sk-octopus-dev-mock-success-local-0001"

func EnsureDevBootstrapData(ctx context.Context) error {
	if !conf.IsDevMockSuccess() {
		return nil
	}

	if _, err := APIKeyGetByAPIKey(devMockAPIKeyValue, ctx); err == nil {
		log.Infof("dev mock api key already exists: %s...%s", devMockAPIKeyValue[:10], devMockAPIKeyValue[len(devMockAPIKeyValue)-4:])
		return nil
	} else if !strings.Contains(strings.ToLower(err.Error()), "api key not found") {
		return err
	}

	key := &model.APIKey{
		Name:    devMockAPIKeyName,
		APIKey:  devMockAPIKeyValue,
		Enabled: true,
	}
	if err := APIKeyCreate(key, ctx); err != nil {
		return err
	}

	log.Warnf("dev mock success mode enabled; seeded relay api key: %s...%s", devMockAPIKeyValue[:10], devMockAPIKeyValue[len(devMockAPIKeyValue)-4:])
	return nil
}
