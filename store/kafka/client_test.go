package kafka

import (
	"context"
	"crypto/tls"
	"os"
	"testing"
	"time"

	segmentio "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireKafkaIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("KIT_KAFKA_INTEGRATION") == "" {
		t.Skip("set KIT_KAFKA_INTEGRATION=1 to run Kafka integration tests")
	}
}

func kafkaEndpoint() string {
	if broker := os.Getenv("KAFKA_BROKER"); broker != "" {
		return broker
	}
	return "127.0.0.1:9092"
}

func TestResolveConfigDefaultsWithoutMutation(t *testing.T) {
	input := Config{}
	resolved, err := resolveConfig(input)
	require.NoError(t, err)
	assert.Empty(t, input.Brokers)
	assert.Equal(t, []string{"localhost:9092"}, resolved.Brokers)
	assert.Equal(t, 3*time.Second, resolved.DialTimeout)
	assert.Equal(t, BalancerLeastBytes, resolved.Balancer)
	assert.Equal(t, segmentio.RequireOne, resolved.RequiredAcks)
	assert.Equal(t, 1024, resolved.MinBytes)
	assert.Equal(t, 1048576, resolved.MaxBytes)
}

func TestResolveConfigClonesMutableValues(t *testing.T) {
	tlsConfig := &tls.Config{ServerName: "kafka.internal", MinVersion: tls.VersionTLS13}
	sasl := &SASLPlainConfig{Username: "app", Password: "secret"}
	input := Config{Brokers: []string{"kafka.internal:9093"}, TLS: tlsConfig, SASL: sasl}
	resolved, err := resolveConfig(input)
	require.NoError(t, err)
	resolved.Brokers[0] = "changed:9092"
	resolved.TLS.ServerName = "changed"
	resolved.SASL.Username = "changed"
	assert.Equal(t, "kafka.internal:9093", input.Brokers[0])
	assert.Equal(t, "kafka.internal", tlsConfig.ServerName)
	assert.Equal(t, "app", sasl.Username)
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
	}{
		{name: "nil config"},
		{name: "empty broker", cfg: &Config{Brokers: []string{""}}},
		{name: "invalid balancer", cfg: &Config{Balancer: "random"}},
		{name: "invalid required acks", cfg: &Config{RequiredAcks: 2}},
		{name: "missing SASL username", cfg: &Config{SASL: &SASLPlainConfig{Password: "secret"}}},
		{name: "minimum exceeds maximum", cfg: &Config{MinBytes: 2, MaxBytes: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.cfg)
			assert.Nil(t, client)
			assert.ErrorIs(t, err, ErrInvalidConfig)
		})
	}
}

func TestOptionsValidation(t *testing.T) {
	_, err := New(&Config{}, nil)
	assert.ErrorIs(t, err, ErrInvalidOption)
	_, err = New(&Config{}, WithDialer(nil))
	assert.ErrorIs(t, err, ErrInvalidOption)
	_, err = New(&Config{}, WithTransport(nil))
	assert.ErrorIs(t, err, ErrInvalidOption)
	_, err = New(&Config{}, WithAsyncCompletion(nil))
	assert.ErrorIs(t, err, ErrInvalidOption)
}

func TestProducerCachingAndValidation(t *testing.T) {
	completionCalled := false
	client, err := New(&Config{}, WithAsyncCompletion(func([]segmentio.Message, error) {
		completionCalled = true
	}))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })

	first, err := client.Producer("events")
	require.NoError(t, err)
	second, err := client.Producer("events")
	require.NoError(t, err)
	async, err := client.AsyncProducer("events")
	require.NoError(t, err)
	assert.Same(t, first, second)
	assert.NotSame(t, first, async)
	assert.False(t, first.Async)
	assert.True(t, async.Async)
	assert.Equal(t, segmentio.RequireOne, first.RequiredAcks)
	assert.Equal(t, segmentio.RequireOne, async.RequiredAcks)
	require.NotNil(t, async.Completion)
	async.Completion(nil, nil)
	assert.True(t, completionCalled)
	producer, err := client.Producer("")
	assert.Nil(t, producer)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestConsumerCachingUsesStructuredKey(t *testing.T) {
	client, err := New(&Config{})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })

	direct, err := client.Consumer("orders", 2)
	require.NoError(t, err)
	directAgain, err := client.Consumer("orders", 2)
	require.NoError(t, err)
	otherPartition, err := client.Consumer("orders", 3)
	require.NoError(t, err)
	group, err := client.ConsumerGroup("orders", "workers")
	require.NoError(t, err)
	assert.Same(t, direct, directAgain)
	assert.NotSame(t, direct, otherPartition)
	assert.NotSame(t, direct, group)

	reader, err := client.ConsumerGroup("orders", "")
	assert.Nil(t, reader)
	assert.ErrorIs(t, err, ErrInvalidConfig)
	reader, err = client.Consumer("orders", -1)
	assert.Nil(t, reader)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestCloseIsIdempotentAndPreventsNewResources(t *testing.T) {
	client, err := New(&Config{})
	require.NoError(t, err)
	require.NoError(t, client.Close())
	require.NoError(t, client.Close())
	producer, err := client.Producer("events")
	assert.Nil(t, producer)
	assert.ErrorIs(t, err, ErrClosed)
	consumer, err := client.Consumer("events", 0)
	assert.Nil(t, consumer)
	assert.ErrorIs(t, err, ErrClosed)
	assert.ErrorIs(t, client.Ping(t.Context()), ErrClosed)
}

func TestPingReturnsContextualError(t *testing.T) {
	client, err := New(&Config{Brokers: []string{"127.0.0.1:1"}})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	err = client.Ping(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kafka: ping")
}

func TestIntegrationProduceAndConsume(t *testing.T) {
	requireKafkaIntegration(t)
	client, err := New(&Config{
		Brokers:                []string{kafkaEndpoint()},
		AllowAutoTopicCreation: true,
		MinBytes:               1,
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	require.NoError(t, client.Start(t.Context()))
	require.NoError(t, client.HealthCheck(t.Context()))

	topic := "kit-kafka-integration"
	producer, err := client.Producer(topic)
	require.NoError(t, err)
	require.NoError(t, producer.WriteMessages(t.Context(), segmentio.Message{Value: []byte("hello")}))

	consumer, err := client.Consumer(topic, 0)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	message, err := consumer.ReadMessage(ctx)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(message.Value))
}
