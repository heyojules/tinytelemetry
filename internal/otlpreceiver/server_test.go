package otlpreceiver

import (
	"context"
	"sync"
	"testing"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/tinytelemetry/lotus/internal/model"
)

type mockSink struct {
	mu      sync.Mutex
	records []*model.LogRecord
}

func (m *mockSink) Add(r *model.LogRecord) {
	m.mu.Lock()
	m.records = append(m.records, r)
	m.mu.Unlock()
}

func (m *mockSink) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.records)
}

func TestServer_RoundTrip(t *testing.T) {
	t.Parallel()

	sink := &mockSink{}
	srv := NewServer("127.0.0.1:0", sink)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	conn, err := grpc.NewClient(srv.Addr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc dial: %v", err)
	}
	defer conn.Close()

	client := collogspb.NewLogsServiceClient(conn)

	req := &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "test-svc"}}},
					},
				},
				ScopeLogs: []*logspb.ScopeLogs{
					{
						LogRecords: []*logspb.LogRecord{
							{
								TimeUnixNano: 1700000000000000000,
								SeverityText: "INFO",
								Body:         &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "hello from grpc"}},
							},
							{
								TimeUnixNano: 1700000001000000000,
								SeverityText: "ERROR",
								Body:         &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "error from grpc"}},
							},
						},
					},
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.Export(ctx, req)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	if got := sink.count(); got != 2 {
		t.Fatalf("sink has %d records, want 2", got)
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()

	r0 := sink.records[0]
	if r0.Message != "hello from grpc" {
		t.Fatalf("records[0].Message = %q", r0.Message)
	}
	if r0.Source != "otlp" {
		t.Fatalf("records[0].Source = %q, want otlp", r0.Source)
	}
	if r0.Service != "test-svc" {
		t.Fatalf("records[0].Service = %q, want test-svc", r0.Service)
	}

	r1 := sink.records[1]
	if r1.Level != "ERROR" {
		t.Fatalf("records[1].Level = %q, want ERROR", r1.Level)
	}
}

func TestServer_StopIsIdempotent(t *testing.T) {
	t.Parallel()

	sink := &mockSink{}
	srv := NewServer("127.0.0.1:0", sink)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	srv.Stop()
	srv.Stop() // should not panic
}
