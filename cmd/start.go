package cmd

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/relay/balancer"
	"github.com/gypg/lodestar/internal/server"
	"github.com/gypg/lodestar/internal/task"
	"github.com/gypg/lodestar/internal/utils/cache"
	"github.com/gypg/lodestar/internal/utils/crypto"
	"github.com/gypg/lodestar/internal/utils/log"
	"github.com/gypg/lodestar/internal/utils/shutdown"
	"github.com/gypg/lodestar/internal/utils/telemetry"
	"github.com/spf13/cobra"
)

var cfgFile string

// allowEphemeralEncryptionKey opts in to the old behaviour of generating a
// throwaway encryption key when security.encryption_key is unset. It exists for
// local development only (see README_zh.md "本地开发"): a process started this
// way cannot read any ciphertext written by a previous run, so every sensitive
// setting persisted earlier becomes unreadable — and, worse, unwritable through
// the admin UI (setting.SetString decrypts before comparing). Requiring an
// explicit flag keeps that trade-off a deliberate choice instead of a warning
// nobody reads.
var allowEphemeralEncryptionKey bool

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start " + conf.APP_NAME,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		conf.PrintBanner()
		if err := conf.Load(cfgFile); err != nil {
			return err
		}
		log.SetLevel(conf.AppConfig.Log.Level)
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStart()
	},
}

func runStart() error {
	shutdown.Init(log.Logger)

	if err := initEncryption(); err != nil {
		return err
	}

	if err := db.InitDB(conf.AppConfig.Database.Type, conf.AppConfig.Database.Path, conf.IsDebug()); err != nil {
		return fmt.Errorf("database init error: %w", err)
	}
	// 独立日志库（仅承载 relay_logs）。log_type/log_path 留空时回落到主库，
	// 行为与旧版一致。必须在主库 InitDB 之后调用。
	if err := db.InitLogDB(conf.AppConfig.Database.LogType, conf.AppConfig.Database.LogPath, conf.IsDebug()); err != nil {
		return fmt.Errorf("log database init error: %w", err)
	}
	shutdown.Register(db.Close)

	startupTaskCtx, startupTaskCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if interruptedCount, err := op.AIRouteTaskMarkActiveInterrupted(startupTaskCtx, op.DefaultAIRouteTaskInterruptedMessage); err != nil {
		log.Warnf("ai route task recovery failed: %v", err)
	} else if interruptedCount > 0 {
		log.Warnf("marked %d stale ai route task(s) as interrupted on startup", interruptedCount)
	}
	startupTaskCancel()

	if err := op.InitCache(); err != nil {
		shutdown.Shutdown()
		return fmt.Errorf("cache init error: %w", err)
	}

	// Redis is optional — when redis.host is configured, connect; otherwise skip silently.
	if err := cache.InitRedis(); err != nil {
		log.Warnf("Redis init failed (continuing without Redis): %v", err)
	}

	// One-time backfill of site model hourly stats from relay logs.
	// Runs asynchronously to avoid blocking startup.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		op.StatsSiteModelBackfill(ctx)
	}()

	telemetry.Global().StartBackground()

	restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := balancer.LoadRuntimeState(restoreCtx); err != nil {
		log.Warnf("balancer runtime state load error: %v", err)
	}
	restoreCancel()

	if err := op.UserInit(); err != nil {
		shutdown.Shutdown()
		return fmt.Errorf("user init error: %w", err)
	}
	if err := op.EnsureDevBootstrapData(context.Background()); err != nil {
		shutdown.Shutdown()
		return fmt.Errorf("dev bootstrap init error: %w", err)
	}

	if err := server.Start(); err != nil {
		shutdown.Shutdown()
		return fmt.Errorf("server start error: %w", err)
	}

	loc := time.Now().Location()
	log.Infof("server timezone: %s (UTC offset: %s)", loc.String(), time.Now().Format("-07:00"))
	log.Infof("server local time: %s", time.Now().Format(time.RFC3339))
	log.Infof("server utc time:   %s", time.Now().UTC().Format(time.RFC3339))

	shutdown.Register(server.Close)
	shutdown.Register(func() error {
		telemetry.Global().StopBackground()
		return nil
	})
	shutdown.Register(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return balancer.SaveRuntimeState(ctx)
	})
	shutdown.Register(func() error {
		task.Shutdown()
		db.StopSerialWriter()
		return nil
	})
	shutdown.Register(op.SaveCache)
	shutdown.Register(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		interruptedCount, err := op.AIRouteTaskMarkActiveInterrupted(ctx, op.DefaultAIRouteTaskInterruptedMessage)
		if err != nil {
			return err
		}
		if interruptedCount > 0 {
			log.Warnf("marked %d active ai route task(s) as interrupted during shutdown", interruptedCount)
		}
		return nil
	})

	task.Init()
	go task.RUN()
	shutdown.Listen()
	return nil
}

// initEncryption initialises the at-rest encryption key, refusing to start when
// none is configured (fail-closed).
//
// Starting with a freshly generated key is never a safe default: the key is not
// persisted anywhere, so on the next restart every "enc:" value already in the
// database becomes permanently undecryptable. That is not limited to read
// failures — internal/op/setting.SetString decrypts the cached value before
// comparing it, so a failed decrypt makes the setting unwritable too and the
// affected credentials can no longer be repaired from the admin UI. Callers
// that swallow the read error (stripe.go, turnstile.go) then degrade silently:
// top-ups stop working and the human-verification challenge switches itself off
// with nothing in the logs to explain why.
//
// --allow-ephemeral-encryption-key restores the old behaviour for local
// development, where there is no ciphertext worth preserving.
func initEncryption() error {
	if key := conf.AppConfig.Security.EncryptionKey; key != "" {
		crypto.Init(key)
		return nil
	}
	if !allowEphemeralEncryptionKey {
		return fmt.Errorf(
			"security.encryption_key is not set: refusing to start because a generated key is lost on restart, "+
				"making every encrypted setting permanently unreadable and unwritable. "+
				"Set %s_SECURITY_ENCRYPTION_KEY (or security.encryption_key in the config file) to a long random value, "+
				"or pass --%s for local development only",
			strings.ToUpper(conf.APP_NAME), allowEphemeralEncryptionKeyFlag,
		)
	}
	randomKey, err := generateEphemeralEncryptionKey()
	if err != nil {
		return fmt.Errorf("failed to generate ephemeral encryption key: %w", err)
	}
	crypto.Init(randomKey)
	log.Warnf("--%s is set and security.encryption_key is empty; generated a throwaway key for this process. "+
		"Encrypted settings written now cannot be read after a restart. Never use this in production",
		allowEphemeralEncryptionKeyFlag)
	return nil
}

// generateEphemeralEncryptionKey produces a 32-byte random hex string suitable
// for use as an AES-256 encryption key. Only reachable via
// --allow-ephemeral-encryption-key.
func generateEphemeralEncryptionKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := crypto_rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

const allowEphemeralEncryptionKeyFlag = "allow-ephemeral-encryption-key"

func init() {
	startCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./data/config.json)")
	startCmd.PersistentFlags().BoolVar(&allowEphemeralEncryptionKey, allowEphemeralEncryptionKeyFlag, false,
		"start with a throwaway encryption key when security.encryption_key is unset (local development only; encrypted settings become unreadable after restart)")
	rootCmd.AddCommand(startCmd)
}
