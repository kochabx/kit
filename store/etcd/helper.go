package etcd

import (
	"context"
	"fmt"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// DistributedLock 分布式锁结构
type DistributedLock struct {
	etcd    *Etcd
	key     string
	leaseID clientv3.LeaseID
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// NewDistributedLock 创建分布式锁
func (e *Etcd) NewDistributedLock(key string, ttl int64) *DistributedLock {
	return &DistributedLock{
		etcd:   e,
		key:    key,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// TryLock 尝试获取锁（非阻塞）
func (dl *DistributedLock) TryLock(ctx context.Context, ttl int64) error {
	if dl.etcd.Client == nil {
		return ErrEtcdNotInitialized
	}

	// 创建租约
	lease, err := dl.etcd.Client.Grant(ctx, ttl)
	if err != nil {
		return err
	}
	dl.leaseID = lease.ID

	// 尝试获取锁
	cmp := clientv3.Compare(clientv3.CreateRevision(dl.key), "=", 0)
	put := clientv3.OpPut(dl.key, "locked", clientv3.WithLease(dl.leaseID))

	resp, err := dl.etcd.Client.Txn(ctx).If(cmp).Then(put).Commit()
	if err != nil {
		dl.etcd.Client.Revoke(ctx, dl.leaseID)
		return err
	}

	if !resp.Succeeded {
		dl.etcd.Client.Revoke(ctx, dl.leaseID)
		return fmt.Errorf("failed to acquire lock: key already exists")
	}

	// 开始保持租约活跃
	go dl.keepAlive(ctx)

	return nil
}

// Unlock 释放锁
func (dl *DistributedLock) Unlock(ctx context.Context) error {
	close(dl.stopCh)
	<-dl.doneCh

	if dl.leaseID != 0 {
		_, err := dl.etcd.Client.Revoke(ctx, dl.leaseID)
		return err
	}
	return nil
}

// keepAlive 保持租约活跃
func (dl *DistributedLock) keepAlive(ctx context.Context) {
	defer close(dl.doneCh)

	ch, kaerr := dl.etcd.Client.KeepAlive(ctx, dl.leaseID)
	if kaerr != nil {
		return
	}

	for {
		select {
		case <-dl.stopCh:
			return
		case <-ctx.Done():
			return
		case ka := <-ch:
			if ka == nil {
				return
			}
		}
	}
}

// ServiceRegistry 服务注册结构
type ServiceRegistry struct {
	etcd      *Etcd
	keyPrefix string
	ttl       int64
	leaseID   clientv3.LeaseID
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// NewServiceRegistry 创建服务注册实例
func (e *Etcd) NewServiceRegistry(keyPrefix string, ttl int64) *ServiceRegistry {
	return &ServiceRegistry{
		etcd:      e,
		keyPrefix: keyPrefix,
		ttl:       ttl,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// Register 注册服务
func (sr *ServiceRegistry) Register(ctx context.Context, serviceID, serviceInfo string) error {
	if sr.etcd.Client == nil {
		return ErrEtcdNotInitialized
	}

	// 创建租约
	lease, err := sr.etcd.Client.Grant(ctx, sr.ttl)
	if err != nil {
		return err
	}
	sr.leaseID = lease.ID

	// 注册服务
	key := fmt.Sprintf("%s/%s", sr.keyPrefix, serviceID)
	_, err = sr.etcd.Client.Put(ctx, key, serviceInfo, clientv3.WithLease(sr.leaseID))
	if err != nil {
		sr.etcd.Client.Revoke(ctx, sr.leaseID)
		return err
	}

	// 开始保持租约活跃
	go sr.keepAlive(ctx)

	return nil
}

// Deregister 注销服务
func (sr *ServiceRegistry) Deregister(ctx context.Context) error {
	close(sr.stopCh)
	<-sr.doneCh

	if sr.leaseID != 0 {
		_, err := sr.etcd.Client.Revoke(ctx, sr.leaseID)
		return err
	}
	return nil
}

// keepAlive 保持租约活跃
func (sr *ServiceRegistry) keepAlive(ctx context.Context) {
	defer close(sr.doneCh)

	ch, kaerr := sr.etcd.Client.KeepAlive(ctx, sr.leaseID)
	if kaerr != nil {
		return
	}

	for {
		select {
		case <-sr.stopCh:
			return
		case <-ctx.Done():
			return
		case ka := <-ch:
			if ka == nil {
				return
			}
		}
	}
}

// DiscoverServices 发现服务
func (sr *ServiceRegistry) DiscoverServices(ctx context.Context) (map[string]string, error) {
	if sr.etcd.Client == nil {
		return nil, ErrEtcdNotInitialized
	}

	resp, err := sr.etcd.Client.Get(ctx, sr.keyPrefix, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, kv := range resp.Kvs {
		result[string(kv.Key)] = string(kv.Value)
	}
	return result, nil
}

// WatchServices 监听服务变化
func (sr *ServiceRegistry) WatchServices(ctx context.Context) clientv3.WatchChan {
	if sr.etcd.Client == nil {
		return nil
	}
	return sr.etcd.Client.Watch(ctx, sr.keyPrefix, clientv3.WithPrefix())
}
