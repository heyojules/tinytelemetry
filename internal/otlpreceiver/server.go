package otlpreceiver

import (
	"net"
	"sync"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/grpc"
)

// Server is an OTLP/gRPC log receiver.
type Server struct {
	addr     string
	sink     RecordSink
	grpc     *grpc.Server
	listener net.Listener
	stopOnce sync.Once
}

// NewServer creates a new OTLP/gRPC server.
func NewServer(addr string, sink RecordSink) *Server {
	return &Server{
		addr: addr,
		sink: sink,
	}
}

// Start begins listening and serving gRPC in a background goroutine.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = ln

	s.grpc = grpc.NewServer()
	collogspb.RegisterLogsServiceServer(s.grpc, &logsHandler{sink: s.sink})

	go s.grpc.Serve(ln)

	return nil
}

// Stop gracefully shuts down the gRPC server.
func (s *Server) Stop() {
	s.stopOnce.Do(func() {
		if s.grpc != nil {
			s.grpc.GracefulStop()
		}
	})
}

// Addr returns the actual listen address (useful when port 0 is used).
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.addr
}
