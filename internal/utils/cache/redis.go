package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/utils/log"
	"github.com/redis/go-redis/v9"
)

// RedisClient is the global Redis client. Nil when Redis is not configured.
var RedisClient *redis.Client

// InitRedis initializes the global Redis client if redis.host is configured.
// Returns nil (no error) when Redis is not configured — this is not a failure.
func InitRedis() error {
	cfg := conf.AppConfig.Redis
	if cfg.Host == "" {
		log.Infof("Redis not configured (redis.host empty), using in-memory cache only")
		return nil
	}

	addr := cfg.Host
	if cfg.Port > 0 {
		addr = addr + ":" + fmt.Sprintf("%d", cfg.Port)
	}

	RedisClient = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := RedisClient.Ping(ctx).Err(); err != nil {
		RedisClient = nil
		return fmt.Errorf("redis ping failed: %w", err)
	}

	log.Infof("Redis connected: %s (db=%d)", addr, cfg.DB)
	return nil
}

// IsRedisAvailable returns true if Redis is configured and connected.
func IsRedisAvailable() bool {
	return RedisClient != nil
}
