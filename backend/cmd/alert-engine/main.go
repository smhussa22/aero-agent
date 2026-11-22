// Command alert-engine reads metric batches from Kafka, evaluates alert rules and
// publishes fire/resolve events to the alerts topic.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	aerov1 "github.com/smhussa22/aero-agent/backend/gen/aerov1"
	"github.com/smhussa22/aero-agent/backend/internal/alert"
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
	inTopic := env("KAFKA_TOPIC", "metrics.raw")
	outTopic := env("ALERTS_TOPIC", "alerts")
	dbURL := env("DATABASE_URL", "postgres://aero:aero@localhost:5432/aero")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	rules, err := alert.LoadRules(ctx, pool)
	if err != nil {
		slog.Error("load rules failed", "err", err)
		os.Exit(1)
	}
	eval := alert.NewEvaluator(rules)

	// rule edits are picked up within a minute; the evaluator is guarded because the
	// reload goroutine and the consumer both touch it
	var mu sync.Mutex
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
			}
			fresh, err := alert.LoadRules(ctx, pool)
			if err != nil {
				slog.Error("reload rules failed, keeping previous rules", "err", err)
				continue
			}
			mu.Lock()
			eval.SetRules(fresh)
			mu.Unlock()
		}
	}()

	for _, t := range []string{inTopic, outTopic} {
		if err := kafka.EnsureTopic(brokers, t); err != nil {
			slog.Error("ensure topic failed", "topic", t, "err", err)
			os.Exit(1)
		}
	}
	pub := kafka.NewPublisher(brokers, outTopic)
	defer pub.Close()
	consumer := kafka.NewConsumer(brokers, inTopic, "alert-engine")
	defer consumer.Close()

	slog.Info("alert-engine running", "rules", len(rules), "in", inTopic, "out", outTopic)
	err = consumer.Run(ctx, func(ctx context.Context, value []byte) error {
		var batch aerov1.MetricBatch
		if err := proto.Unmarshal(value, &batch); err != nil {
			slog.Error("dropping unparseable message", "err", err)
			return nil
		}

		mu.Lock()
		events := eval.Process(batch.GetOrgId(), batch.GetHostId(), batch.GetCollectedAt().AsTime(), store.Flatten(&batch))
		mu.Unlock()

		// a failed publish returns the error so the batch is redelivered; evaluator state has
		// already advanced, so a retried fire may be missed rather than duplicated
		for _, ev := range events {
			out, err := proto.Marshal(ev)
			if err != nil {
				return err
			}
			if err := pub.Publish(ctx, []byte(ev.HostId), out); err != nil {
				return err
			}
			slog.Info("alert", "host", ev.HostId, "metric", ev.Metric, "value", ev.Value, "resolved", ev.Resolved)
		}
		return nil
	})
	if err != nil {
		slog.Error("consumer stopped", "err", err)
		os.Exit(1)
	}
}
