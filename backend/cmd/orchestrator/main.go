// Command orchestrator turns alerts into incidents, hands them to the remediation agent on
// the affected host, and relays human approvals back.
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
	"github.com/smhussa22/aero-agent/backend/internal/orchestrator"
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
	listenAddr := env("ORCH_ADDR", ":50052")
	brokers := strings.Split(env("KAFKA_BROKERS", "localhost:9092"), ",")
	alertsTopic := env("ALERTS_TOPIC", "alerts")
	dbURL := env("DATABASE_URL", "postgres://aero:aero@localhost:5432/aero")

	// fail closed, same rule as the ingest gateway
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

	if err := kafka.EnsureTopic(brokers, alertsTopic); err != nil {
		fatal("ensure topic", err)
	}

	svc := orchestrator.NewService(pool)
	opts = append(opts, grpc.StreamInterceptor(ingest.AuthInterceptor(store.TokenStore{Pool: pool})))
	srv := grpc.NewServer(opts...)
	aerov1.RegisterRemediationServiceServer(srv, svc)

	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		fatal("listen", err)
	}

	go svc.Listen(ctx)
	go func() {
		consumer := kafka.NewConsumer(brokers, alertsTopic, "orchestrator")
		defer consumer.Close()
		if err := consumer.Run(ctx, svc.HandleAlert); err != nil {
			slog.Error("alert consumer stopped", "err", err)
			stop()
		}
	}()
	go func() {
		<-ctx.Done()
		srv.GracefulStop()
	}()

	slog.Info("orchestrator listening", "addr", listenAddr, "alerts", alertsTopic)
	if err := srv.Serve(lis); err != nil {
		fatal("serve", err)
	}
}
