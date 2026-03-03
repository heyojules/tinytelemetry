package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/tinytelemetry/lotus/internal/backup"
	"github.com/tinytelemetry/lotus/internal/duckdb"
	"github.com/tinytelemetry/lotus/internal/httpserver"
	"github.com/tinytelemetry/lotus/internal/ingest"
	"github.com/tinytelemetry/lotus/internal/journal"
	"github.com/tinytelemetry/lotus/internal/model"
	"github.com/tinytelemetry/lotus/internal/otlpreceiver"
	"github.com/tinytelemetry/lotus/internal/socketrpc"
	"golang.org/x/sync/errgroup"
)

// runServer starts headless log ingestion with the HTTP API.
func runServer(cfg appConfig) error {
	cleanupLogger := configureRuntimeLogger()
	defer cleanupLogger()

	// Initialize DuckDB store
	store, err := duckdb.NewStore(cfg.DBPath, cfg.QueryTimeout)
	if err != nil {
		return fmt.Errorf("failed to initialize DuckDB: %w", err)
	}
	defer store.Close()
	store.SetMaxConcurrentQueries(cfg.MaxConcurrentReads)

	// Open local ingest journal for crash-safe replay and durable buffering.
	var ingestJournal *journal.Journal
	if cfg.JournalEnabled {
		ingestJournal, err = journal.Open(cfg.JournalPath)
		if err != nil {
			return fmt.Errorf("failed to open ingest journal: %w", err)
		}
		if err := replayUncommittedJournal(ingestJournal, store, cfg.InsertBatchSize); err != nil {
			_ = ingestJournal.Close()
			return fmt.Errorf("failed to replay ingest journal: %w", err)
		}
	}

	// Create insert buffer for batched DuckDB writes
	insertBuffer := duckdb.NewInsertBuffer(store, duckdb.InsertBufferConfig{
		BatchSize:      cfg.InsertBatchSize,
		FlushInterval:  cfg.InsertFlushInterval,
		FlushQueueSize: cfg.InsertFlushQueue,
		Journal:        ingestJournal,
	})
	defer insertBuffer.Stop()

	// Start retention cleaner for automatic log expiry
	retentionCleaner := duckdb.NewRetentionCleaner(store, duckdb.RetentionConfig{
		RetentionDays: cfg.LogRetention,
	})
	if retentionCleaner != nil {
		defer retentionCleaner.Stop()
	}

	// Start periodic backups when enabled.
	backupManager, err := backup.NewManager(store, backup.Config{
		Enabled:        cfg.BackupEnabled,
		Interval:       cfg.BackupInterval,
		LocalDir:       cfg.BackupLocalDir,
		KeepLast:       cfg.BackupKeepLast,
		BucketURL:      cfg.BackupBucketURL,
		S3Endpoint:     cfg.BackupS3Endpoint,
		S3Region:       cfg.BackupS3Region,
		S3AccessKey:    cfg.BackupS3AccessKey,
		S3SecretKey:    cfg.BackupS3SecretKey,
		S3SessionToken: cfg.BackupS3SessionToken,
		S3UseSSL:       cfg.BackupS3UseSSL,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize backups: %w", err)
	}
	if backupManager != nil {
		defer backupManager.Stop()
	}

	// Start HTTP API server if enabled
	if cfg.APIEnabled {
		apiServer := httpserver.NewServer(cfg.APIAddr, store)
		if err := apiServer.Start(); err != nil {
			return fmt.Errorf("failed to start API server: %w", err)
		}
		defer apiServer.Stop()
	}

	// Start socket RPC server for TUI IPC
	sockServer := socketrpc.NewServer(cfg.SocketPath, store)
	if err := sockServer.Start(); err != nil {
		log.Printf("Warning: failed to start socket server: %v", err)
	} else {
		defer sockServer.Stop()
	}

	// Set up context and signal handling before errgroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down gracefully... (press Ctrl+C again to force)")
		cancel()

		// Shutdown deadline starts now — not at boot.
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()

		select {
		case <-sigCh:
			fmt.Println("\nForce shutdown.")
		case <-deadline.C:
			fmt.Println("Shutdown timed out, forcing exit.")
		}
		cleanupSocket(cfg.SocketPath)
		os.Exit(1)
	}()

	// Start OTLP/gRPC receiver if enabled
	if cfg.GRPCEnabled {
		otlpServer := otlpreceiver.NewServer(cfg.GRPCAddr, insertBuffer)
		if err := otlpServer.Start(); err != nil {
			return fmt.Errorf("failed to start OTLP receiver: %w", err)
		}
		defer otlpServer.Stop()
	}

	// Build input plugins and source multiplexer
	plugins := buildInputPlugins()

	sources := make([]NamedLogSource, 0, len(plugins))
	for _, plugin := range plugins {
		if !plugin.Enabled() {
			continue
		}
		src, err := plugin.Build(ctx)
		if err != nil {
			log.Printf("Error initializing input plugin %q: %v", plugin.Name(), err)
			continue
		}
		sources = append(sources, src)
	}

	if len(sources) == 0 {
		// Fall back to stdin if piped
		fallback := stdinInputPlugin{}
		if fallback.Enabled() {
			if src, err := fallback.Build(ctx); err == nil {
				sources = append(sources, src)
			}
		}
	}

	mux := NewSourceMultiplexer(ctx, sources, cfg.MuxBufferSize)
	mux.Start()

	// OTEL is the single supported processing path.
	processor := ingest.NewEnvelopeProcessor(insertBuffer, "")

	printStartupBanner(cfg, mux.HasSources(), processor.Name())

	// Use errgroup for concurrent goroutine lifecycle management.
	g, gctx := errgroup.WithContext(ctx)

	// Ingestion loop
	if mux.HasSources() {
		g.Go(func() error {
			for env := range mux.Lines() {
				processor.ProcessEnvelope(env)
			}
			return nil
		})
	}

	// Wait for context cancellation (from signal handler) in the errgroup
	g.Go(func() error {
		<-gctx.Done()
		return nil
	})

	// Wait for either signal or all sources to close.
	if err := g.Wait(); err != nil {
		log.Printf("server: errgroup exited with error: %v", err)
	}

	cancel()
	mux.Stop()

	// If we reach here, graceful shutdown succeeded within the deadline.
	// The signal goroutine (if active) dies with the process.
	signal.Stop(sigCh)

	return nil
}

