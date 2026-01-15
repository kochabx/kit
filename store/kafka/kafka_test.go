package kafka

import (
	"context"
	"fmt"
	"testing"

	"github.com/segmentio/kafka-go"
)

func TestProducer(t *testing.T) {
	k, err := New(&Config{
		Brokers:                []string{"127.0.0.1:9092"},
		AllowAutoTopicCreation: true})
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	producer := k.Producer("test")
	err = producer.WriteMessages(context.Background(),
		kafka.Message{
			Key:   []byte("Key-A"),
			Value: []byte("Hello World!"),
		},
		kafka.Message{
			Key:   []byte("Key-B"),
			Value: []byte("One!"),
		},
		kafka.Message{
			Key:   []byte("Key-C"),
			Value: []byte("Two!"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestConsumer(t *testing.T) {
	k, err := New(&Config{
		Brokers: []string{"127.0.0.1:9092"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()

	consumer := k.Consumer("test")

	for {
		m, err := consumer.ReadMessage(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("message at topic/partition/offset %v/%v/%v: %s = %s\n",
			m.Topic, m.Partition, m.Offset, string(m.Key), string(m.Value))
	}
}

func TestConsumerGroup(t *testing.T) {
	k, err := New(&Config{
		Brokers: []string{"127.0.0.1:9092"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()

	consumerGroup := k.ConsumerGroup("test", "test")

	for {
		m, err := consumerGroup.ReadMessage(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("message at topic/partition/offset %v/%v/%v: %s = %s\n",
			m.Topic, m.Partition, m.Offset, string(m.Key), string(m.Value))
	}
}
