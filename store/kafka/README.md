# Kafka

`store/kafka` 基于 `kafka-go` 管理可复用的 producer 和 consumer，并提供配置校验、SASL/PLAIN、TLS、连接检查和应用生命周期支持。

## 创建客户端

```go
client, err := kafka.New(&kafka.Config{
	Brokers: []string{"kafka.internal:9092"},
})
if err != nil {
	return err
}
defer client.Close()

if err := client.Ping(ctx); err != nil {
	return err
}
```

`New` 不会修改调用方配置，也不会强制连接 broker。需要启动时验证连接，可以调用 `Ping(ctx)`，或将 Client 注册为应用组件，由 `Start(ctx)` 执行检查。

默认配置：

| 字段 | 默认值 |
| --- | --- |
| `Brokers` | `localhost:9092` |
| `DialTimeout` | `3s` |
| `Balancer` | `least_bytes` |
| `RequiredAcks` | `1`（leader 确认） |
| `MinBytes` | `1024` |
| `MaxBytes` | `1048576` |

## 认证和 TLS

```go
client, err := kafka.New(&kafka.Config{
	Brokers: []string{"kafka.internal:9093"},
	SASL: &kafka.SASLPlainConfig{
		Username: "app",
		Password: "secret",
	},
	TLS: &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: "kafka.internal",
	},
})
```

同一套认证和 TLS 配置会同时用于 producer、consumer 和 `Ping`。TLS 配置不会参与 JSON 序列化。

## Producer

同步 producer 会把 broker 返回的写入错误传递给调用方：

```go
producer, err := client.Producer("orders.created")
if err != nil {
	return err
}
err = producer.WriteMessages(ctx, segmentio.Message{Value: payload})
```

异步 producer 适合调用方不等待 broker 确认的场景：

```go
producer, err := client.AsyncProducer("telemetry")
if err != nil {
	return err
}
err = producer.WriteMessages(ctx, segmentio.Message{Value: payload})
```

异步 `WriteMessages` 只表示消息已进入本地队列，不能用于需要确认投递结果的业务操作。相同 topic 和模式会返回同一个并发安全的 Writer。

支持的分区均衡策略：

- `BalancerLeastBytes`
- `BalancerHash`

### 异步投递结果

需要观察异步投递结果时，在创建 Client 时设置统一的 Completion：

```go
client, err := kafka.New(cfg,
	kafka.WithAsyncCompletion(func(messages []segmentio.Message, err error) {
		if err != nil {
			// 记录失败、告警或提交到补偿流程。
		}
	}),
)

producer, err := client.AsyncProducer("telemetry")
```

Completion 是可选的；未设置时，异步投递错误不会反馈给调用方。设置后，它会在创建 Client 时固定，并应用到 Client 管理的所有异步 producer，避免修改共享 Writer 引发数据竞争。`Close` 会等待异步队列刷新以及正在执行的 Completion 返回。

可以通过原生 `segmentio.RequiredAcks` 配置 broker 确认级别：

```go
cfg.RequiredAcks = segmentio.RequireAll
```

默认使用 `segmentio.RequireOne`。同步与异步 producer 使用相同的确认策略。

## Consumer

指定分区消费：

```go
consumer, err := client.Consumer("orders.created", 0)
message, err := consumer.ReadMessage(ctx)
```

partition 属于具体 consumer，因此在创建 Consumer 时传入，不放在全局 Config 中。

消费组：

```go
consumer, err := client.ConsumerGroup("orders.created", "billing")
message, err := consumer.FetchMessage(ctx)
if err != nil {
	return err
}

if err := handle(message); err != nil {
	return err
}
return consumer.CommitMessages(ctx, message)
```

需要严格处理顺序时，使用 `FetchMessage`，在业务操作持久化成功后再调用 `CommitMessages`。Kafka offset 与外部数据库写入不是同一个原子事务，处理函数必须能承受重复消息。

## 高级依赖

普通连接配置应放在 `Config`。只有需要完整替换 kafka-go 底层组件时才使用：

```go
client, err := kafka.New(cfg,
	kafka.WithDialer(customDialer),
	kafka.WithTransport(customTransport),
)
```

`WithDialer` 影响 consumer 和健康检查，`WithTransport` 影响 producer。传入自定义对象后，其配置一致性由调用方负责。

## 生命周期

Client 实现：

- `Start(ctx)`：检查 broker 连接
- `HealthCheck(ctx)`：检查 broker 连接
- `Stop(ctx)`：刷新并关闭所有 producer 和 consumer

`Close` 可重复调用。关闭开始后，创建 producer 或 consumer 会返回 `ErrClosed`。异步 producer 的关闭可能等待本地队列刷新，因此应给应用整体优雅退出流程预留足够时间。

由应用统一管理 Client 时，不要再对同一实例执行 `defer client.Close()`。
