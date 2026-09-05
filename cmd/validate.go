package cmd

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/gypg/lodestar/internal/conf"
	"github.com/gypg/lodestar/internal/db"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration and check common deployment issues",
	Long: `Validate configuration and check common deployment issues.

This command performs pre-flight checks to help diagnose configuration
problems before starting the server:

  • Environment variables are set correctly
  • Data directory permissions are writable
  • Database connection is reachable
  • Required ports are available
  • Configuration values are valid

Exit code 0 means all checks passed; non-zero indicates issues found.`,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return conf.Load(cfgFile)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runValidate()
	},
}

func runValidate() error {
	var failed bool
	check := func(name string, fn func() error) {
		err := fn()
		if err != nil {
			fmt.Printf("✗ %s: %v\n", name, err)
			failed = true
		} else {
			fmt.Printf("✓ %s\n", name)
		}
	}

	fmt.Println("Running configuration validation checks...")
	fmt.Println()

	check("Encryption key configured", validateEncryptionKey)
	check("JWT secret configured", validateJWTSecret)
	check("Data directory writable", validateDataDirWritable)
	check("Database connection", validateDatabaseConnection)
	check("Server port available", validateServerPort)
	check("Trusted proxies format", validateTrustedProxies)

	fmt.Println()
	if failed {
		return fmt.Errorf("validation failed: one or more checks did not pass")
	}
	fmt.Println("All checks passed. Configuration looks good!")
	return nil
}

func validateEncryptionKey() error {
	key := conf.AppConfig.Security.EncryptionKey
	if key == "" {
		return fmt.Errorf("security.encryption_key is not set (set %s_SECURITY_ENCRYPTION_KEY environment variable or security.encryption_key in config)", strings.ToUpper(conf.APP_NAME))
	}
	if len(key) < 32 {
		return fmt.Errorf("encryption key is too short (got %d chars, need at least 32 for AES-256)", len(key))
	}
	return nil
}

func validateJWTSecret() error {
	secret := conf.AppConfig.Auth.JWTSecret
	if secret == "" {
		return fmt.Errorf("auth.jwt_secret is not set (will be auto-generated on start, but tokens won't persist across restarts)")
	}
	if len(secret) < 16 {
		return fmt.Errorf("JWT secret is too short (got %d chars, recommended at least 32)", len(secret))
	}
	return nil
}

func validateDataDirWritable() error {
	dataDir := conf.DataDir()
	testFile := filepath.Join(dataDir, ".write-test")

	// Attempt to create the directory if it doesn't exist
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("cannot create data directory %s: %w (hint: check parent directory permissions)", dataDir, err)
	}

	// Try to write a test file
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("cannot write to data directory %s: %w (hint: check directory ownership, should be UID 1000 in Docker)", dataDir, err)
	}

	// Clean up
	os.Remove(testFile)
	return nil
}

func validateDatabaseConnection() error {
	dbType := conf.AppConfig.Database.Type
	dbPath := conf.AppConfig.Database.Path

	if dbType == "" || dbPath == "" {
		return fmt.Errorf("database type or path is empty")
	}

	// For SQLite, check parent directory is writable
	if dbType == "sqlite" {
		dir := filepath.Dir(dbPath)
		if dir == "." {
			dir = conf.DataDir()
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("cannot create database directory %s: %w", dir, err)
		}
		// Don't actually create the DB file, just check the directory
		return nil
	}

	// For PostgreSQL/MySQL, try to connect with a short timeout
	// Note: db.InitDB doesn't currently accept a context, so this timeout
	// relies on the database driver's default connection timeout
	if err := db.InitDB(dbType, dbPath, false); err != nil {
		return fmt.Errorf("cannot connect to %s database: %w (hint: check connection string format and network reachability)", dbType, err)
	}
	db.Close()

	return nil
}

func validateServerPort() error {
	port := conf.AppConfig.Server.Port
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid port %d (must be 1-65535)", port)
	}

	// Try to bind to the port briefly to check if it's available
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d is not available: %w (hint: check for conflicting services with 'netstat -tlnp | grep %d' or 'lsof -i:%d')", port, err, port, port)
	}
	ln.Close()
	return nil
}

func validateTrustedProxies() error {
	proxies := conf.AppConfig.Server.TrustedProxies
	if len(proxies) == 0 {
		// Empty is valid (means trust no proxies)
		return nil
	}

	for _, proxy := range proxies {
		proxy = strings.TrimSpace(proxy)
		if proxy == "" {
			continue
		}

		// Check if it's a valid CIDR or IP
		if strings.Contains(proxy, "/") {
			_, _, err := net.ParseCIDR(proxy)
			if err != nil {
				return fmt.Errorf("invalid CIDR %q: %w (hint: use format like '10.0.0.0/8' or '127.0.0.1/32')", proxy, err)
			}
		} else {
			if net.ParseIP(proxy) == nil {
				return fmt.Errorf("invalid IP address %q (hint: use CIDR notation like '%s/32' for single IPs)", proxy, proxy)
			}
		}
	}
	return nil
}

func init() {
	validateCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ./data/config.json)")
	rootCmd.AddCommand(validateCmd)
}
