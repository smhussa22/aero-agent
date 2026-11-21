// Package kafka is the Kafka-backed ingest.Publisher.
package kafka

import (
	"context"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

const publishAttempts = 5

type Publisher struct {
	w *kafkago.Writer
}

func NewPublisher(brokers []string, topic string) *Publisher {
	return &Publisher{w: &kafkago.Writer{
		Addr:         kafkago.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafkago.Hash{}, // same host_id always lands on the same partition
		RequiredAcks: kafkago.RequireAll,
		BatchTimeout: 10 * time.Millisecond,
	}}
}

// Publish retries briefly so a leader election or broker restart does not fail the caller.
func (p *Publisher) Publish(ctx context.Context, key, value []byte) error {
	var err error
	for attempt := 1; attempt <= publishAttempts; attempt++ {
		if err = p.w.WriteMessages(ctx, kafkago.Message{Key: key, Value: value}); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
		}
	}
	return err
}

func (p *Publisher) Close() error {
	return p.w.Close()
}
