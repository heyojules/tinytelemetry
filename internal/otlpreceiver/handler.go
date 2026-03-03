package otlpreceiver

import (
	"context"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"github.com/tinytelemetry/lotus/internal/model"
)

// RecordSink accepts processed log records.
type RecordSink interface {
	Add(*model.LogRecord)
}

// logsHandler implements the OTLP LogsService gRPC server.
type logsHandler struct {
	collogspb.UnimplementedLogsServiceServer
	sink RecordSink
}

// Export handles an incoming ExportLogsServiceRequest.
func (h *logsHandler) Export(_ context.Context, req *collogspb.ExportLogsServiceRequest) (*collogspb.ExportLogsServiceResponse, error) {
	for _, rl := range req.GetResourceLogs() {
		resourceAttrs := extractResourceAttrs(rl.GetResource())

		for _, sl := range rl.GetScopeLogs() {
			scopeAttrs := cloneAttrs(resourceAttrs)

			// Merge scope-level attributes
			if scope := sl.GetScope(); scope != nil {
				if scope.Name != "" {
					scopeAttrs["otel.scope.name"] = scope.Name
				}
				if scope.Version != "" {
					scopeAttrs["otel.scope.version"] = scope.Version
				}
				mergeKeyValues(scopeAttrs, scope.Attributes)
			}

			for _, lr := range sl.GetLogRecords() {
				record := convertLogRecord(lr, scopeAttrs)
				h.sink.Add(record)
			}
		}
	}

	return &collogspb.ExportLogsServiceResponse{}, nil
}
