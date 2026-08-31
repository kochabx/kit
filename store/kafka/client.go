package kafka

import (
	"context"
	"errors"
	"fmt"
	"sync"

	segmentio "github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
)

type consumerKey struct {
	topic     string
	groupID   string
	partition int
}

// Client owns cached Kafka producers and consumers.
type Client struct {
	config          Config
	dialer          *segmentio.Dialer
	transport       segmentio.RoundTripper
	ownedTransport  *segmentio.Transport
	asyncCompletion func([]segmentio.Message, error)

	mu             sync.Mutex
	closed         bool
	producers      map[string]*segmentio.Writer
	asyncProducers map[string]*segmentio.Writer
	consumers      map[consumerKey]*segmentio.Reader
	closeOnce      sync.Once
	closeErr       error
}

// New creates a lazy Kafka client without modifying cfg. Call Ping or Start
// when broker availability must be verified before use.
func New(cfg *Config, opts ...Option) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("%w: config is required", ErrInvalidConfig)
	}
	resolved, err := resolveConfig(*cfg)
	if err != nil {
		return nil, err
	}

	settings := clientOptions{}
	for index, option := range opts {
		if option == nil {
			return nil, fmt.Errorf("%w: nil option at index %d", ErrInvalidOption, index)
		}
		if err := option(&settings); err != nil {
			return nil, err
		}
	}

	mechanism := saslMechanism(resolved.SASL)
	dialer := settings.dialer
	if dialer == nil {
		dialer = &segmentio.Dialer{
			Timeout:       resolved.DialTimeout,
			DualStack:     true,
			TLS:           resolved.TLS,
			SASLMechanism: mechanism,
		}
	}
	transport := settings.transport
	var owned *segmentio.Transport
	if transport == nil {
		owned = &segmentio.Transport{
			DialTimeout: resolved.DialTimeout,
			TLS:         resolved.TLS,
			SASL:        mechanism,
		}
		transport = owned
	}

	return &Client{
		config:          resolved,
		dialer:          dialer,
		transport:       transport,
		ownedTransport:  owned,
		asyncCompletion: settings.asyncCompletion,
		producers:       make(map[string]*segmentio.Writer),
		asyncProducers:  make(map[string]*segmentio.Writer),
		consumers:       make(map[consumerKey]*segmentio.Reader),
	}, nil
}

func saslMechanism(cfg *SASLPlainConfig) sasl.Mechanism {
	if cfg == nil {
		return nil
	}
	return plain.Mechanism{Username: cfg.Username, Password: cfg.Password}
}

// Producer returns a cached synchronous writer for topic.
func (c *Client) Producer(topic string) (*segmentio.Writer, error) {
	return c.producer(topic, false)
}

// AsyncProducer returns a cached asynchronous writer for topic. WriteMessages
// only queues messages; delivery errors are not returned to the caller.
func (c *Client) AsyncProducer(topic string) (*segmentio.Writer, error) {
	return c.producer(topic, true)
}

func (c *Client) producer(topic string, async bool) (*segmentio.Writer, error) {
	if topic == "" {
		return nil, fmt.Errorf("%w: topic is required", ErrInvalidConfig)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrClosed
	}
	producers := c.producers
	if async {
		producers = c.asyncProducers
	}
	if producer := producers[topic]; producer != nil {
		return producer, nil
	}
	producer := &segmentio.Writer{
		Addr:                   segmentio.TCP(c.config.Brokers...),
		Topic:                  topic,
		Balancer:               c.config.newBalancer(),
		Transport:              c.transport,
		AllowAutoTopicCreation: c.config.AllowAutoTopicCreation,
		RequiredAcks:           c.config.RequiredAcks,
		Async:                  async,
	}
	if async {
		producer.Completion = c.asyncCompletion
	}
	producers[topic] = producer
	return producer, nil
}

// Consumer returns a cached reader for partition of topic.
func (c *Client) Consumer(topic string, partition int) (*segmentio.Reader, error) {
	if partition < 0 {
		return nil, fmt.Errorf("%w: partition must not be negative", ErrInvalidConfig)
	}
	return c.consumer(consumerKey{topic: topic, partition: partition})
}

// ConsumerGroup returns a cached group reader for topic and groupID.
func (c *Client) ConsumerGroup(topic, groupID string) (*segmentio.Reader, error) {
	if groupID == "" {
		return nil, fmt.Errorf("%w: consumer group ID is required", ErrInvalidConfig)
	}
	return c.consumer(consumerKey{topic: topic, groupID: groupID})
}

func (c *Client) consumer(key consumerKey) (*segmentio.Reader, error) {
	if key.topic == "" {
		return nil, fmt.Errorf("%w: topic is required", ErrInvalidConfig)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrClosed
	}
	if consumer := c.consumers[key]; consumer != nil {
		return consumer, nil
	}
	config := segmentio.ReaderConfig{
		Brokers:   append([]string(nil), c.config.Brokers...),
		Topic:     key.topic,
		GroupID:   key.groupID,
		Partition: key.partition,
		MinBytes:  c.config.MinBytes,
		MaxBytes:  c.config.MaxBytes,
		Dialer:    c.dialer,
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("%w: validate consumer: %w", ErrInvalidConfig, err)
	}
	consumer := segmentio.NewReader(config)
	c.consumers[key] = consumer
	return consumer, nil
}

// Ping verifies that at least one configured broker accepts a Kafka connection.
func (c *Client) Ping(ctx context.Context) error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return ErrClosed
	}
	var pingErrors []error
	for _, broker := range c.config.Brokers {
		connection, err := c.dialer.DialContext(ctx, "tcp", broker)
		if err != nil {
			pingErrors = append(pingErrors, fmt.Errorf("broker %q: %w", broker, err))
			continue
		}
		if err := connection.Close(); err != nil {
			return fmt.Errorf("kafka: close ping connection: %w", err)
		}
		return nil
	}
	return fmt.Errorf("kafka: ping: %w", errors.Join(pingErrors...))
}

// Close flushes and closes all producers and consumers. It is safe to call
// more than once.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		closers := make([]func() error, 0, len(c.producers)+len(c.asyncProducers)+len(c.consumers))
		for _, producer := range c.producers {
			closers = append(closers, producer.Close)
		}
		for _, producer := range c.asyncProducers {
			closers = append(closers, producer.Close)
		}
		for _, consumer := range c.consumers {
			closers = append(closers, consumer.Close)
		}
		c.mu.Unlock()

		errCh := make(chan error, len(closers))
		var group sync.WaitGroup
		for _, closeResource := range closers {
			group.Go(func() {
				if err := closeResource(); err != nil {
					errCh <- err
				}
			})
		}
		group.Wait()
		close(errCh)
		var closeErrors []error
		for err := range errCh {
			closeErrors = append(closeErrors, err)
		}
		if c.ownedTransport != nil {
			c.ownedTransport.CloseIdleConnections()
		}
		if err := errors.Join(closeErrors...); err != nil {
			c.closeErr = fmt.Errorf("kafka: close: %w", err)
		}
	})
	return c.closeErr
}

func (c *Client) Start(ctx context.Context) error       { return c.Ping(ctx) }
func (c *Client) Stop(context.Context) error            { return c.Close() }
func (c *Client) HealthCheck(ctx context.Context) error { return c.Ping(ctx) }
