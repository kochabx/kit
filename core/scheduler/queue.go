package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Queue 队列管理器
type Queue struct {
	client          *redis.Client
	namespace       string
	consumerName    string      // Worker唯一标识
	priorities      [3]Priority // 预分配优先级数组
	keyDelayedCache string      // 缓存延迟队列key
	keyGroupCache   string      // 缓存消费者组key
	keyStreamHigh   string      // 缓存高优先级stream key
	keyStreamNormal string      // 缓存普通优先级stream key
	keyStreamLow    string      // 缓存低优先级stream key
}

// NewQueue 创建队列管理器
func NewQueue(client *redis.Client, namespace string) *Queue {
	q := &Queue{
		client:     client,
		namespace:  namespace,
		priorities: [3]Priority{PriorityHigh, PriorityNormal, PriorityLow},
	}
	// 预计算常用key
	q.keyDelayedCache = fmt.Sprintf("%s:delayed", namespace)
	q.keyGroupCache = fmt.Sprintf("%s:consumers", namespace)
	q.keyStreamHigh = fmt.Sprintf("%s:stream:high", namespace)
	q.keyStreamNormal = fmt.Sprintf("%s:stream:normal", namespace)
	q.keyStreamLow = fmt.Sprintf("%s:stream:low", namespace)
	return q
}

// SetConsumer 设置消费者名称
func (q *Queue) SetConsumer(consumerName string) {
	q.consumerName = consumerName
}

// keyDelayed 延迟队列key
func (q *Queue) keyDelayed() string {
	return q.keyDelayedCache
}

// keyStream 就绪队列Stream key（使用预计算缓存）
func (q *Queue) keyStream(priority Priority) string {
	switch {
	case priority >= PriorityHigh:
		return q.keyStreamHigh
	case priority >= PriorityNormal:
		return q.keyStreamNormal
	default:
		return q.keyStreamLow
	}
}

// keyConsumerGroup 消费者组名
func (q *Queue) keyConsumerGroup() string {
	return q.keyGroupCache
}

// AddDelayed 添加任务到延迟队列
func (q *Queue) AddDelayed(ctx context.Context, taskID string, score float64) error {
	return q.client.ZAdd(ctx, q.keyDelayed(), redis.Z{
		Score:  score,
		Member: taskID,
	}).Err()
}

// AddReady 添加任务到就绪队列
func (q *Queue) AddReady(ctx context.Context, taskID string, priority Priority) error {
	streamKey := q.keyStream(priority)
	groupName := q.keyConsumerGroup()

	// 确保消费者组存在
	err := q.client.XGroupCreateMkStream(ctx, streamKey, groupName, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("failed to create consumer group: %w", err)
	}

	// 添加消息到Stream
	return q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]any{
			"task_id":  taskID,
			"priority": int(priority),
			"added_at": time.Now().Unix(),
		},
	}).Err()
}

// PopReady 从就绪队列获取任务
// 按优先级顺序获取：high -> normal -> low
func (q *Queue) PopReady(ctx context.Context, timeout int) (string, Priority, string, error) {
	if q.consumerName == "" {
		return "", 0, "", fmt.Errorf("consumer name not set")
	}

	groupName := q.keyGroupCache

	// 按优先级尝试读取（高优先级优先）- 使用预分配数组
	for i := range 3 {
		priority := q.priorities[i]
		streamKey := q.keyStream(priority)

		// 尝试读取单个 stream（不阻塞）
		results, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    groupName,
			Consumer: q.consumerName,
			Streams:  []string{streamKey, ">"},
			Count:    1,
			Block:    -1, // 不阻塞：负值使 go-redis 省略 BLOCK 参数
		}).Result()

		if err != nil {
			// 如果 stream 或 group 不存在，跳过
			if strings.Contains(err.Error(), "NOGROUP") {
				continue
			}
			if err != redis.Nil {
				return "", 0, "", err
			}
			continue
		}

		if len(results) > 0 && len(results[0].Messages) > 0 {
			msg := results[0].Messages[0]
			taskID, ok := msg.Values["task_id"].(string)
			if !ok {
				continue
			}
			msgID := msg.ID

			return taskID, priority, msgID, nil
		}
	}

	// 所有优先级都没有任务，进行阻塞等待
	// 构建streams参数 - 使用预分配数组
	streams := make([]string, 6) // 3 streams + 3 ">"
	for i := range 3 {
		streams[i] = q.keyStream(q.priorities[i])
		streams[i+3] = ">"
	}

	// 使用 XREADGROUP 阻塞等待新任务（Redis 原生阻塞）
	results, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    groupName,
		Consumer: q.consumerName,
		Streams:  streams,
		Count:    1,
		Block:    time.Duration(timeout) * time.Second, // Redis 原生阻塞
	}).Result()

	if err != nil {
		if err == redis.Nil {
			// 超时，没有新任务
			return "", 0, "", nil
		}
		// 忽略 NOGROUP 错误（stream 还未创建）
		if strings.Contains(err.Error(), "NOGROUP") {
			return "", 0, "", nil
		}
		return "", 0, "", err
	}

	// 返回第一个有消息的 stream
	for _, result := range results {
		if len(result.Messages) > 0 {
			msg := result.Messages[0]
			taskID, ok := msg.Values["task_id"].(string)
			if !ok {
				continue
			}
			msgID := msg.ID

			// 根据 stream key 确定优先级 - 使用预分配数组
			var priority Priority
			for i := range 3 {
				if result.Stream == q.keyStream(q.priorities[i]) {
					priority = q.priorities[i]
					break
				}
			}

			return taskID, priority, msgID, nil
		}
	}

	return "", 0, "", nil
}

