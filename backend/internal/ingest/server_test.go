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

// start serves an in-memory gRPC server and returns a client for it
func start(t *testing.T, pub Publisher) aerov1.IngestServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
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
	return aerov1.NewIngestServiceClient(conn)
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
	stream, err := start(t, pub).StreamMetrics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
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

func TestInvalidBatchRejected(t *testing.T) {
	for name, b := range map[string]*aerov1.MetricBatch{
		"no host":      {CollectedAt: timestamppb.Now()},
		"no timestamp": {HostId: "h"},
	} {
		pub := &fakePublisher{}
		stream, _ := start(t, pub).StreamMetrics(context.Background())
		stream.Send(b)
		_, err := stream.CloseAndRecv()
		if status.Code(err) != codes.InvalidArgument || len(pub.keys) != 0 {
			t.Errorf("%s: code=%v published=%d", name, status.Code(err), len(pub.keys))
		}
	}
}

func TestPublishFailureIsUnavailable(t *testing.T) {
	stream, _ := start(t, &fakePublisher{err: errors.New("kafka down")}).StreamMetrics(context.Background())
	stream.Send(batch("host-a"))
	_, err := stream.CloseAndRecv()
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("code=%v", status.Code(err))
	}
}
