package kafka

import (
	"errors"
	"net"
	"strconv"

	kafkago "github.com/segmentio/kafka-go"
)

// shortcut: replication factor 1 suits a single broker; raise it through deploy config for a real cluster
const (
	topicPartitions        = 3
	topicReplicationFactor = 1
)

// EnsureTopic creates the topic if it is missing. It is safe to call from every service at startup.
func EnsureTopic(brokers []string, topic string) error {
	conn, err := kafkago.Dial("tcp", brokers[0])
	if err != nil {
		return err
	}
	defer conn.Close()

	// topic creation must go to the controller broker
	controller, err := conn.Controller()
	if err != nil {
		return err
	}
	ctrl, err := kafkago.Dial("tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		return err
	}
	defer ctrl.Close()

	err = ctrl.CreateTopics(kafkago.TopicConfig{Topic: topic, NumPartitions: topicPartitions, ReplicationFactor: topicReplicationFactor})
	if err != nil && !errors.Is(err, kafkago.TopicAlreadyExists) {
		return err
	}
	return nil
}
