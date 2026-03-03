package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildInputPlugins_RegistersStdin(t *testing.T) {
	t.Parallel()

	plugins := buildInputPlugins()

	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].Name() != "stdin" {
		t.Fatalf("plugins[0] name = %q, want %q", plugins[0].Name(), "stdin")
	}
}

func TestLoadConfig_AddressResolution(t *testing.T) {
	resetLotusEnv(t)

	tests := []struct {
		name         string
		configYAML   string
		wantErr      bool
		wantHost     string
		wantGRPCAddr string
		wantAPIAddr  string
		errSubstring string
	}{
		{
			name: "defaults to localhost host",
			configYAML: `
grpc-port: 4317
api-port: 3100
`,
			wantHost:     "127.0.0.1",
			wantGRPCAddr: "127.0.0.1:4317",
			wantAPIAddr:  "127.0.0.1:3100",
		},
		{
			name: "host applies to derived grpc and api addresses",
			configYAML: `
host: 0.0.0.0
grpc-port: 4318
api-port: 3200
`,
			wantHost:     "0.0.0.0",
			wantGRPCAddr: "0.0.0.0:4318",
			wantAPIAddr:  "0.0.0.0:3200",
		},
		{
			name: "explicit addresses override host and ports",
			configYAML: `
host: 0.0.0.0
grpc-port: 4319
api-port: 3300
grpc-addr: 10.0.0.5:9999
api-addr: 10.0.0.5:8888
`,
			wantHost:     "0.0.0.0",
			wantGRPCAddr: "10.0.0.5:9999",
			wantAPIAddr:  "10.0.0.5:8888",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := writeTempConfig(t, tt.configYAML)
			cfg, err := loadConfig(configPath)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstring != "" && !strings.Contains(err.Error(), tt.errSubstring) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tt.errSubstring)
				}
				return
			}

			if err != nil {
				t.Fatalf("loadConfig returned error: %v", err)
			}

			if cfg.Host != tt.wantHost {
				t.Fatalf("Host = %q, want %q", cfg.Host, tt.wantHost)
			}
			if cfg.GRPCAddr != tt.wantGRPCAddr {
				t.Fatalf("GRPCAddr = %q, want %q", cfg.GRPCAddr, tt.wantGRPCAddr)
			}
			if cfg.APIAddr != tt.wantAPIAddr {
				t.Fatalf("APIAddr = %q, want %q", cfg.APIAddr, tt.wantAPIAddr)
			}
		})
	}
}

func TestLoadConfig_BackupSettings(t *testing.T) {
	resetLotusEnv(t)

	tests := []struct {
		name         string
		configYAML   string
		wantErr      bool
		errSubstring string
		assert       func(t *testing.T, cfg appConfig)
	}{
		{
			name: "backup defaults disabled",
			configYAML: `
grpc-port: 4317
api-port: 3000
`,
			assert: func(t *testing.T, cfg appConfig) {
				t.Helper()
				if cfg.BackupEnabled {
					t.Fatal("backup should be disabled by default")
				}
				if cfg.BackupInterval <= 0 {
					t.Fatalf("backup interval should be > 0, got %s", cfg.BackupInterval)
				}
				if cfg.BackupKeepLast <= 0 {
					t.Fatalf("backup keep-last should be > 0, got %d", cfg.BackupKeepLast)
				}
			},
		},
		{
			name: "backup accepts custom s3 config",
			configYAML: `
backup-enabled: true
backup-interval: 1h
backup-local-dir: /tmp/lotus-backups
backup-keep-last: 10
backup-bucket-url: s3://my-bucket/lotus
backup-s3-endpoint: s3.amazonaws.com
backup-s3-region: us-east-1
backup-s3-access-key: key
backup-s3-secret-key: secret
backup-s3-use-ssl: true
grpc-port: 4317
api-port: 3000
`,
			assert: func(t *testing.T, cfg appConfig) {
				t.Helper()
				if !cfg.BackupEnabled {
					t.Fatal("backup should be enabled")
				}
				if cfg.BackupBucketURL != "s3://my-bucket/lotus" {
					t.Fatalf("bucket url = %q", cfg.BackupBucketURL)
				}
				if cfg.BackupKeepLast != 10 {
					t.Fatalf("keep-last = %d, want 10", cfg.BackupKeepLast)
				}
			},
		},
		{
			name: "invalid backup interval rejected",
			configYAML: `
backup-enabled: true
backup-interval: 0s
grpc-port: 4317
api-port: 3000
`,
			wantErr:      true,
			errSubstring: "invalid backup-interval",
		},
		{
			name: "invalid backup keep-last rejected",
			configYAML: `
backup-enabled: true
backup-keep-last: -1
grpc-port: 4317
api-port: 3000
`,
			wantErr:      true,
			errSubstring: "invalid backup-keep-last",
		},
		{
			name: "bucket url requires credentials",
			configYAML: `
backup-enabled: true
backup-bucket-url: s3://my-bucket/lotus
grpc-port: 4317
api-port: 3000
`,
			wantErr:      true,
			errSubstring: "backup-s3-access-key and backup-s3-secret-key are required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := writeTempConfig(t, tt.configYAML)
			cfg, err := loadConfig(configPath)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstring != "" && !strings.Contains(err.Error(), tt.errSubstring) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tt.errSubstring)
				}
				return
			}

			if err != nil {
				t.Fatalf("loadConfig returned error: %v", err)
			}
			if tt.assert != nil {
				tt.assert(t, cfg)
			}
		})
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func resetLotusEnv(t *testing.T) {
	t.Helper()

	original := make(map[string]string)
	existed := make(map[string]bool)

	for _, kv := range os.Environ() {
		key, value, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(key, "LOTUS_") {
			continue
		}
		original[key] = value
		existed[key] = true
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}

	t.Cleanup(func() {
		for key := range existed {
			if err := os.Unsetenv(key); err != nil {
				t.Fatalf("cleanup unset %s: %v", key, err)
			}
		}
		for key, value := range original {
			if err := os.Setenv(key, value); err != nil {
				t.Fatalf("cleanup restore %s: %v", key, err)
			}
		}
	})
}
