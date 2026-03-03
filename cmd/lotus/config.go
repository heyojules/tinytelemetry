package main

import (
	"time"

	"github.com/tinytelemetry/lotus/internal/model"
)

const (
	defaultUpdateInterval      = model.DefaultUpdateInterval
	defaultLogBuffer           = model.DefaultLogBuffer
	defaultBindHost            = "127.0.0.1"
	defaultGRPCPort            = 4317
	defaultMuxBufferSize       = DefaultMuxBuffer
	defaultSkin                = model.DefaultSkin
	defaultAPIPort             = 5000
	defaultQueryTimeout        = 30 * time.Second
	defaultMaxConcurrentReads  = 8
	defaultInsertBatchSize     = 2000
	defaultInsertFlushInterval = 100 * time.Millisecond
	defaultInsertFlushQueue    = 64
	defaultJournalEnabled      = true
	defaultLogRetention        = 30 // days, 0 = disabled
	defaultBackupInterval      = 6 * time.Hour
	defaultBackupKeepLast      = 24
	defaultBackupS3Region      = "us-east-1"
)

// appConfig is internal runtime configuration.
// It is package-private to keep defaults and shape local to the CLI entrypoint.
type appConfig struct {
	UpdateInterval       time.Duration `mapstructure:"update-interval"`
	LogBuffer            int           `mapstructure:"log-buffer"`
	TestMode             bool          `mapstructure:"test-mode"`
	Host                 string        `mapstructure:"host"`
	GRPCEnabled          bool          `mapstructure:"grpc-enabled"`
	GRPCPort             int           `mapstructure:"grpc-port"`
	GRPCAddr             string        `mapstructure:"grpc-addr"`
	MuxBufferSize        int           `mapstructure:"mux-buffer-size"`
	DBPath               string        `mapstructure:"db-path"`
	Skin                 string        `mapstructure:"skin"`
	DisableVersionCheck  bool          `mapstructure:"disable-version-check"`
	ReverseScrollWheel   bool          `mapstructure:"reverse-scroll-wheel"`
	UseLogTime           bool          `mapstructure:"use-log-time"`
	APIEnabled           bool          `mapstructure:"api-enabled"`
	APIPort              int           `mapstructure:"api-port"`
	APIAddr              string        `mapstructure:"api-addr"`
	QueryTimeout         time.Duration `mapstructure:"query-timeout"`
	MaxConcurrentReads   int           `mapstructure:"max-concurrent-queries"`
	InsertBatchSize      int           `mapstructure:"insert-batch-size"`
	InsertFlushInterval  time.Duration `mapstructure:"insert-flush-interval"`
	InsertFlushQueue     int           `mapstructure:"insert-flush-queue-size"`
	JournalEnabled       bool          `mapstructure:"journal-enabled"`
	JournalPath          string        `mapstructure:"journal-path"`
	SocketPath           string        `mapstructure:"socket-path"`
	LogRetention         int           `mapstructure:"log-retention"`
	BackupEnabled        bool          `mapstructure:"backup-enabled"`
	BackupInterval       time.Duration `mapstructure:"backup-interval"`
	BackupLocalDir       string        `mapstructure:"backup-local-dir"`
	BackupKeepLast       int           `mapstructure:"backup-keep-last"`
	BackupBucketURL      string        `mapstructure:"backup-bucket-url"`
	BackupS3Endpoint     string        `mapstructure:"backup-s3-endpoint"`
	BackupS3Region       string        `mapstructure:"backup-s3-region"`
	BackupS3AccessKey    string        `mapstructure:"backup-s3-access-key"`
	BackupS3SecretKey    string        `mapstructure:"backup-s3-secret-key"`
	BackupS3SessionToken string        `mapstructure:"backup-s3-session-token"`
	BackupS3UseSSL       bool          `mapstructure:"backup-s3-use-ssl"`
	ConfigPath           string        `mapstructure:"-"` // not from config file
}
