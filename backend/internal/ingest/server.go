// Package ingest receives agent metric streams and hands each batch to a Publisher.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
)

// limits applied at the trust boundary: agents run on user machines
const (
	maxHostIDLen    = 253
	maxListItems    = 256
	maxTopProcesses = 100

	// agents send about one batch per few seconds; this leaves room for bursts after a reconnect
	batchesPerSecond = 2
	batchBurst       = 10
)

// Publisher sends one serialized batch to the message bus, keyed so a host stays ordered.
type Publisher interface {
	Publish(ctx context.Context, key, value []byte) error
}

// Server implements aerov1.IngestServiceServer. Streams must pass AuthInterceptor first.
type Server struct {
	aerov1.UnimplementedIngestServiceServer
	pub Publisher

	mu       sync.Mutex
	limiters map[string]*rate.Limiter // per authenticated host, shared by all its streams
}

func NewServer(pub Publisher) *Server {
	return &Server{pub: pub, limiters: map[string]*rate.Limiter{}}
}

func (s *Server) limiter(host string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.limiters[host]
	if !ok {
		l = rate.NewLimiter(batchesPerSecond, batchBurst)
		s.limiters[host] = l
	}
	return l
}

func validate(b *aerov1.MetricBatch) error {
	switch {
	case b.GetHostId() == "":
		return errors.New("host_id is required")
	case len(b.GetHostId()) > maxHostIDLen:
		return fmt.Errorf("host_id longer than %d bytes", maxHostIDLen)
	case b.GetCollectedAt() == nil:
		return errors.New("collected_at is required")
	case len(b.GetDisks()) > maxListItems || len(b.GetInterfaces()) > maxListItems:
		return fmt.Errorf("more than %d disks or interfaces", maxListItems)
	case len(b.GetTopProcesses()) > maxTopProcesses:
		return fmt.Errorf("more than %d processes", maxTopProcesses)
	}
	return nil
}

// StreamMetrics publishes every batch on the stream and acks the count when the agent closes it.
func (s *Server) StreamMetrics(stream aerov1.IngestService_StreamMetricsServer) error {
	host := hostFrom(stream.Context())
	if host == "" {
		return status.Error(codes.Unauthenticated, "no authenticated host")
	}
	limiter := s.limiter(host)

	var accepted uint64
	for {
		batch, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&aerov1.IngestAck{Accepted: accepted})
		}
		if err != nil {
			return err
		}

		if !limiter.Allow() {
			return status.Error(codes.ResourceExhausted, "rate limit exceeded")
		}
		if err := validate(batch); err != nil {
			return status.Error(codes.InvalidArgument, err.Error())
		}
		// a token only speaks for its own host
		if batch.GetHostId() != host {
			return status.Error(codes.PermissionDenied, "host_id does not match token")
		}

		value, err := proto.Marshal(batch)
		if err != nil {
			return status.Error(codes.Internal, "marshal failed")
		}

		if err := s.pub.Publish(stream.Context(), []byte(host), value); err != nil {
			slog.Error("publish failed", "host", host, "err", err)
			return status.Error(codes.Unavailable, "publish failed")
		}
		accepted++
	}
}
