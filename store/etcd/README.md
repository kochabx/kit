# Etcd

`store/etcd` 提供 etcd 客户端生命周期管理，以及基于租约的非阻塞分布式锁和服务注册。

## 客户端

```go
client, err := etcd.New(&etcd.Config{
	Endpoints: []string{"etcd.internal:2379"},
	Username:  "app",
	Password:  "secret",
})
if err != nil {
	return err
}
defer client.Close()

if err := client.Ping(ctx); err != nil {
	return err
}
```

`New` 会复制配置、应用默认值、执行校验并创建官方 etcd 客户端，但不会强制等待集群连接。需要启动时验证连接，可以调用 `Ping(ctx)`，或将 Client 注册为应用组件，由 `Start(ctx)` 完成检查。

默认配置：

| 字段 | 默认值 |
| --- | --- |
| `Endpoints` | `localhost:2379` |
| `DialTimeout` | `5s` |
| `KeepAliveTime` | `30s` |
| `KeepAliveTimeout` | `5s` |

TLS 使用原生配置，且不会参与 JSON 序列化：

```go
client, err := etcd.New(&etcd.Config{
	Endpoints: []string{"etcd.internal:2379"},
	TLS: &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: "etcd.internal",
	},
})
```

需要官方客户端的完整 API 时：

```go
response, err := client.Client().Get(ctx, "config/app")
```

## 分布式锁

```go
lock, err := client.NewLock("locks/order/123", 10*time.Second)
if err != nil {
	return err
}
if err := lock.TryLock(ctx); err != nil {
	if errors.Is(err, etcd.ErrLockHeld) {
		return nil
	}
	return err
}
defer lock.Unlock(context.Background())
```

`TryLock` 不等待其他持有者释放锁。TTL 在创建 Lock 时配置一次；成功加锁后，模块会持续续租，直到调用 `Unlock`。`Unlock` 可重复调用。

## 服务注册

```go
registry, err := client.NewRegistry("services/payment", 15*time.Second)
if err != nil {
	return err
}
if err := registry.Register(ctx, "instance-1", "10.0.0.8:8080"); err != nil {
	return err
}
defer registry.Deregister(context.Background())

services, err := registry.Services(ctx)
watch := registry.Watch(ctx)
```

一个 Registry 实例同时维护一个注册记录。重复注册会返回 `ErrServiceExists`，`Deregister` 可重复调用。`Services` 返回以 service ID 为键的结果，不暴露内部完整 etcd key。

## 生命周期

Client 实现以下应用组件方法：

- `Start(ctx)`：检查集群连接
- `HealthCheck(ctx)`：检查集群连接
- `Stop(ctx)`：关闭客户端

由应用统一管理 Client 时，不要再对同一实例执行 `defer client.Close()`。
