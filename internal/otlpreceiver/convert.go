package otlpreceiver

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/tinytelemetry/lotus/internal/logparse"
	"github.com/tinytelemetry/lotus/internal/model"
)

// convertLogRecord converts an OTLP proto LogRecord into a model.LogRecord.
// inherited contains merged resource + scope attributes (resource < scope priority).
func convertLogRecord(lr *logspb.LogRecord, inherited map[string]string) *model.LogRecord {
	receiveTime := time.Now()

	attributes := cloneAttrs(inherited)
	mergeKeyValues(attributes, lr.GetAttributes())

	// Trace/span context
	if len(lr.TraceId) > 0 {
		attributes["trace.id"] = hex.EncodeToString(lr.TraceId)
	}
	if len(lr.SpanId) > 0 {
		attributes["span.id"] = hex.EncodeToString(lr.SpanId)
	}
	if lr.Flags != 0 {
		attributes["trace.flags"] = fmt.Sprintf("%d", lr.Flags)
	}
	if lr.DroppedAttributesCount > 0 {
		attributes["otel.dropped_attributes_count"] = fmt.Sprintf("%d", lr.DroppedAttributesCount)
	}

	message := anyValueToString(lr.GetBody())

	rawLine := ""
	if b, err := protojson.Marshal(lr); err == nil {
		rawLine = string(b)
	}
	if message == "" {
		message = rawLine
	}
	message = sanitizeMessage(message)

	severityNumber := int(lr.SeverityNumber)
	severity := lr.SeverityText
	if severity == "" && severityNumber > 0 {
		severity = severityFromNumber(severityNumber)
	}
	if severity == "" {
		severity = "INFO"
	}
	normalizedSeverity := logparse.NormalizeSeverity(severity)
	if severityNumber == 0 {
		severityNumber = defaultSeverityNumber(normalizedSeverity)
	}

	var origTimestamp time.Time
	if lr.TimeUnixNano > 0 {
		origTimestamp = time.Unix(0, int64(lr.TimeUnixNano))
	} else if lr.ObservedTimeUnixNano > 0 {
		origTimestamp = time.Unix(0, int64(lr.ObservedTimeUnixNano))
	}

	app := extractApp(attributes)
	if app == "" {
		app = "default"
	}

	return &model.LogRecord{
		Timestamp:     receiveTime,
		OrigTimestamp: origTimestamp,
		Level:         normalizedSeverity,
		LevelNum:      severityNumber,
		Message:       message,
		RawLine:       rawLine,
		Attributes:    attributes,
		Source:        "otlp",
		App:           app,
		Service:       extractService(attributes),
		Hostname:      extractHostname(attributes),
	}
}

// extractResourceAttrs extracts attributes from a Resource proto.
func extractResourceAttrs(resource *resourcepb.Resource) map[string]string {
	if resource == nil {
		return map[string]string{}
	}
	attrs := make(map[string]string, len(resource.Attributes))
	for _, kv := range resource.Attributes {
		if kv.Key == "" {
			continue
		}
		if v := anyValueToString(kv.Value); v != "" {
			attrs[kv.Key] = v
		}
	}
	return attrs
}

// mergeKeyValues merges proto KeyValue pairs into the dst map.
func mergeKeyValues(dst map[string]string, kvs []*commonpb.KeyValue) {
	for _, kv := range kvs {
		if kv.Key == "" {
			continue
		}
		if v := anyValueToString(kv.Value); v != "" {
			dst[kv.Key] = v
		}
	}
}

// anyValueToString converts an OTLP AnyValue to a string representation.
func anyValueToString(av *commonpb.AnyValue) string {
	if av == nil {
		return ""
	}
	switch v := av.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return v.StringValue
	case *commonpb.AnyValue_BoolValue:
		return fmt.Sprintf("%v", v.BoolValue)
	case *commonpb.AnyValue_IntValue:
		return fmt.Sprintf("%d", v.IntValue)
	case *commonpb.AnyValue_DoubleValue:
		return fmt.Sprintf("%v", v.DoubleValue)
	case *commonpb.AnyValue_BytesValue:
		return hex.EncodeToString(v.BytesValue)
	case *commonpb.AnyValue_ArrayValue:
		if v.ArrayValue == nil {
			return ""
		}
		parts := make([]string, 0, len(v.ArrayValue.Values))
		for _, val := range v.ArrayValue.Values {
			if s := anyValueToString(val); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ",")
	case *commonpb.AnyValue_KvlistValue:
		if v.KvlistValue == nil {
			return ""
		}
		if b, err := protojson.Marshal(v.KvlistValue); err == nil {
			return string(b)
		}
		return ""
	default:
		return ""
	}
}

func cloneAttrs(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func severityFromNumber(number int) string {
	switch {
	case number >= 1 && number <= 4:
		return "TRACE"
	case number >= 5 && number <= 8:
		return "DEBUG"
	case number >= 9 && number <= 12:
		return "INFO"
	case number >= 13 && number <= 16:
		return "WARN"
	case number >= 17 && number <= 20:
		return "ERROR"
	case number >= 21 && number <= 24:
		return "FATAL"
	default:
		return ""
	}
}

func defaultSeverityNumber(level string) int {
	switch logparse.NormalizeSeverity(level) {
	case "TRACE":
		return 1
	case "DEBUG":
		return 5
	case "INFO":
		return 9
	case "WARN":
		return 13
	case "ERROR":
		return 17
	case "FATAL":
		return 21
	default:
		return 9
	}
}

func extractApp(attributes map[string]string) string {
	for _, key := range []string{"app", "service.name", "service_name", "service", "name"} {
		if v := attributes[key]; v != "" {
			return v
		}
	}
	return ""
}

func extractService(attributes map[string]string) string {
	for _, key := range []string{"service.name", "service", "serviceName", "app", "name"} {
		if v := attributes[key]; v != "" {
			return v
		}
	}
	return "unknown"
}

func extractHostname(attributes map[string]string) string {
	for _, key := range []string{"host", "hostname", "host.name"} {
		if v := attributes[key]; v != "" {
			return v
		}
	}
	return ""
}

func sanitizeMessage(message string) string {
	clean := strings.ReplaceAll(message, "\t", " ")
	clean = strings.ReplaceAll(clean, "\n", " ")
	clean = strings.ReplaceAll(clean, "\r", " ")
	return clean
}
