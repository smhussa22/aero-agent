package ingest

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
)

type fakePublisher struct {
	mu   sync.Mutex
	keys []string
	vals [][]byte
	err  error
}

func (f *fakePublisher) Publish(_ context.Context, key, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.keys = append(f.keys, string(key))
	f.vals = append(f.vals, value)
	return nil
}

// fakeTokens knows two hosts; anything else is unknown
type fakeTokens struct{}

func (fakeTokens) HostForToken(_ context.Context, token string) (string, error) {
	switch token {
	case "tok-a":
		return "host-a", nil
	case "tok-b":
		return "host-b", nil
	case "broken":
		return "", errors.New("db down")
	}
	return "", ErrUnknownToken
}

// open starts an in-memory server behind the auth interceptor and opens a stream using token
func open(t *testing.T, pub Publisher, token string) aerov1.IngestService_StreamMetricsClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.StreamInterceptor(AuthInterceptor(fakeTokens{})))
	aerov1.RegisterIngestServiceServer(srv, NewServer(pub))
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	ctx := context.Background()
	if token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	}
	stream, err := aerov1.NewIngestServiceClient(conn).StreamMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func batch(host string) *aerov1.MetricBatch {
	return &aerov1.MetricBatch{
		HostId:      host,
		CollectedAt: timestamppb.Now(),
		Cpu:         &aerov1.CpuMetrics{UsagePercent: 42},
	}
}

func TestStreamPublishesAndAcks(t *testing.T) {
	pub := &fakePublisher{}
	stream := open(t, pub, "tok-a")
	for range 2 {
		if err := stream.Send(batch("host-a")); err != nil {
			t.Fatal(err)
		}
	}
	ack, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatal(err)
	}

	if ack.Accepted != 2 || len(pub.keys) != 2 || pub.keys[0] != "host-a" {
		t.Fatalf("ack=%d keys=%v", ack.Accepted, pub.keys)
	}
	var got aerov1.MetricBatch
	if err := proto.Unmarshal(pub.vals[0], &got); err != nil || got.Cpu.UsagePercent != 42 {
		t.Fatalf("published payload wrong: %v %v", err, &got)
	}
}

func TestAuthRejected(t *testing.T) {
	for name, tc := range map[string]struct {
		token string
		want  codes.Code
	}{
		"missing token": {"", codes.Unauthenticated},
		"wrong token":   {"nope", codes.Unauthenticated},
		"backend error": {"broken", codes.Unavailable},
	} {
		pub := &fakePublisher{}
		stream := open(t, pub, tc.token)
		stream.Send(batch("host-a"))
		_, err := stream.CloseAndRecv()
		if status.Code(err) != tc.want || len(pub.keys) != 0 {
			t.Errorf("%s: code=%v published=%d", name, status.Code(err), len(pub.keys))
		}
	}
}

func TestTokenCannotSpeakForAnotherHost(t *testing.T) {
	pub := &fakePublisher{}
	stream := open(t, pub, "tok-a")
	stream.Send(batch("host-b"))
	_, err := stream.CloseAndRecv()
	if status.Code(err) != codes.PermissionDenied || len(pub.keys) != 0 {
		t.Fatalf("code=%v published=%d", status.Code(err), len(pub.keys))
	}
}

func TestInvalidBatchRejected(t *testing.T) {
	for name, b := range map[string]*aerov1.MetricBatch{
		"no host":      {CollectedAt: timestamppb.Now()},
		"no timestamp": {HostId: "host-a"},
	} {
		pub := &fakePublisher{}
		stream := open(t, pub, "tok-a")
		stream.Send(b)
		_, err := stream.CloseAndRecv()
		if status.Code(err) != codes.InvalidArgument || len(pub.keys) != 0 {
			t.Errorf("%s: code=%v published=%d", name, status.Code(err), len(pub.keys))
		}
	}
}

func TestRateLimited(t *testing.T) {
	pub := &fakePublisher{}
	stream := open(t, pub, "tok-a")
	for range 100 {
		if stream.Send(batch("host-a")) != nil {
			break // server already closed the stream
		}
	}
	_, err := stream.CloseAndRecv()
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("code=%v", status.Code(err))
	}
	if len(pub.keys) > batchBurst+2 {
		t.Fatalf("published %d, burst is %d", len(pub.keys), batchBurst)
	}
}

func TestPublishFailureIsUnavailable(t *testing.T) {
	stream := open(t, &fakePublisher{err: errors.New("kafka down")}, "tok-a")
	stream.Send(batch("host-a"))
	_, err := stream.CloseAndRecv()
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("code=%v", status.Code(err))
	}
}
