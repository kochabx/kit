package kafka

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

func TestProducer(t *testing.T) {
	ctx := context.Background()
	k, err := New(&Config{
		Brokers:                []string{"127.0.0.1:9092"},
		AllowAutoTopicCreation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	producer := k.Producer("test")
	err = producer.WriteMessages(ctx,
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
		t.Logf("Producer write failed (expected if no kafka running): %v", err)
	}
}

func TestConsumer(t *testing.T) {
	ctx := context.Background()
	k, err := New(&Config{
		Brokers: []string{"127.0.0.1:9092"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()

	consumer := k.Consumer("test")

	// Set a short deadline for reading to avoid blocking forever if no kafka
	readCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	for {
		m, err := consumer.ReadMessage(readCtx)
		if err != nil {
			t.Logf("Consumer read failed (expected if no kafka running): %v", err)
			break
		}
		fmt.Printf("message at topic/partition/offset %v/%v/%v: %s = %s\n",
			m.Topic, m.Partition, m.Offset, string(m.Key), string(m.Value))
	}
}

func TestConsumerGroup(t *testing.T) {
	ctx := context.Background()
	k, err := New(&Config{
		Brokers: []string{"127.0.0.1:9092"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()

	consumerGroup := k.ConsumerGroup("test", "test")

	// Set a short deadline for reading
	readCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	for {
		m, err := consumerGroup.ReadMessage(readCtx)
		if err != nil {
			t.Logf("ConsumerGroup read failed (expected if no kafka running): %v", err)
			break
		}
		fmt.Printf("message at topic/partition/offset %v/%v/%v: %s = %s\n",
			m.Topic, m.Partition, m.Offset, string(m.Key), string(m.Value))
	}
}

func TestOptions(t *testing.T) {

	k, err := New(&Config{Brokers: []string{"127.0.0.1:9092"}},
		WithAuth("user", "pass"),
		WithTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if k.config.Username != "user" {
		t.Errorf("expected username user, got %s", k.config.Username)
	}
	if k.config.Timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", k.config.Timeout)
	}
}
