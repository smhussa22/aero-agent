// Package kafka is the Kafka-backed ingest.Publisher.
package kafka

import (
	"context"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

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
		// shortcut: topic auto-creation is for local dev, create it explicitly in deploy
		AllowAutoTopicCreation: true,
	}}
}

func (p *Publisher) Publish(ctx context.Context, key, value []byte) error {
	return p.w.WriteMessages(ctx, kafkago.Message{Key: key, Value: value})
}

func (p *Publisher) Close() error {
	return p.w.Close()
}
