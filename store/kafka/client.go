package kafka

import (
	"context"
	"strings"
	"sync"

	"github.com/kochabx/kit/log"
	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"
	"golang.org/x/sync/errgroup"
)

// Client Kafka 客户端封装，提供生产者和消费者的管理
type Client struct {
	config    *Config
	dialer    *kafka.Dialer
	transport *kafka.Transport
	logger    *log.Logger

	syncProducers  map[string]*kafka.Writer
	asyncProducers map[string]*kafka.Writer
	consumers      map[string]*kafka.Reader
	mu             sync.RWMutex
}

// New 创建新的 Kafka 客户端实例
func New(cfg *Config, opts ...Option) (*Client, error) {
	if cfg == nil {
		return nil, ErrInvalidConfig
	}

	if err := cfg.ApplyDefaults(); err != nil {
		return nil, err
	}

	clientOpts := applyOptions(opts)

	c := &Client{
		config:         cfg,
		logger:         clientOpts.logger,
		syncProducers:  make(map[string]*kafka.Writer),
		asyncProducers: make(map[string]*kafka.Writer),
		consumers:      make(map[string]*kafka.Reader),
	}

	// 使用默认全局日志
	if c.logger == nil {
		c.logger = log.G
	}

	if clientOpts.dialer != nil {
		c.dialer = clientOpts.dialer
	} else {
		c.dialer = c.createDialer()
	}
	c.transport = c.createTransport()

	return c, nil
}

// mechanism 创建 SASL 认证机制
func (c *Client) mechanism() plain.Mechanism {
	return plain.Mechanism{
		Username: c.config.Username,
		Password: c.config.Password,
	}
}

// createDialer 创建 Kafka 连接拨号器
func (c *Client) createDialer() *kafka.Dialer {
	dialer := &kafka.Dialer{
		Timeout:   c.config.Timeout,
		DualStack: true,
	}

	if c.config.Username != "" && c.config.Password != "" {
		dialer.SASLMechanism = c.mechanism()
	}

	return dialer
}

// createTransport 创建 Kafka 传输配置
func (c *Client) createTransport() *kafka.Transport {
	transport := &kafka.Transport{}

	if c.config.Username != "" && c.config.Password != "" {
		transport.SASL = c.mechanism()
	}

	return transport
}

// createWriter 创建 kafka writer 的通用方法
func (c *Client) createWriter(topic string, async bool) *kafka.Writer {
	return &kafka.Writer{
		Addr:                   kafka.TCP(c.config.Brokers...),
		Topic:                  topic,
		Balancer:               c.config.balancer(),
		Transport:              c.transport,
		AllowAutoTopicCreation: c.config.AllowAutoTopicCreation,
		Async:                  async,
	}
}

// Producer 获取指定主题的同步生产者，如果不存在则创建
func (c *Client) Producer(topic string) *kafka.Writer {
	c.mu.RLock()
	if w, ok := c.syncProducers[topic]; ok {
		c.mu.RUnlock()
		return w
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if w, ok := c.syncProducers[topic]; ok {
		return w
	}

	w := c.createWriter(topic, false)
	c.syncProducers[topic] = w
	return w
}

// AsyncProducer 获取指定主题的异步生产者，如果不存在则创建
func (c *Client) AsyncProducer(topic string) *kafka.Writer {
	c.mu.RLock()
	if w, ok := c.asyncProducers[topic]; ok {
		c.mu.RUnlock()
		return w
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if w, ok := c.asyncProducers[topic]; ok {
		return w
	}

	w := c.createWriter(topic, true)
	c.asyncProducers[topic] = w
	return w
}

// createReader 创建 kafka reader 的通用方法
func (c *Client) createReader(topic string, groupId string, partition int) *kafka.Reader {
	config := kafka.ReaderConfig{
		Brokers:  c.config.Brokers,
		Topic:    topic,
		MinBytes: c.config.MinBytes,
		MaxBytes: c.config.MaxBytes,
		Dialer:   c.dialer,
	}

	if groupId != "" {
		config.GroupID = groupId
	} else {
		config.Partition = partition
	}

	return kafka.NewReader(config)
}

// Consumer 获取指定主题的消费者，如果不存在则创建
func (c *Client) Consumer(topic string) *kafka.Reader {
	c.mu.RLock()
	if r, ok := c.consumers[topic]; ok {
		c.mu.RUnlock()
		return r
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if r, ok := c.consumers[topic]; ok {
		return r
	}

	r := c.createReader(topic, "", c.config.Partition)
	c.consumers[topic] = r
	return r
}

// buildConsumerGroupKey 构建消费者组的 key
func (c *Client) buildConsumerGroupKey(topic string, groupId string) string {
	var builder strings.Builder
	builder.WriteString(topic)
	builder.WriteString("-")
	builder.WriteString(groupId)
	return builder.String()
}

// ConsumerGroup 获取指定主题和消费者组的消费者，如果不存在则创建
func (c *Client) ConsumerGroup(topic string, groupId string) *kafka.Reader {
	key := c.buildConsumerGroupKey(topic, groupId)

	c.mu.RLock()
	if r, ok := c.consumers[key]; ok {
		c.mu.RUnlock()
		return r
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if r, ok := c.consumers[key]; ok {
		return r
	}

	r := c.createReader(topic, groupId, 0)
	c.consumers[key] = r
	return r
}

// Close 关闭所有的生产者和消费者连接
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), c.config.CloseTimeout)
	defer cancel()

	eg, _ := errgroup.WithContext(ctx)

	// 关闭同步生产者
	for _, w := range c.syncProducers {
		w := w
		eg.Go(func() error {
			return w.Close()
		})
	}

	// 关闭异步生产者
	for _, w := range c.asyncProducers {
		w := w
		eg.Go(func() error {
			return w.Close()
		})
	}

	// 关闭消费者
	for _, r := range c.consumers {
		r := r
		eg.Go(func() error {
			return r.Close()
		})
	}

	return eg.Wait()
}
