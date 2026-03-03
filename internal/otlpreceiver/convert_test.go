package otlpreceiver

import (
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

func TestConvertLogRecord_BasicFields(t *testing.T) {
	t.Parallel()

	lr := &logspb.LogRecord{
		TimeUnixNano: 1700000000000000000,
		SeverityText: "ERROR",
		Body:         &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "something failed"}},
		Attributes: []*commonpb.KeyValue{
			{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "my-svc"}}},
			{Key: "host.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "host-1"}}},
			{Key: "app", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "my-app"}}},
		},
		TraceId: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		SpanId:  []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
	}

	record := convertLogRecord(lr, map[string]string{})

	if record.Level != "ERROR" {
		t.Fatalf("Level = %q, want ERROR", record.Level)
	}
	if record.LevelNum != 17 {
		t.Fatalf("LevelNum = %d, want 17", record.LevelNum)
	}
	if record.Message != "something failed" {
		t.Fatalf("Message = %q", record.Message)
	}
	if record.Source != "otlp" {
		t.Fatalf("Source = %q, want otlp", record.Source)
	}
	if record.App != "my-app" {
		t.Fatalf("App = %q, want my-app", record.App)
	}
	if record.Service != "my-svc" {
		t.Fatalf("Service = %q, want my-svc", record.Service)
	}
	if record.Hostname != "host-1" {
		t.Fatalf("Hostname = %q, want host-1", record.Hostname)
	}
	if record.Attributes["trace.id"] != "0102030405060708090a0b0c0d0e0f10" {
		t.Fatalf("trace.id = %q", record.Attributes["trace.id"])
	}
	if record.Attributes["span.id"] != "0102030405060708" {
		t.Fatalf("span.id = %q", record.Attributes["span.id"])
	}
	if record.OrigTimestamp.IsZero() {
		t.Fatal("OrigTimestamp should not be zero")
	}
}

func TestConvertLogRecord_SeverityNumberFallback(t *testing.T) {
	t.Parallel()

	lr := &logspb.LogRecord{
		SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_WARN,
		Body:           &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "warn msg"}},
	}

	record := convertLogRecord(lr, map[string]string{})

	if record.Level != "WARN" {
		t.Fatalf("Level = %q, want WARN", record.Level)
	}
	if record.LevelNum != 13 {
		t.Fatalf("LevelNum = %d, want 13", record.LevelNum)
	}
}

func TestConvertLogRecord_InheritedAttributes(t *testing.T) {
	t.Parallel()

	inherited := map[string]string{
		"service.name": "resource-svc",
		"env":          "prod",
	}

	lr := &logspb.LogRecord{
		SeverityText: "Info",
		Body:         &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "hello"}},
		Attributes: []*commonpb.KeyValue{
			{Key: "env", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "staging"}}},
		},
	}

	record := convertLogRecord(lr, inherited)

	// Log record attribute should override inherited
	if record.Attributes["env"] != "staging" {
		t.Fatalf("env = %q, want staging", record.Attributes["env"])
	}
	// Inherited should still be there
	if record.Service != "resource-svc" {
		t.Fatalf("Service = %q, want resource-svc", record.Service)
	}
}

func TestConvertLogRecord_DefaultApp(t *testing.T) {
	t.Parallel()

	lr := &logspb.LogRecord{
		SeverityText: "Info",
		Body:         &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "no app"}},
	}

	record := convertLogRecord(lr, map[string]string{})

	if record.App != "default" {
		t.Fatalf("App = %q, want default", record.App)
	}
}

func TestConvertLogRecord_ObservedTimeFallback(t *testing.T) {
	t.Parallel()

	lr := &logspb.LogRecord{
		ObservedTimeUnixNano: 1700000000000000000,
		SeverityText:         "Debug",
		Body:                 &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "observed"}},
	}

	record := convertLogRecord(lr, map[string]string{})

	if record.OrigTimestamp.IsZero() {
		t.Fatal("OrigTimestamp should fall back to ObservedTimeUnixNano")
	}
}

func TestAnyValueToString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		av   *commonpb.AnyValue
		want string
	}{
		{"nil", nil, ""},
		{"string", &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "hello"}}, "hello"},
		{"bool", &commonpb.AnyValue{Value: &commonpb.AnyValue_BoolValue{BoolValue: true}}, "true"},
		{"int", &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: 42}}, "42"},
		{"double", &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: 3.14}}, "3.14"},
		{"array", &commonpb.AnyValue{Value: &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{
			Values: []*commonpb.AnyValue{
				{Value: &commonpb.AnyValue_StringValue{StringValue: "a"}},
				{Value: &commonpb.AnyValue_StringValue{StringValue: "b"}},
			},
		}}}, "a,b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := anyValueToString(tt.av)
			if got != tt.want {
				t.Fatalf("anyValueToString = %q, want %q", got, tt.want)
			}
		})
	}
}
