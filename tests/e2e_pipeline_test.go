package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/tinytelemetry/lotus/internal/duckdb"
	"github.com/tinytelemetry/lotus/internal/httpserver"
	"github.com/tinytelemetry/lotus/internal/model"
	"github.com/tinytelemetry/lotus/internal/otlpreceiver"
	"github.com/tinytelemetry/lotus/internal/socketrpc"
)

type e2eConfig struct {
	MaxConcurrentQueries int
	InsertBatchSize      int
	InsertFlushInterval  time.Duration
	InsertFlushQueueSize int
}

type e2eStack struct {
	store    *duckdb.Store
	insert   *duckdb.InsertBuffer
	api      *httpserver.Server
	socket   *socketrpc.Server
	otlp     *otlpreceiver.Server
	apiAddr  string
	grpcAddr string
	sock     string

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func startE2EStack(t *testing.T, cfg e2eConfig) *e2eStack {
	t.Helper()

	if cfg.MaxConcurrentQueries <= 0 {
		cfg.MaxConcurrentQueries = 16
	}
	if cfg.InsertBatchSize <= 0 {
		cfg.InsertBatchSize = 512
	}
	if cfg.InsertFlushInterval <= 0 {
		cfg.InsertFlushInterval = 20 * time.Millisecond
	}
	if cfg.InsertFlushQueueSize <= 0 {
		cfg.InsertFlushQueueSize = 128
	}

	dbPath := filepath.Join(t.TempDir(), "lotus-e2e.duckdb")
	store, err := duckdb.NewStore(dbPath, 5*time.Second)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	store.SetMaxConcurrentQueries(cfg.MaxConcurrentQueries)

	insert := duckdb.NewInsertBuffer(store, duckdb.InsertBufferConfig{
		BatchSize:      cfg.InsertBatchSize,
		FlushInterval:  cfg.InsertFlushInterval,
		FlushQueueSize: cfg.InsertFlushQueueSize,
	})

	api := httpserver.NewServer("127.0.0.1:0", store)
	if err := api.Start(); err != nil {
		t.Fatalf("http Start: %v", err)
	}

	sock := filepath.Join(os.TempDir(), fmt.Sprintf("lotus-e2e-%d.sock", time.Now().UnixNano()))
	socket := socketrpc.NewServer(sock, store)
	if err := socket.Start(); err != nil {
		t.Fatalf("socket Start: %v", err)
	}

	otlpServer := otlpreceiver.NewServer("127.0.0.1:0", insert)
	if err := otlpServer.Start(); err != nil {
		t.Fatalf("otlp Start: %v", err)
	}

	_, cancel := context.WithCancel(context.Background())
	stack := &e2eStack{
		store:    store,
		insert:   insert,
		api:      api,
		socket:   socket,
		otlp:     otlpServer,
		apiAddr:  api.Addr(),
		grpcAddr: otlpServer.Addr(),
		sock:     sock,
		cancel:   cancel,
	}

	waitEventually(t, 3*time.Second, 20*time.Millisecond, func() bool {
		url := "http://" + stack.apiAddr + "/api/health"
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return false
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, "api health endpoint did not become ready")

	waitEventually(t, 3*time.Second, 20*time.Millisecond, func() bool {
		c, err := socketrpc.Dial(stack.sock)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, "socket endpoint did not become ready")

	t.Cleanup(func() {
		stack.cancel()
		stack.otlp.Stop()
		stack.wg.Wait()
		stack.insert.Stop()
		stack.socket.Stop()
		_ = stack.api.Stop()
		_ = stack.store.Close()
	})

	return stack
}

func waitEventually(t *testing.T, timeout, interval time.Duration, condition func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if condition() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("eventually timeout: %s", msg)
		}
		time.Sleep(interval)
	}
}

func sendOTLPLogs(t *testing.T, addr string, req *collogspb.ExportLogsServiceRequest) {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc dial %s: %v", addr, err)
	}
	defer conn.Close()

	client := collogspb.NewLogsServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Export(ctx, req); err != nil {
		t.Fatalf("Export: %v", err)
	}
}