// MoveDelayedToReady 将到期的延迟任务移到就绪队列
func (q *Queue) MoveDelayedToReady(ctx context.Context, now int64, limit int) (int64, error) {
	// 获取到期任务
	tasks, err := q.client.ZRangeByScore(ctx, q.keyDelayed(), &redis.ZRangeBy{
		Min:   "-inf",
		Max:   fmt.Sprintf("%d", now),
		Count: int64(limit),
	}).Result()

	if err != nil {
		return 0, err
	}

	if len(tasks) == 0 {
		return 0, nil
	}

	// 第一阶段 Pipeline：获取任务优先级
	pipe := q.client.Pipeline()

	// 批量获取任务优先级
	taskKeys := make([]string, len(tasks))
	for i, taskID := range tasks {
		taskKeys[i] = fmt.Sprintf("%s:task:%s", q.namespace, taskID)
		pipe.HGet(ctx, taskKeys[i], "priority")
	}

	results, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return 0, err
	}

	// 第二个 Pipeline：批量移动任务
	pipe = q.client.Pipeline()
	moved := int64(0)

	for i, taskID := range tasks {
		if i >= len(results) {
			break
		}

		// 获取优先级
		priorityStr, err := results[i].(*redis.StringCmd).Result()
		if err != nil {
			continue
		}

		priority, _ := strconv.Atoi(priorityStr)
		streamKey := q.keyStream(Priority(priority))
		groupName := q.keyConsumerGroup()

		// 确保消费者组存在
		err = q.client.XGroupCreateMkStream(ctx, streamKey, groupName, "0").Err()
		if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
			continue
		}

		// 添加到 Stream
		pipe.XAdd(ctx, &redis.XAddArgs{
			Stream: streamKey,
			Values: map[string]any{
				"task_id": taskID,
			},
		})

		// 从延迟队列移除
		pipe.ZRem(ctx, q.keyDelayed(), taskID)
		moved++
	}

	cmders, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		// 检查各 command 结果，统计实际成功数
		actualMoved := int64(0)
		for _, cmd := range cmders {
			if cmd.Err() == nil {
				actualMoved++
			}
		}
		// 每个任务对应 XAdd + ZRem 两条命令，实际移动数 = 成功的 XAdd 数
		return actualMoved / 2, err
	}

	return moved, nil
}

// RemoveDelayed 从延迟队列移除任务
func (q *Queue) RemoveDelayed(ctx context.Context, taskID string) error {
	return q.client.ZRem(ctx, q.keyDelayed(), taskID).Err()
}

// RemoveReady 从就绪队列移除任务
func (q *Queue) RemoveReady(ctx context.Context, taskID string) error {
	// 需要从所有优先级的 Stream 中查找并删除该任务
	groupName := q.keyGroupCache
	const batchSize = 100

	for i := range 3 {
		streamKey := q.keyStream(q.priorities[i])

		// 分批扫描 Stream，找到后立即返回（避免全量扫描）
		lastID := "-"
		for {
			messages, err := q.client.XRangeN(ctx, streamKey, lastID, "+", int64(batchSize)).Result()
			if err != nil || len(messages) == 0 {
				break
			}

			for _, msg := range messages {
				lastID = msg.ID
				if taskIDVal, ok := msg.Values["task_id"]; ok {
					if taskIDVal.(string) == taskID {
						q.client.XDel(ctx, streamKey, msg.ID)
						q.client.XAck(ctx, streamKey, groupName, msg.ID)
						return nil
					}
				}
			}

			if len(messages) < batchSize {
				break // 已到末尾
			}
			// XRange 是闭区间，需要跳过最后一条已处理的消息
			// 通过在 lastID 后追加以确保不重复（Redis stream ID 是递增的）
			lastID = lastID + "\x00"
		}
	}

	return nil
}

