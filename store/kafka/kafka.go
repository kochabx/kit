package kafka

import (
	"context"
	"strings"
	"sync"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"
	"golang.org/x/sync/errgroup"
)

// Kafka 客户端封装，提供生产者和消费者的管理
type Kafka struct {
	config         *Config          // 配置信息
	dialer         *kafka.Dialer    // 连接拨号器
	transport      *kafka.Transport // 传输配置
	syncProducers  sync.Map         // 同步生产者映射表, 按主题存储
	asyncProducers sync.Map         // 异步生产者映射表，按主题存储
	consumers      sync.Map         // 消费者映射表，按主题-组合ID存储
}

// Option 配置选项函数类型
type Option func(*Kafka)

// New 创建新的Kafka客户端实例
func New(config *Config, opts ...Option) (*Kafka, error) {
	k := &Kafka{
		config: config,
	}

	if err := k.config.init(); err != nil {
		return nil, err
	}

	for _, opt := range opts {
		opt(k)
	}

	k.dialer = k.createDialer()
	k.transport = k.createTransport()

	return k, nil
}

// mechanism 创建SASL认证机制
func (k *Kafka) mechanism() plain.Mechanism {
	return plain.Mechanism{
		Username: k.config.Username,
		Password: k.config.Password,
	}
}

// createDialer 创建Kafka连接拨号器
func (k *Kafka) createDialer() *kafka.Dialer {
	dialer := &kafka.Dialer{
		Timeout:   k.config.Timeout,
		DualStack: true,
	}

	if k.config.Username != "" && k.config.Password != "" {
		dialer.SASLMechanism = k.mechanism()
	}

	return dialer
}

// createTransport 创建Kafka传输配置
func (k *Kafka) createTransport() *kafka.Transport {
	transport := &kafka.Transport{}

	if k.config.Username != "" && k.config.Password != "" {
		transport.SASL = k.mechanism()
	}

	return transport
}

// loadOrStore 通用的 LoadOrStore 逻辑，避免重复代码
func (k *Kafka) loadOrStore(store *sync.Map, key string, creator func() any) any {
	if value, exists := store.Load(key); exists {
		return value
	}

	newValue := creator()

	if actual, loaded := store.LoadOrStore(key, newValue); loaded {
		// 如果已经存在，尝试关闭新创建的资源
		if closer, ok := newValue.(interface{ Close() error }); ok {
			closer.Close() // 忽略关闭错误，因为这是清理临时资源
		}
		return actual
	}

	return newValue
}

// createWriter 创建kafka writer的通用方法
func (k *Kafka) createWriter(topic string, async bool) *kafka.Writer {
	return &kafka.Writer{
		Addr:                   kafka.TCP(k.config.Brokers...),
		Topic:                  topic,
		Balancer:               k.config.balancer(),
		Transport:              k.transport,
		AllowAutoTopicCreation: k.config.AllowAutoTopicCreation,
		Async:                  async,
	}
}

// Producer 获取指定主题的同步生产者，如果不存在则创建
func (k *Kafka) Producer(topic string) *kafka.Writer {
	return k.loadOrStore(&k.syncProducers, topic, func() any {
		return k.createWriter(topic, false)
	}).(*kafka.Writer)
}

// AsyncProducer 获取指定主题的异步生产者，如果不存在则创建
func (k *Kafka) AsyncProducer(topic string) *kafka.Writer {
	return k.loadOrStore(&k.asyncProducers, topic, func() any {
		return k.createWriter(topic, true)
	}).(*kafka.Writer)
}

// createReader 创建kafka reader的通用方法
func (k *Kafka) createReader(topic string, groupId string, partition int) *kafka.Reader {
	config := kafka.ReaderConfig{
		Brokers:  k.config.Brokers,
		Topic:    topic,
		MinBytes: int(k.config.MinBytes),
		MaxBytes: int(k.config.MaxBytes),
		Dialer:   k.dialer,
	}

	if groupId != "" {
		config.GroupID = groupId
	} else {
		config.Partition = partition
	}

	return kafka.NewReader(config)
}

// Consumer 获取指定主题的消费者，如果不存在则创建
func (k *Kafka) Consumer(topic string) *kafka.Reader {
	return k.loadOrStore(&k.consumers, topic, func() any {
		return k.createReader(topic, "", k.config.Partition)
	}).(*kafka.Reader)
}

// buildConsumerGroupKey 构建消费者组的key
func (k *Kafka) buildConsumerGroupKey(topic string, groupId string) string {
	var builder strings.Builder
	builder.WriteString(topic)
	builder.WriteString("-")
	builder.WriteString(groupId)
	return builder.String()
}

// ConsumerGroup 获取指定主题和消费者组的消费者，如果不存在则创建
func (k *Kafka) ConsumerGroup(topic string, groupId string) *kafka.Reader {
	key := k.buildConsumerGroupKey(topic, groupId)
	return k.loadOrStore(&k.consumers, key, func() any {
		return k.createReader(topic, groupId, 0)
	}).(*kafka.Reader)
}

// closeAll 关闭sync.Map中所有实现了Close方法的资源
func (k *Kafka) closeAll(store *sync.Map, eg *errgroup.Group) {
	store.Range(func(key, value any) bool {
		if closer, ok := value.(interface{ Close() error }); ok {
			eg.Go(func() error {
				return closer.Close()
			})
		}
		return true
	})
}

// Close 关闭所有的生产者和消费者连接
func (k *Kafka) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), k.config.CloseTimeout)
	defer cancel()

	eg, _ := errgroup.WithContext(ctx)

	// 关闭所有生产者和消费者
	k.closeAll(&k.syncProducers, eg)
	k.closeAll(&k.asyncProducers, eg)
	k.closeAll(&k.consumers, eg)

	return eg.Wait()
}
