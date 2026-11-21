// Command metrics-writer consumes metric batches from Kafka and stores them in TimescaleDB.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
	"github.com/smhussa22/aero-agent/backend/internal/kafka"
	"github.com/smhussa22/aero-agent/backend/internal/store"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	brokers := strings.Split(env("KAFKA_BROKERS", "localhost:9092"), ",")
	topic := env("KAFKA_TOPIC", "metrics.raw")
	dbURL := env("DATABASE_URL", "postgres://aero:aero@localhost:5432/aero")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	consumer := kafka.NewConsumer(brokers, topic, "metrics-writer")
	defer consumer.Close()

	slog.Info("metrics-writer running", "brokers", brokers, "topic", topic)
	err = consumer.Run(ctx, func(ctx context.Context, value []byte) error {
		var batch aerov1.MetricBatch
		if err := proto.Unmarshal(value, &batch); err != nil {
			// a message that can never parse would block the partition forever, so drop it
			slog.Error("dropping unparseable message", "err", err)
			return nil
		}
		return store.InsertBatch(ctx, pool, &batch)
	})
	if err != nil {
		slog.Error("consumer stopped", "err", err)
		os.Exit(1)
	}
}
