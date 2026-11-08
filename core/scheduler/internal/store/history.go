package store

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"github.com/kochabx/kit/core/scheduler/internal/model"
	"time"
)

// Attempts returns newest-first history, independent of the Job retention TTL.
func (s *Namespace) Attempts(ctx context.Context, id string) ([]model.Attempt, error) {
	ctx, cancel := s.commandContext(ctx)
	defer cancel()
	rows, err := s.Client.LRange(ctx, s.Keys.History(id), 0, 999).Result()
	if err != nil {
		return nil, err
	}
	out := make([]model.Attempt, 0, len(rows))
	for _, row := range rows {
		var r struct {
			JobID      string `json:"job_id"`
			Token      string `json:"token"`
			WorkerID   string `json:"worker_id"`
			Number     int    `json:"number"`
			StartedAt  int64  `json:"started_at"`
			FinishedAt int64  `json:"finished_at"`
			Outcome    string `json:"outcome"`
			Error      string `json:"error"`
		}
		if err := json.Unmarshal([]byte(row), &r); err != nil {
			return nil, fmt.Errorf("decode attempt history: %w", err)
		}
		out = append(
			out,
			model.Attempt{
				JobID:      r.JobID,
				Token:      r.Token,
				WorkerID:   r.WorkerID,
				Number:     r.Number,
				StartedAt:  time.UnixMilli(r.StartedAt),
				FinishedAt: time.UnixMilli(r.FinishedAt),
				Outcome:    r.Outcome,
				Error:      r.Error,
			},
		)
	}
	return out, nil
}
