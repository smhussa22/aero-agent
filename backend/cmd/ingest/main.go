// Command ingest is the gRPC gateway agents stream metrics to; it forwards them to Kafka.
package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"google.golang.org/grpc"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
	"github.com/smhussa22/aero-agent/backend/internal/ingest"
	"github.com/smhussa22/aero-agent/backend/internal/kafka"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	listenAddr := env("LISTEN_ADDR", ":50051")
	brokers := strings.Split(env("KAFKA_BROKERS", "localhost:9092"), ",")
	topic := env("KAFKA_TOPIC", "metrics.raw")

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		slog.Error("listen failed", "addr", listenAddr, "err", err)
		os.Exit(1)
	}

	pub := kafka.NewPublisher(brokers, topic)
	defer pub.Close()

	// shortcut: no TLS or agent auth yet, add both before exposing this port publicly
	srv := grpc.NewServer()
	aerov1.RegisterIngestServiceServer(srv, ingest.NewServer(pub))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		srv.GracefulStop()
	}()

	slog.Info("ingest listening", "addr", listenAddr, "brokers", brokers, "topic", topic)
	if err := srv.Serve(lis); err != nil {
		slog.Error("serve failed", "err", err)
		os.Exit(1)
	}
}
