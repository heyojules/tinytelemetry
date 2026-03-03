package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tinytelemetry/lotus/internal/socketrpc"

	"github.com/spf13/viper"
)

// Build variables - set by ldflags during build.
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
	goVersion = "unknown"
)

// GetVersionInfo returns the current version and commit information.
func GetVersionInfo() (string, string) {
	return version, commit
}

func main() {
	var configPath string
	var showVersion bool

	flag.StringVar(&configPath, "config", "", "config file (default is $HOME/.config/lotus/config.yml)")
	flag.BoolVar(&showVersion, "version", false, "print version information")
	flag.Parse()

	if showVersion {
		fmt.Printf("Lotus - Log Ingestion Service\n")
		fmt.Printf("  Version:    %s\n", version)
		fmt.Printf("  Commit:     %s\n", commit)
		fmt.Printf("  Built:      %s\n", buildTime)
		fmt.Printf("  Go version: %s\n", goVersion)
		return
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if err := runServer(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func loadConfig(configPath string) (appConfig, error) {
	var cfg appConfig

	home, err := os.UserHomeDir()
	if err != nil {
		return cfg, fmt.Errorf("finding home directory: %w", err)
	}

	defaultDBPath := filepath.Join(home, ".local", "share", "lotus", "lotus.duckdb")
	defaultBackupDir := filepath.Join(home, ".local", "share", "lotus", "backups")
	defaultJournalPath := filepath.Join(home, ".local", "state", "lotus", "ingest.journal")

	v := viper.New()
	v.SetEnvPrefix("LOTUS")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	v.SetDefault("update-interval", defaultUpdateInterval)
	v.SetDefault("log-buffer", defaultLogBuffer)
	v.SetDefault("test-mode", false)
	v.SetDefault("host", defaultBindHost)
	v.SetDefault("grpc-enabled", true)
	v.SetDefault("grpc-port", defaultGRPCPort)
	v.SetDefault("mux-buffer-size", defaultMuxBufferSize)
	v.SetDefault("db-path", defaultDBPath)
	v.SetDefault("skin", defaultSkin)
	v.SetDefault("disable-version-check", false)
	v.SetDefault("reverse-scroll-wheel", false)
	v.SetDefault("use-log-time", false)
	v.SetDefault("api-enabled", true)
	v.SetDefault("api-port", defaultAPIPort)
	v.SetDefault("query-timeout", defaultQueryTimeout)
	v.SetDefault("max-concurrent-queries", defaultMaxConcurrentReads)
	v.SetDefault("insert-batch-size", defaultInsertBatchSize)
	v.SetDefault("insert-flush-interval", defaultInsertFlushInterval)
	v.SetDefault("insert-flush-queue-size", defaultInsertFlushQueue)
	v.SetDefault("journal-enabled", defaultJournalEnabled)
	v.SetDefault("journal-path", defaultJournalPath)
	v.SetDefault("socket-path", socketrpc.DefaultSocketPath())
	v.SetDefault("log-retention", defaultLogRetention)
	v.SetDefault("backup-enabled", false)
	v.SetDefault("backup-interval", defaultBackupInterval)
	v.SetDefault("backup-local-dir", defaultBackupDir)
	v.SetDefault("backup-keep-last", defaultBackupKeepLast)
	v.SetDefault("backup-bucket-url", "")
	v.SetDefault("backup-s3-endpoint", "")
	v.SetDefault("backup-s3-region", defaultBackupS3Region)
	v.SetDefault("backup-s3-access-key", "")
	v.SetDefault("backup-s3-secret-key", "")
	v.SetDefault("backup-s3-session-token", "")
	v.SetDefault("backup-s3-use-ssl", true)

	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		defaultConfigPath := filepath.Join(home, ".config", "lotus", "config.yml")
		v.SetConfigFile(defaultConfigPath)
	}

	if err := v.ReadInConfig(); err != nil {
		var configFileNotFound viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFound) && !os.IsNotExist(err) {
			return cfg, err
		}
	}

	if err := v.Unmarshal(&cfg); err != nil {
		return cfg, err
	}
	cfg.ConfigPath = v.ConfigFileUsed()
	if cfg.GRPCPort <= 0 || cfg.GRPCPort > 65535 {
		return cfg, fmt.Errorf("invalid grpc-port: %d", cfg.GRPCPort)
	}
	if cfg.APIPort <= 0 || cfg.APIPort > 65535 {
		return cfg, fmt.Errorf("invalid api-port: %d", cfg.APIPort)
	}
	if cfg.BackupEnabled && cfg.BackupInterval <= 0 {
		return cfg, fmt.Errorf("invalid backup-interval: %s", cfg.BackupInterval)
	}
	if cfg.BackupEnabled && cfg.BackupKeepLast < 0 {
		return cfg, fmt.Errorf("invalid backup-keep-last: %d", cfg.BackupKeepLast)
	}
	if cfg.BackupEnabled && strings.TrimSpace(cfg.BackupBucketURL) != "" {
		if strings.TrimSpace(cfg.BackupS3AccessKey) == "" || strings.TrimSpace(cfg.BackupS3SecretKey) == "" {
			return cfg, fmt.Errorf("backup-s3-access-key and backup-s3-secret-key are required when backup-bucket-url is set")
		}
	}

	// Expand ~ in db-path
	if strings.HasPrefix(cfg.DBPath, "~/") {
		cfg.DBPath = filepath.Join(home, cfg.DBPath[2:])
	}
	if strings.HasPrefix(cfg.BackupLocalDir, "~/") {
		cfg.BackupLocalDir = filepath.Join(home, cfg.BackupLocalDir[2:])
	}
	if strings.HasPrefix(cfg.JournalPath, "~/") {
		cfg.JournalPath = filepath.Join(home, cfg.JournalPath[2:])
	}
	if cfg.BackupEnabled && cfg.DBPath == "" {
		return cfg, fmt.Errorf("backup-enabled requires on-disk db-path")
	}

	host := cfg.Host
	if host == "" {
		host = defaultBindHost
	}

	if cfg.GRPCAddr == "" {
		cfg.GRPCAddr = net.JoinHostPort(host, strconv.Itoa(cfg.GRPCPort))
	}
	if cfg.APIAddr == "" {
		cfg.APIAddr = net.JoinHostPort(host, strconv.Itoa(cfg.APIPort))
	}

	return cfg, nil
}
