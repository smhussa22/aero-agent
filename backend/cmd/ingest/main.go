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

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
	"github.com/smhussa22/aero-agent/backend/internal/ingest"
	"github.com/smhussa22/aero-agent/backend/internal/kafka"
	"github.com/smhussa22/aero-agent/backend/internal/store"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}

func main() {
	listenAddr := env("LISTEN_ADDR", ":50051")
	brokers := strings.Split(env("KAFKA_BROKERS", "localhost:9092"), ",")
	topic := env("KAFKA_TOPIC", "metrics.raw")
	dbURL := env("DATABASE_URL", "postgres://aero:aero@localhost:5432/aero")

	// fail closed: serving without TLS requires an explicit opt-in meant for local dev
	opts := []grpc.ServerOption{}
	certFile, keyFile := os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE")
	switch {
	case certFile != "" && keyFile != "":
		creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
		if err != nil {
			fatal("load tls cert", err)
		}
		opts = append(opts, grpc.Creds(creds))
	case os.Getenv("INSECURE_DEV") == "1":
		slog.Warn("serving WITHOUT TLS; tokens cross the network in cleartext")
	default:
		slog.Error("set TLS_CERT_FILE and TLS_KEY_FILE (or INSECURE_DEV=1 for local development)")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fatal("connect db", err)
	}
	defer pool.Close()

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		fatal("listen", err)
	}

	pub := kafka.NewPublisher(brokers, topic)
	defer pub.Close()

	opts = append(opts, grpc.StreamInterceptor(ingest.AuthInterceptor(store.TokenStore{Pool: pool})))
	srv := grpc.NewServer(opts...)
	aerov1.RegisterIngestServiceServer(srv, ingest.NewServer(pub))

	go func() {
		<-ctx.Done()
		srv.GracefulStop()
	}()

	slog.Info("ingest listening", "addr", listenAddr, "brokers", brokers, "topic", topic)
	if err := srv.Serve(lis); err != nil {
		fatal("serve", err)
	}
}