// AckMessage 确认消息已处理
func (q *Queue) AckMessage(ctx context.Context, priority Priority, msgID string) error {
	streamKey := q.keyStream(priority)
	groupName := q.keyConsumerGroup()
	return q.client.XAck(ctx, streamKey, groupName, msgID).Err()
}

// GetDelayedCount 获取延迟队列任务数
func (q *Queue) GetDelayedCount(ctx context.Context) (int64, error) {
	return q.client.ZCard(ctx, q.keyDelayed()).Result()
}

// GetReadyCount 获取就绪队列任务数
func (q *Queue) GetReadyCount(ctx context.Context, priority Priority) (int64, error) {
	streamKey := q.keyStream(priority)
	info, err := q.client.XInfoStream(ctx, streamKey).Result()
	if err != nil {
		if err.Error() == "ERR no such key" {
			return 0, nil
		}
		return 0, err
	}
	return info.Length, nil
}

// GetPendingCount 获取Pending消息数（未ACK的消息）
func (q *Queue) GetPendingCount(ctx context.Context, priority Priority) (int64, error) {
	streamKey := q.keyStream(priority)
	groupName := q.keyConsumerGroup()

	pending, err := q.client.XPending(ctx, streamKey, groupName).Result()
	if err != nil {
		if err.Error() == "NOGROUP No such key" || err.Error() == "ERR no such key" {
			return 0, nil
		}
		return 0, err
	}
	return pending.Count, nil
}

// ClaimStaleMessages 接管超时的Pending消息
func (q *Queue) ClaimStaleMessages(ctx context.Context, priority Priority, idleTime time.Duration) ([]string, error) {
	streamKey := q.keyStream(priority)
	groupName := q.keyConsumerGroup()

	// 获取Pending消息列表
	pending, err := q.client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: streamKey,
		Group:  groupName,
		Start:  "-",
		End:    "+",
		Count:  100,
	}).Result()

	if err != nil {
		// 如果stream或consumer group不存在，直接返回空结果
		if strings.Contains(err.Error(), "NOGROUP") {
			return nil, nil
		}
		return nil, err
	}

	var claimedTaskIDs []string
	for _, msg := range pending {
		// 超过空闲时间的消息才接管
		if msg.Idle >= idleTime {
			// 使用XCLAIM接管消息
			claimed, err := q.client.XClaim(ctx, &redis.XClaimArgs{
				Stream:   streamKey,
				Group:    groupName,
				Consumer: q.consumerName,
				MinIdle:  idleTime,
				Messages: []string{msg.ID},
			}).Result()

			if err == nil && len(claimed) > 0 {
				taskID, ok := claimed[0].Values["task_id"].(string)
				if ok {
					claimedTaskIDs = append(claimedTaskIDs, taskID)
				}
			}
		}
	}

	return claimedTaskIDs, nil
}

// GetStats 获取队列统计信息
func (q *Queue) GetStats(ctx context.Context) (*QueueStats, error) {
	delayedCount, _ := q.GetDelayedCount(ctx)
	highCount, _ := q.GetReadyCount(ctx, PriorityHigh)
	normalCount, _ := q.GetReadyCount(ctx, PriorityNormal)
	lowCount, _ := q.GetReadyCount(ctx, PriorityLow)

	// Pending消息计入运行中
	highPending, _ := q.GetPendingCount(ctx, PriorityHigh)
	normalPending, _ := q.GetPendingCount(ctx, PriorityNormal)
	lowPending, _ := q.GetPendingCount(ctx, PriorityLow)
	runningCount := highPending + normalPending + lowPending

	return &QueueStats{
		DelayedCount: delayedCount,
		HighCount:    highCount,
		NormalCount:  normalCount,
		LowCount:     lowCount,
		RunningCount: runningCount,
	}, nil
}

// Clear 清空所有队列
func (q *Queue) Clear(ctx context.Context) error {
	pipe := q.client.Pipeline()
	pipe.Del(ctx, q.keyDelayed())
	pipe.Del(ctx, q.keyStream(PriorityHigh))
	pipe.Del(ctx, q.keyStream(PriorityNormal))
	pipe.Del(ctx, q.keyStream(PriorityLow))
	_, err := pipe.Exec(ctx)
	return err
}
