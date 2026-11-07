package scheduler

import (
	"context"

	"github.com/kochabx/kit/core/scheduler/internal/model"
	"github.com/kochabx/kit/core/scheduler/internal/store"
)

type Stats = model.Stats

func (s *clientState) Stats(ctx context.Context) (Stats, error) {
	stores := make([]*store.Queue, 0, len(s.queueOrder))
	for _, q := range s.queueOrder {
		stores = append(stores, q.store)
	}
	total, err := s.namespace.StatsBatch(ctx, stores)
	if err != nil {
		return Stats{}, err
	}
	total.ObserverDropped = s.events.droppedCount()
	return total, nil
}
