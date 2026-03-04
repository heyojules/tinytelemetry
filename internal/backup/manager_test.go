package backup

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeSnapshotter struct {
	dbPath string
	data   []byte
}

func (f *fakeSnapshotter) DBPath() string { return f.dbPath }

func (f *fakeSnapshotter) SnapshotTo(dstPath string) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(dstPath, f.data, 0644)
}

func TestNewManager_Disabled(t *testing.T) {
	t.Parallel()

	m, err := NewManager(&fakeSnapshotter{dbPath: "/tmp/lotus.duckdb", data: []byte("x")}, Config{})
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}
	if m != nil {
		t.Fatal("expected nil manager when disabled")
	}
}

func TestNewManager_EnabledRequiresDBPath(t *testing.T) {
	t.Parallel()

	_, err := NewManager(&fakeSnapshotter{dbPath: "", data: []byte("x")}, Config{
		Enabled:  true,
		LocalDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for empty db path")
	}
}

func TestRunOnce_CreatesAndPrunesLocalBackups(t *testing.T) {
	t.Parallel()

	localDir := t.TempDir()
	store := &fakeSnapshotter{
		dbPath: "/tmp/lotus.duckdb",
		data:   []byte("snapshot"),
	}

	m := &Manager{
		store: store,
		cfg: Config{
			Enabled:  true,
			LocalDir: localDir,
			KeepLast: 2,
		},
	}

	if err := m.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce #1: %v", err)
	}
	if err := m.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce #2: %v", err)
	}
	if err := m.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce #3: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(localDir, "lotus-*.duckdb"))
	if err != nil {
		t.Fatalf("glob backups: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("backup files = %d, want 2", len(files))
	}
}

type blockingUploader struct {
	started chan struct{}
	once    sync.Once
}

func (u *blockingUploader) UploadFile(ctx context.Context, _ string) error {
	u.once.Do(func() { close(u.started) })
	<-ctx.Done()
	return ctx.Err()
}

func TestStop_CancelsInFlightUpload(t *testing.T) {
	t.Parallel()

	localDir := t.TempDir()
	uploader := &blockingUploader{started: make(chan struct{})}
	m := &Manager{
		store: &fakeSnapshotter{
			dbPath: "/tmp/lotus.duckdb",
			data:   []byte("snapshot"),
		},
		cfg: Config{
			Enabled:  true,
			Interval: 5 * time.Millisecond,
			LocalDir: localDir,
			KeepLast: 2,
		},
		uploader: uploader,
		done:     make(chan struct{}),
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())

	m.wg.Add(1)
	go m.loop()

	select {
	case <-uploader.started:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for upload to start")
	}

	done := make(chan struct{})
	go func() {
		m.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not return; upload likely not canceled")
	}
}
