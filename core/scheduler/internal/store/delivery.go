package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func (s *Queue) RecoverDeliveries(ctx context.Context, consumer string, idle time.Duration, cursor string, count int64) ([]Delivery, string, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	messages, next, err := s.Client.XAutoClaim(
		ctx,
		&redis.XAutoClaimArgs{
			Stream:   s.Keys.Ready(),
			Group:    s.Keys.Group(),
			Consumer: consumer,
			MinIdle:  idle,
			Start:    cursor,
			Count:    count,
		},
	).
		Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, cursor, err
	}
	out := make([]Delivery, 0, len(messages))
	for _, message := range messages {
		if id, ok := message.Values["job_id"].(string); ok && id != "" {
			out = append(out, Delivery{JobID: id, MessageID: message.ID, Recovered: true, Consumer: consumer})
		}
	}
	return out, next, nil
}

func (s *Queue) ReleaseDeliveries(ctx context.Context, deliveries []Delivery) error {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	if len(deliveries) == 0 {
		return nil
	}
	pipe := s.Client.Pipeline()
	for _, d := range deliveries {
		ReleaseDelivery.Eval(ctx, pipe, []string{s.Keys.Ready()}, s.Keys.Group(), d.MessageID, d.Consumer)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (s *Queue) EnsureConsumerGroup(ctx context.Context) error {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	if err := s.CheckCapabilities(ctx); err != nil {
		return err
	}
	err := s.Client.XGroupCreateMkStream(ctx, s.Keys.Ready(), s.Keys.Group(), "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create consumer group: %w", err)
	}
	return nil
}

type Delivery struct {
	JobID, MessageID string
	Recovered        bool
	Consumer         string
}

func (s *Queue) ReadDeliveries(ctx context.Context, consumer string, block time.Duration, count int64) ([]Delivery, error) {
	block = max(time.Millisecond, block)
	ctx, cancelRead := context.WithTimeout(ctx, block+s.Timeout+time.Second)
	defer cancelRead()
	streams := []string{s.Keys.Ready(), ">"}
	result, err := s.Client.XReadGroup(
		ctx,
		&redis.XReadGroupArgs{
			Group:    s.Keys.Group(),
			Consumer: consumer,
			Streams:  streams,
			Count:    count,
			MaxCount: count,
			MaxSize:  s.ReadMaxBytes,
			Claim:    s.ClaimIdle,
			Block:    block,
		},
	).
		Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	deliveries := make([]Delivery, 0, len(result))
	for _, stream := range result {
		if len(stream.Messages) == 0 {
			continue
		}
		for _, message := range stream.Messages {
			id, _ := message.Values["job_id"].(string)
			if id != "" {
				deliveries = append(deliveries, Delivery{
					JobID:     id,
					MessageID: message.ID,
					Consumer:  consumer,
					Recovered: message.DeliveredCount > 1,
				})
			}
		}
	}
	return deliveries, nil
}

func (s *Queue) DiscardDelivery(ctx context.Context, d Delivery) error {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	return Discard.Run(ctx, s.Client, []string{s.Keys.Ready()}, s.Keys.Group(), d.MessageID).Err()
}
