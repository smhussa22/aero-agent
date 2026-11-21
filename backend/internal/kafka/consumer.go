package kafka

import (
	"context"

	kafkago "github.com/segmentio/kafka-go"
)

type Consumer struct {
	r *kafkago.Reader
}

func NewConsumer(brokers []string, topic, group string) *Consumer {
	return &Consumer{r: kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:     brokers,
		Topic:       topic,
		GroupID:     group,
		StartOffset: kafkago.FirstOffset, // a new group replays from the start
	})}
}

// Run calls handle for each message and commits its offset only after handle succeeds,
// so a crash re-delivers instead of losing data. It returns nil when ctx is cancelled.
func (c *Consumer) Run(ctx context.Context, handle func(ctx context.Context, value []byte) error) error {
	for {
		msg, err := c.r.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err := handle(ctx, msg.Value); err != nil {
			return err
		}
		if err := c.r.CommitMessages(ctx, msg); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

func (c *Consumer) Close() error {
	return c.r.Close()
}