func cleanupSocket(path string) {
	if path != "" {
		os.Remove(path)
	}
}

func configureRuntimeLogger() func() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	home, err := os.UserHomeDir()
	if err != nil {
		log.SetOutput(os.Stderr)
		return func() {}
	}

	logDir := filepath.Join(home, ".local", "state", "lotus")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.SetOutput(os.Stderr)
		return func() {}
	}

	logPath := filepath.Join(logDir, "lotus.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.SetOutput(os.Stderr)
		return func() {}
	}

	log.SetOutput(f)
	return func() {
		_ = f.Close()
	}
}

func replayUncommittedJournal(j *journal.Journal, store *duckdb.Store, batchSize int) error {
	if j == nil {
		return nil
	}
	if batchSize <= 0 {
		batchSize = defaultInsertBatchSize
	}

	batch := make([]*duckdb.LogRecord, 0, batchSize)
	batchMaxSeq := uint64(0)
	replayed := 0

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := store.InsertLogBatch(batch); err != nil {
			return err
		}
		if batchMaxSeq > 0 {
			if err := j.Commit(batchMaxSeq); err != nil {
				return err
			}
		}
		replayed += len(batch)
		batch = make([]*duckdb.LogRecord, 0, batchSize)
		batchMaxSeq = 0
		return nil
	}

	if err := j.Replay(func(seq uint64, record *model.LogRecord) error {
		copied := *record
		batch = append(batch, &copied)
		if seq > batchMaxSeq {
			batchMaxSeq = seq
		}
		if len(batch) >= batchSize {
			return flush()
		}
		return nil
	}); err != nil {
		return err
	}

	if err := flush(); err != nil {
		return err
	}
	if replayed > 0 {
		log.Printf("ingest journal: replayed %d uncommitted records", replayed)
	}
	return nil
}

func printStartupBanner(cfg appConfig, _ bool, processorName string) {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	cyan := lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	bold := lipgloss.NewStyle().Bold(true)

	check := green.Render("●")
	dot := dim.Render("●")

	logo := cyan.Bold(true).Render(`
    ╦  ╔═╗╔╦╗╦ ╦╔═╗
    ║  ║ ║ ║ ║ ║╚═╗
    ╩═╝╚═╝ ╩ ╚═╝╚═╝`)

	ver := dim.Render("v" + version)

	var lines []string
	lines = append(lines, "")
	lines = append(lines, logo)
	lines = append(lines, "    "+ver)
	lines = append(lines, "")

	separator := dim.Render("    ─────────────────────────────────")
	lines = append(lines, separator)
	lines = append(lines, "")

	// Gateway
	lines = append(lines, bold.Render("    Gateway"))
	lines = append(lines, "")

	if cfg.APIEnabled {
		lines = append(lines, fmt.Sprintf("    %s  HTTP API       %s", check, cyan.Render(cfg.APIAddr)))
	} else {
		lines = append(lines, fmt.Sprintf("    %s  HTTP API       %s", dot, dim.Render("disabled")))
	}

	if cfg.GRPCEnabled {
		lines = append(lines, fmt.Sprintf("    %s  OTLP/gRPC      %s", check, cyan.Render(cfg.GRPCAddr)))
	} else {
		lines = append(lines, fmt.Sprintf("    %s  OTLP/gRPC      %s", dot, dim.Render("disabled")))
	}

	lines = append(lines, fmt.Sprintf("    %s  Unix Socket    %s", check, cyan.Render(shortenPath(cfg.SocketPath))))
	lines = append(lines, "")

	// Storage
	lines = append(lines, bold.Render("    Storage"))
	lines = append(lines, "")

	lines = append(lines, fmt.Sprintf("    %s  Storage        %s", check, dim.Render(shortenPath(cfg.DBPath))))
	if cfg.BackupEnabled {
		lines = append(lines, fmt.Sprintf("    %s  Snapshots      %s", check, dim.Render(shortenPath(cfg.BackupLocalDir))))
	} else {
		lines = append(lines, fmt.Sprintf("    %s  Snapshots      %s", dot, dim.Render("disabled")))
	}

	lines = append(lines, "")

	// Runtime
	lines = append(lines, bold.Render("    Runtime"))
	lines = append(lines, "")

	lines = append(lines, fmt.Sprintf("    %s  Processor      %s", check, dim.Render(processorName)))

	lines = append(lines, "")
	lines = append(lines, bold.Render("    Config"))
	lines = append(lines, "")
	if cfg.ConfigPath != "" {
		lines = append(lines, fmt.Sprintf("    %s  Config File    %s", check, dim.Render(shortenPath(cfg.ConfigPath))))
	} else {
		lines = append(lines, fmt.Sprintf("    %s  Config File    %s", dot, dim.Render("default (no file)")))
	}

	lines = append(lines, "")
	lines = append(lines, separator)
	lines = append(lines, "")
	lines = append(lines, "    "+dim.Render("Press ")+yellow.Render("Ctrl+C")+dim.Render(" to stop"))
	lines = append(lines, "")

	fmt.Println(strings.Join(lines, "\n"))
}

func shortenPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
