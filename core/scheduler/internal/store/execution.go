package store

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"time"
)

type LeaseRequest struct {
	JobID, Token string
	Lease        time.Duration
	Renew        bool
}

type LeaseResult struct {
	OK  bool
	Err error
}

func encodePolicy(p ExecutionPolicy) string {
	b, _ := json.Marshal(p)
	return string(b)
}

func decodePolicy(raw string) ExecutionPolicy {
	var p ExecutionPolicy
	_ = json.Unmarshal([]byte(raw), &p)
	return p
}

type ClaimCommand struct {
	Token, Worker                                  string
	Duration, ExpiredRetention, CancelledRetention time.Duration
}

func (s *Queue) ClaimJob(ctx context.Context, d Delivery, command ClaimCommand) (*Job, error) {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	result, err := Claim.Run(
		ctx,
		s.Client,
		[]string{s.Keys.Job(d.JobID), s.Keys.Running(), s.Keys.Ready(), s.Keys.Dead(), s.Keys.History(d.JobID)},
		command.Token,
		command.Worker,
		CeilDurationMillis(command.Duration),
		CeilDurationMillis(command.ExpiredRetention),
		s.Keys.Name,
		CeilDurationMillis(command.CancelledRetention),
		s.Keys.Group(),
		d.MessageID,
		CeilDurationMillis(s.DeadRetention),
		s.HistoryLimit,
		CeilDurationMillis(s.HistoryTTL),
	).
		Result()
	if err != nil {
		return nil, err
	}
	if code, ok := result.(int64); ok {
		if code < 0 {
			return nil, ErrInvalidState
		}
		return nil, ErrLeaseLost
	}
	fields, ok := result.([]any)
	if !ok || len(fields) != 19 {
		return nil, fmt.Errorf("invalid start job response")
	}
	var values [19]string
	for i, field := range fields {
		value, ok := field.(string)
		if !ok {
			return nil, fmt.Errorf("invalid start job field")
		}
		values[i] = value
	}
	return &Job{
		Version:       values[14],
		Policy:        decodePolicy(values[15]),
		RecoveryCount: parseInt(values[16]),
		FailureCount:  parseInt(values[17]),
		Queue:         values[13],
		ID:            d.JobID,
		TaskName:      values[0],
		Payload:       []byte(values[1]),
		State:         JobState(values[2]),
		Attempt:       parseInt(values[3]),
		MaxAttempts:   parseInt(values[4]),
		RunAt:         ParseTimeMillis(values[5]),
		CreatedAt:     ParseTimeMillis(values[6]),
		StartedAt:     ParseTimeMillis(values[7]),
		FinishedAt:    ParseTimeMillis(values[8]),
		ExpiresAt:     ParseTimeMillis(values[9]),
		UniqueKey:     values[10],
		Fingerprint:   values[11],
		LastError:     values[12],
	}, nil
}

func (s *Queue) CompleteJob(ctx context.Context, d Delivery, token string, succeededRetention, cancelledRetention time.Duration) error {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	result, err := Complete.Run(
		ctx,
		s.Client,
		[]string{
			s.Keys.Job(d.JobID),
			s.Keys.Ready(),
			s.Keys.Running(),
			s.Keys.Receipt(d.JobID, token),
			s.Keys.History(d.JobID),
		},
		token,
		s.Keys.Group(),
		d.MessageID,
		CeilDurationMillis(succeededRetention),
		CeilDurationMillis(cancelledRetention),
		CeilDurationMillis(s.CompletionRetention),
		fmt.Sprintf("complete:%d:%d", succeededRetention, cancelledRetention),
		s.HistoryLimit,
		CeilDurationMillis(s.HistoryTTL),
	).
		Int64()
	if err != nil {
		return err
	}
	return decodeFinishResult(result)
}

type FailureCommand struct {
	Message                           string
	Delay                             time.Duration
	Retry                             bool
	DeadRetention, CancelledRetention time.Duration
}

func (s *Queue) FailJob(ctx context.Context, d Delivery, token string, command FailureCommand) error {
	ctx, cancelOperation := s.commandContext(ctx)
	defer cancelOperation()
	retryFlag := 0
	if command.Retry {
		retryFlag = 1
	}
	result, err := Fail.Run(
		ctx,
		s.Client,
		[]string{
			s.Keys.Job(d.JobID),
			s.Keys.Ready(),
			s.Keys.Scheduled(),
			s.Keys.Dead(),
			s.Keys.Running(),
			s.Keys.Receipt(d.JobID, token),
			s.Keys.History(d.JobID),
		},
		token,
		command.Message,
		CeilDurationMillis(command.Delay),
		retryFlag,
		s.Keys.Group(),
		d.MessageID,
		CeilDurationMillis(command.DeadRetention),
		CeilDurationMillis(command.CancelledRetention),
		CeilDurationMillis(s.CompletionRetention),
		failureSignature(command),
		s.HistoryLimit,
		CeilDurationMillis(s.HistoryTTL),
	).
		Int64()
	if err != nil {
		return err
	}
	return decodeFinishResult(result)
}

func failureSignature(c FailureCommand) string {
	b, _ := json.Marshal(struct {
		Message         string
		Delay           int64
		Retry           bool
		Dead, Cancelled int64
	}{c.Message, int64(c.Delay), c.Retry, int64(c.DeadRetention), int64(c.CancelledRetention)}, json.Deterministic(true))
	return fmt.Sprintf("fail:%x", sha256.Sum256(b))
}

func decodeFinishResult(code int64) error {
	switch code {
	case -3:
		return ErrConflict
	case 0:
		return ErrLeaseLost
	case 1:
		return nil
	case 2:
		return ErrJobCancelled
	default:
		return fmt.Errorf("unknown finish result %d: %w", code, ErrInvalidState)
	}
}