func buildOTLPLogRequest(records []otlpLogEntry) *collogspb.ExportLogsServiceRequest {
	// Group records by service+app for resource grouping
	logRecords := make([]*logspb.LogRecord, 0, len(records))
	var resourceAttrs []*commonpb.KeyValue

	if len(records) > 0 {
		r := records[0]
		resourceAttrs = []*commonpb.KeyValue{
			{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: r.Service}}},
			{Key: "host.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: r.Hostname}}},
			{Key: "app", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: r.App}}},
		}
	}

	for _, r := range records {
		lr := &logspb.LogRecord{
			TimeUnixNano: r.TimeUnixNano,
			SeverityText: r.SeverityText,
			Body:         &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: r.Body}},
			Attributes: []*commonpb.KeyValue{
				{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: r.Service}}},
				{Key: "host.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: r.Hostname}}},
				{Key: "app", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: r.App}}},
			},
		}
		logRecords = append(logRecords, lr)
	}

	return &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{
			{
				Resource: &resourcepb.Resource{Attributes: resourceAttrs},
				ScopeLogs: []*logspb.ScopeLogs{
					{LogRecords: logRecords},
				},
			},
		},
	}
}

type otlpLogEntry struct {
	TimeUnixNano uint64
	SeverityText string
	Body         string
	Service      string
	Hostname     string
	App          string
}

func generateOTLPBurst(n int, app, service string) *collogspb.ExportLogsServiceRequest {
	logRecords := make([]*logspb.LogRecord, 0, n)
	for i := 0; i < n; i++ {
		lr := &logspb.LogRecord{
			TimeUnixNano: 1761268800000000000,
			SeverityText: "Info",
			Body:         &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: fmt.Sprintf("burst-%d", i)}},
			Attributes: []*commonpb.KeyValue{
				{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: service}}},
				{Key: "app", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: app}}},
				{Key: "host.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "load-host"}}},
			},
		}
		logRecords = append(logRecords, lr)
	}

	return &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: service}}},
					},
				},
				ScopeLogs: []*logspb.ScopeLogs{
					{LogRecords: logRecords},
				},
			},
		},
	}
}

func waitForLogCount(t *testing.T, store *duckdb.Store, expected int64, timeout time.Duration) {
	t.Helper()
	waitEventually(t, timeout, 20*time.Millisecond, func() bool {
		got, err := store.TotalLogCount(model.QueryOpts{})
		return err == nil && got == expected
	}, fmt.Sprintf("expected log count %d", expected))
}

type sqlResponse struct {
	Columns  []string                 `json:"columns"`
	Rows     []map[string]interface{} `json:"rows"`
	RowCount int                      `json:"row_count"`
}

func postSQL(addr, sql string) (int, sqlResponse, error) {
	var out sqlResponse
	body, err := json.Marshal(map[string]string{"sql": sql})
	if err != nil {
		return 0, out, err
	}
	url := "http://" + addr + "/api/query"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, out, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, out, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, out, err
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, out, nil
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return resp.StatusCode, out, err
	}
	return resp.StatusCode, out, nil
}

func rowsToAppCount(t *testing.T, rows []map[string]interface{}) map[string]int64 {
	t.Helper()
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		app, ok := row["app"].(string)
		if !ok {
			t.Fatalf("row missing app string: %#v", row)
		}
		switch v := row["c"].(type) {
		case float64:
			out[app] = int64(v)
		case int64:
			out[app] = v
		default:
			t.Fatalf("unexpected count type %T in row %#v", v, row)
		}
	}
	return out
}

func containsDimension(items []model.DimensionCount, want string) bool {
	for _, item := range items {
		if item.Value == want {
			return true
		}
	}
	return false
}

func TestE2E_Pipeline_OTLPToHTTPAndSocket(t *testing.T) {
	stack := startE2EStack(t, e2eConfig{})

	entries := []otlpLogEntry{
		{TimeUnixNano: 1761238800000000000, SeverityText: "Info", Body: "payment created", Service: "billing-api", Hostname: "h1", App: "payments"},
		{TimeUnixNano: 1761238801000000000, SeverityText: "Warn", Body: "retrying webhook", Service: "billing-api", Hostname: "h1", App: "payments"},
		{TimeUnixNano: 1761238802000000000, SeverityText: "Error", Body: "search timeout", Service: "search-api", Hostname: "h2", App: "search"},
	}

	// Build separate requests per resource (service+app combo)
	billingEntries := entries[:2]
	searchEntries := entries[2:]

	sendOTLPLogs(t, stack.grpcAddr, buildOTLPLogRequest(billingEntries))
	sendOTLPLogs(t, stack.grpcAddr, buildOTLPLogRequest(searchEntries))

	waitForLogCount(t, stack.store, int64(len(entries)), 8*time.Second)

	client, err := socketrpc.Dial(stack.sock)
	if err != nil {
		t.Fatalf("socket dial: %v", err)
	}
	defer client.Close()

	count, err := client.TotalLogCount(model.QueryOpts{})
	if err != nil {
		t.Fatalf("TotalLogCount: %v", err)
	}
	if count != int64(len(entries)) {
		t.Fatalf("TotalLogCount=%d want=%d", count, len(entries))
	}

	apps, err := client.ListApps()
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	sort.Strings(apps)
	gotApps := strings.Join(apps, ",")
	for _, required := range []string{"payments", "search"} {
		if !strings.Contains(gotApps, required) {
			t.Fatalf("apps missing %q in %v", required, apps)
		}
	}

	services, err := client.TopServices(10, model.QueryOpts{})
	if err != nil {
		t.Fatalf("TopServices: %v", err)
	}
	if !containsDimension(services, "billing-api") || !containsDimension(services, "search-api") {
		t.Fatalf("unexpected services: %+v", services)
	}

	code, resp, err := postSQL(stack.apiAddr, "SELECT app, COUNT(*) AS c FROM logs GROUP BY app ORDER BY app")
	if err != nil {
		t.Fatalf("postSQL: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("postSQL status=%d", code)
	}
	gotCounts := rowsToAppCount(t, resp.Rows)
	want := map[string]int64{"payments": 2, "search": 1}
	if len(gotCounts) != len(want) {
		t.Fatalf("app count rows=%v want=%v", gotCounts, want)
	}
	for app, c := range want {
		if gotCounts[app] != c {
			t.Fatalf("app=%s count=%d want=%d (all=%v)", app, gotCounts[app], c, gotCounts)
		}
	}
}

func TestE2E_BurstIngest_NoLoss(t *testing.T) {
	stack := startE2EStack(t, e2eConfig{
		InsertBatchSize:      1000,
		InsertFlushInterval:  15 * time.Millisecond,
		InsertFlushQueueSize: 256,
		MaxConcurrentQueries: 32,
	})

	const total = 12000
	req := generateOTLPBurst(total, "load", "load-svc")
	sendOTLPLogs(t, stack.grpcAddr, req)

	waitForLogCount(t, stack.store, total, 20*time.Second)

	code, resp, err := postSQL(stack.apiAddr, "SELECT COUNT(*) AS c FROM logs")
	if err != nil {
		t.Fatalf("postSQL: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("count status=%d", code)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("unexpected rows: %+v", resp.Rows)
	}
	c, ok := resp.Rows[0]["c"].(float64)
	if !ok {
		t.Fatalf("unexpected count type: %#v", resp.Rows[0]["c"])
	}
	if int64(c) != total {
		t.Fatalf("final count=%d want=%d", int64(c), total)
	}
}

func TestE2E_ConcurrentReadsDuringIngest(t *testing.T) {
	stack := startE2EStack(t, e2eConfig{
		MaxConcurrentQueries: 64,
	})

	const total = 6000
	req := generateOTLPBurst(total, "concurrency", "query-svc")

	var wg sync.WaitGroup
	errCh := make(chan error, 128)

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client, err := socketrpc.Dial(stack.sock)
			if err != nil {
				errCh <- fmt.Errorf("socket dial: %w", err)
				return
			}
			defer client.Close()
			for j := 0; j < 120; j++ {
				if _, err := client.TotalLogCount(model.QueryOpts{}); err != nil {
					errCh <- fmt.Errorf("socket count: %w", err)
					return
				}
				if _, err := client.TopServices(5, model.QueryOpts{}); err != nil {
					errCh <- fmt.Errorf("socket services: %w", err)
					return
				}
			}
		}()
	}

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 120; j++ {
				code, _, err := postSQL(stack.apiAddr, "SELECT COUNT(*) AS c FROM logs")
				if err != nil {
					errCh <- fmt.Errorf("http query error: %w", err)
					return
				}
				if code != http.StatusOK {
					errCh <- fmt.Errorf("http status=%d", code)
					return
				}
			}
		}()
	}

	sendOTLPLogs(t, stack.grpcAddr, req)
	waitForLogCount(t, stack.store, total, 20*time.Second)

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent read failure: %v", err)
		}
	}
}
