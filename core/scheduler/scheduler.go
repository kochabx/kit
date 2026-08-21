package scheduler

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"
)

type handler struct {
	run         func(context.Context, []byte) error
	timeout     time.Duration
	retry       RetryPolicy
	fingerprint string
}

type Scheduler struct {
	store    store
	config   Config
	consumer string

	mu              sync.RWMutex
	handlers        map[string]handler
	running         atomic.Bool
	closed          atomic.Bool
	observerEvents  chan observedEvent
	observerWG      sync.WaitGroup
	observerOnce    sync.Once
	observerDropped atomic.Uint64
	leaseRequests   chan leaseRequest
	active          sync.Map
}

type observedEvent struct {
	kind  uint8
	ctx   context.Context
	event Event
}
type leaseRequest struct {
	jobID, token string
	lease        time.Duration
	renew        bool
	result       chan leaseResult
}
type leaseResult struct {
	ok  bool
	err error
}
type activeExecution struct {
	jobType string
	cancel  context.CancelCauseFunc
}

const (
	eventEnqueued uint8 = iota + 1
	eventStarted
	eventFinished
)

func New(client redis.UniversalClient, config Config) (*Scheduler, error) {
	if client == nil {
		return nil, fmt.Errorf("redis client is required")
	}
	config, err := prepareConfig(config)
	if err != nil {
		return nil, err
	}
	s := &Scheduler{
		store:          store{client: client, keys: newKeys(config.Namespace)},
		config:         config,
		consumer:       uuid.NewString(),
		handlers:       make(map[string]handler),
		observerEvents: make(chan observedEvent, config.ObserverBuffer),
		leaseRequests:  make(chan leaseRequest, config.Concurrency*2),
	}
	s.observerWG.Add(1)
	go s.observerLoop()
	return s, nil
}

func (s *Scheduler) register(name string, h handler) error {
	if s.running.Load() {
		return fmt.Errorf("register %q: %w", name, ErrAlreadyRun)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.handlers[name]; exists {
		return fmt.Errorf("handler %q already registered", name)
	}
	s.handlers[name] = h
	return nil
}

func (s *Scheduler) handler(name string) (handler, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.handlers[name]
	return h, ok
}

func Enqueue[T any](s *Scheduler, ctx context.Context, d Definition[T], payload T, opts ...EnqueueOption) (*Job, error) {
	record, err := prepareEnqueue(s, d, payload, opts...)
	if err != nil {
		return nil, err
	}
	job, err := s.store.enqueue(ctx, record)
	if err != nil {
		if errors.Is(err, ErrDuplicate) {
			return job, err
		}
		return nil, err
	}
	s.observe(eventEnqueued, ctx, Event{JobID: record.id, Type: d.name})
	return job, nil
}

func prepareEnqueue[T any](s *Scheduler, d Definition[T], payload T, opts ...EnqueueOption) (enqueueRecord, error) {
	if s.closed.Load() {
		return enqueueRecord{}, ErrClosed
	}
	if !validName.MatchString(d.name) || d.codec == nil || d.retry == nil || d.timeout <= 0 {
		return enqueueRecord{}, fmt.Errorf("invalid definition %q", d.name)
	}
	b, err := d.codec.Marshal(payload)
	if err != nil {
		return enqueueRecord{}, fmt.Errorf("encode payload: %w", err)
	}
	if len(b) > s.config.MaxPayloadBytes {
		return enqueueRecord{}, fmt.Errorf("payload is %d bytes; limit is %d", len(b), s.config.MaxPayloadBytes)
	}
	o := enqueueOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	if o.delay < 0 {
		return enqueueRecord{}, fmt.Errorf("delay cannot be negative")
	}
	if len(o.uniqueKey) > 256 || (o.uniqueKey != "" && o.uniqueTTL <= 0) {
		return enqueueRecord{}, fmt.Errorf("invalid unique job option")
	}
	fingerprint, err := definitionFingerprint(d)
	if err != nil {
		return enqueueRecord{}, fmt.Errorf("fingerprint definition %q: %w", d.name, err)
	}
	id := uuid.NewString()
	maxAttempts := retryLimit(d.retry)
	return enqueueRecord{id: id, typ: d.name, payload: b, runAt: o.runAt, delay: o.delay, maxAttempts: maxAttempts, uniqueKey: o.uniqueKey, uniqueTTL: o.uniqueTTL, definition: fingerprint}, nil
}

func retryLimit(policy RetryPolicy) int {
	switch p := policy.(type) {
	case Exponential:
		return max(1, p.MaxAttempts)
	case NoRetry:
		return 1
	default:
		return 0
	}
}

func (s *Scheduler) Get(ctx context.Context, id string) (*Job, error) { return s.store.get(ctx, id) }
func (s *Scheduler) Cancel(ctx context.Context, id string) error {
	if err := s.store.cancel(ctx, id, s.config.Retention); err != nil {
		return err
	}
	if value, ok := s.active.Load(id); ok {
		value.(activeExecution).cancel(ErrJobCancelled)
	}
	return nil
}
func (s *Scheduler) Ping(ctx context.Context) error { return s.store.ping(ctx) }

// Run starts dispatching and executing jobs and blocks until ctx is cancelled.
// A Scheduler may be run once. A producer-only process does not need to call Run.
func (s *Scheduler) Run(ctx context.Context) error {
	if s.closed.Load() {
		return ErrClosed
	}
	if !s.running.CompareAndSwap(false, true) {
		return ErrAlreadyRun
	}
	defer func() { s.closed.Store(true); s.stopObserver() }()
	if s.config.Role&RoleWorker != 0 {
		if err := s.store.ensureGroups(ctx); err != nil {
			return err
		}
	}
	s.config.Logger.Info().Str("consumer", s.consumer).Int("concurrency", s.config.Concurrency).Msg("scheduler started")

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g, gctx := errgroup.WithContext(runCtx)
	leaseCtx, stopLease := context.WithCancel(context.Background())
	defer stopLease()
	var leaseDone chan error
	deliveries := make(chan delivery, s.config.Prefetch)
	if s.config.Role&RoleDispatcher != 0 {
		g.Go(func() error { return s.dispatchLoop(gctx) })
		g.Go(func() error { return s.maintenanceLoop(gctx) })
	}
	if s.config.Role&RoleWorker != 0 {
		leaseDone = make(chan error, 1)
		go func() { leaseDone <- s.leaseLoop(leaseCtx) }()
		g.Go(func() error { return s.fetchLoop(gctx, deliveries) })
		g.Go(func() error { return s.recoveryLoop(gctx, deliveries) })
		for i := range s.config.Concurrency {
			slot := fmt.Sprintf("%s-%d", s.consumer, i)
			g.Go(func() error { return s.executorLoop(gctx, slot, deliveries) })
		}
	}
	done := make(chan error, 1)
	go func() { done <- g.Wait() }()
	var err error
	select {
	case err = <-done:
		cancel()
	case <-ctx.Done():
		cancel() // stop intake; active executions keep their independent context
		timer := time.NewTimer(s.config.ShutdownTimeout)
		select {
		case err = <-done:
		case <-timer.C:
			err = ErrShutdownTimeout
			s.active.Range(func(key, value any) bool {
				jobID, _ := key.(string)
				execution, _ := value.(activeExecution)
				s.config.Logger.Error().Str("job_id", jobID).Str("type", execution.jobType).Msg("job still running at shutdown")
				execution.cancel(ErrShutdownTimeout)
				return true
			})
			force := time.NewTimer(s.config.OperationTimeout)
			select {
			case <-done:
			case <-force.C:
			}
			force.Stop()
		}
		timer.Stop()
	}
	stopLease()
	if leaseDone != nil {
		<-leaseDone
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		if !errors.Is(err, ErrShutdownTimeout) {
			err = nil
		}
	}
	s.config.Logger.Info().Msg("scheduler stopped")
	if s.config.Role&RoleWorker != 0 {
		cleanupCtx, cleanupCancel := s.operationContext(context.Background())
		defer cleanupCancel()
		_ = s.store.cleanupConsumer(cleanupCtx, s.consumer+"-fetch")
		_ = s.store.cleanupConsumer(cleanupCtx, s.consumer+"-recovery")
	}
	return err
}

func (s *Scheduler) dispatchLoop(ctx context.Context) error {
	ticker := time.NewTicker(s.config.DispatchInterval)
	defer ticker.Stop()
	backoff := newFailureBackoff(s.config.FailureBackoff, s.config.FailureBackoffMax)
	for {
		var cycleErr error
		for drained := int64(0); drained < s.config.DispatchDrainLimit; {
			batch := min(s.config.DispatchBatch, s.config.DispatchDrainLimit-drained)
			moved, err := s.store.dispatch(ctx, batch)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				s.config.Logger.Error().Err(err).Msg("dispatch failed")
				cycleErr = err
				break
			}
			drained += moved
			if moved < batch {
				break
			}
		}
		if err := s.dispatchSchedules(ctx); err != nil && ctx.Err() == nil {
			s.config.Logger.Error().Err(err).Msg("cron dispatch failed")
			cycleErr = err
		}
		if cycleErr != nil {
			if !waitContext(ctx, backoff.Next()) {
				return nil
			}
			continue
		}
		backoff.Reset()
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) maintenanceLoop(ctx context.Context) error {
	backoff := newFailureBackoff(s.config.FailureBackoff, s.config.FailureBackoffMax)
	var scheduledCursor, cronCursor uint64
	for {
		acquired, err := s.store.acquireMaintenance(ctx, s.consumer, s.config.MaintenanceLeaseDuration)
		if err == nil && !acquired {
			if !waitContext(ctx, s.config.MaintenanceInterval) {
				return nil
			}
			continue
		}
		var nextScheduled, nextCron uint64
		if err == nil {
			nextScheduled, nextCron, err = s.runMaintenance(ctx, scheduledCursor, cronCursor)
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.config.Logger.Error().Err(err).Msg("scheduler maintenance failed")
			if !waitContext(ctx, backoff.Next()) {
				return nil
			}
			continue
		}
		scheduledCursor, cronCursor = nextScheduled, nextCron
		backoff.Reset()
		if !waitContext(ctx, s.config.MaintenanceInterval) {
			return nil
		}
	}
}

func (s *Scheduler) runMaintenance(ctx context.Context, scheduledCursor, cronCursor uint64) (uint64, uint64, error) {
	for drained := int64(0); drained < s.config.MaintenanceDrainLimit; {
		batch := min(s.config.MaintenanceBatch, s.config.MaintenanceDrainLimit-drained)
		removed, err := s.store.cleanupExpiredDead(ctx, batch)
		if err != nil {
			return scheduledCursor, cronCursor, err
		}
		drained += removed
		if removed < batch {
			break
		}
	}

	nextScheduled, _, err := s.store.cleanupOrphanedJobs(ctx, scheduledCursor, s.config.MaintenanceBatch)
	if err != nil {
		return scheduledCursor, cronCursor, err
	}
	nextCron, _, err := s.store.cleanupOrphanedSchedules(ctx, cronCursor, s.config.MaintenanceBatch)
	if err != nil {
		return nextScheduled, cronCursor, err
	}
	if _, err := s.store.cleanupStaleConsumers(ctx, s.config.ConsumerIdleTimeout, s.config.MaintenanceBatch); err != nil {
		return nextScheduled, nextCron, err
	}
	return nextScheduled, nextCron, nil
}

func (s *Scheduler) dispatchSchedules(ctx context.Context) error {
	for drained := int64(0); drained < s.config.CronDrainLimit; {
		batch := min(s.config.DispatchBatch, s.config.CronDrainLimit-drained)
		now, err := s.store.timestamp(ctx)
		if err != nil {
			return err
		}
		records, err := s.store.dueSchedules(ctx, batch)
		if err != nil {
			return err
		}
		for _, record := range records {
			if record.State != ScheduleStateActive {
				if disableErr := s.store.disableSchedule(ctx, record, "schedule is not active"); disableErr != nil {
					return disableErr
				}
				continue
			}
			next, nextErr := nextCronFrom(record, now)
			if nextErr != nil {
				s.config.Logger.Error().Err(nextErr).Str("schedule_id", record.ID).Msg("invalid persisted cron schedule disabled")
				if disableErr := s.store.disableSchedule(ctx, record, nextErr.Error()); disableErr != nil {
					return disableErr
				}
				continue
			}
			if _, fireErr := s.store.fireSchedule(ctx, record, next); fireErr != nil {
				return fireErr
			}
		}
		drained += int64(len(records))
		if int64(len(records)) < batch {
			break
		}
	}
	return nil
}

func (s *Scheduler) fetchLoop(ctx context.Context, deliveries chan<- delivery) error {
	backoff := newFailureBackoff(s.config.FailureBackoff, s.config.FailureBackoffMax)
	for {
		batch, err := s.store.receive(ctx, s.consumer+"-fetch", s.config.PollTimeout, s.config.FetchBatch)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.config.Logger.Error().Err(err).Msg("receive failed")
			if !waitContext(ctx, backoff.Next()) {
				return nil
			}
			continue
		}
		backoff.Reset()
		for _, d := range batch {
			select {
			case deliveries <- d:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

func (s *Scheduler) executorLoop(ctx context.Context, worker string, deliveries <-chan delivery) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case d := <-deliveries:
			s.execute(ctx, worker, d)
		}
	}
}

func (s *Scheduler) execute(parent context.Context, worker string, d delivery) {
	token := uuid.NewString()
	j, err := s.store.start(parent, d, token, worker, s.config.LeaseDuration)
	if err != nil {
		if !errors.Is(err, ErrInvalidState) && !errors.Is(err, ErrLeaseLost) && parent.Err() == nil {
			s.config.Logger.Error().Err(err).Str("job_id", d.jobID).Msg("start job failed")
		}
		return
	}
	h, ok := s.handler(j.Type)
	if !ok {
		opCtx, opCancel := s.operationContext(parent)
		defer opCancel()
		_ = s.store.fail(opCtx, d, token, ErrNoHandler.Error(), 0, false, s.config.DeadRetention)
		return
	}
	if h.fingerprint != j.Definition {
		opCtx, opCancel := s.operationContext(parent)
		defer opCancel()
		_ = s.store.fail(opCtx, d, token, ErrDefinitionMismatch.Error(), 0, false, s.config.DeadRetention)
		return
	}
	s.observe(eventStarted, parent, Event{JobID: j.ID, Type: j.Type, Attempt: j.Attempt})
	started := time.Now()
	executionCtx, stopExecution := context.WithCancelCause(context.Background())
	timeoutCtx, timeoutCancel := context.WithTimeout(executionCtx, h.timeout)
	execCtx, cancel := context.WithCancelCause(timeoutCtx)
	defer stopExecution(nil)
	defer timeoutCancel()
	defer cancel(nil)
	s.active.Store(j.ID, activeExecution{jobType: j.Type, cancel: stopExecution})
	defer s.active.Delete(j.ID)
	result := make(chan error, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				result <- Permanent(fmt.Errorf("handler panic: %v\n%s", recovered, debug.Stack()))
			}
		}()
		result <- h.run(execCtx, j.Payload)
	}()

	renew := time.NewTicker(s.config.LeaseRenewInterval)
	defer renew.Stop()
	checkCancellation := time.NewTicker(s.config.CancellationCheckInterval)
	defer checkCancellation.Stop()
	var execErr error
	waiting := true
	for waiting {
		select {
		case execErr = <-result:
			waiting = false
		case <-execCtx.Done():
			execErr = context.Cause(execCtx)
			waiting = false
		case <-renew.C:
			ok, renewErr := s.renewLease(executionCtx, j.ID, token, s.config.LeaseDuration)
			if renewErr != nil || !ok {
				if cause := context.Cause(executionCtx); cause != nil {
					renewErr = cause
				}
				execErr = renewErr
				if execErr == nil {
					execErr = ErrLeaseLost
				}
				waiting = false
				cancel(execErr)
			}
		case <-checkCancellation.C:
			cancelled, checkErr := s.checkCancellation(executionCtx, j.ID, token)
			if checkErr != nil {
				if cause := context.Cause(executionCtx); cause != nil {
					execErr = cause
					waiting = false
					cancel(execErr)
					continue
				}
				if !errors.Is(checkErr, ErrJobCancelled) && !errors.Is(checkErr, ErrLeaseLost) {
					s.config.Logger.Warn().Err(checkErr).Str("job_id", j.ID).Msg("cancellation check failed")
					continue
				}
				execErr = checkErr
				waiting = false
				cancel(execErr)
			} else if cancelled {
				execErr = ErrJobCancelled
				waiting = false
				cancel(execErr)
			}
		}
	}
	if errors.Is(context.Cause(execCtx), ErrJobCancelled) {
		execErr = ErrJobCancelled
	}
	if cause := context.Cause(executionCtx); cause != nil {
		execErr = cause
	}
	duration := time.Since(started)
	opCtx, opCancel := s.operationContext(parent)
	defer opCancel()
	if execErr == nil || errors.Is(execErr, ErrJobCancelled) {
		err = s.store.complete(opCtx, d, token, s.config.Retention)
	} else if !errors.Is(execErr, ErrLeaseLost) && !errors.Is(execErr, ErrShutdownTimeout) {
		delay, retry := h.retry.NextDelay(j.Attempt, execErr)
		if delay < 0 {
			delay = 0
		}
		if isPermanent(execErr) || errors.Is(execErr, ErrJobCancelled) || (j.MaxAttempts > 0 && j.Attempt >= j.MaxAttempts) {
			retry = false
		}
		err = s.store.fail(opCtx, d, token, execErr.Error(), delay, retry, s.config.DeadRetention)
	}
	if err != nil && !errors.Is(err, ErrLeaseLost) && !errors.Is(err, ErrJobCancelled) {
		s.config.Logger.Error().Err(err).Str("job_id", j.ID).Msg("finish job failed")
	}
	s.observe(eventFinished, parent, Event{JobID: j.ID, Type: j.Type, Attempt: j.Attempt, Duration: duration, Err: execErr})
}

func (s *Scheduler) recoveryLoop(ctx context.Context, deliveries chan<- delivery) error {
	interval := s.config.LeaseDuration / 2
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		start, recovered := "0-0", int64(0)
		for recovered < s.config.RecoveryLimit {
			count := min(s.config.RecoveryBatch, s.config.RecoveryLimit-recovered)
			messages, next, err := s.store.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: s.store.keys.ready(), Group: s.store.keys.group(), Consumer: s.consumer + "-recovery", MinIdle: s.config.LeaseDuration, Start: start, Count: count}).Result()
			if err != nil && !errors.Is(err, redis.Nil) {
				if ctx.Err() == nil {
					s.config.Logger.Error().Err(err).Msg("recover jobs failed")
				}
				break
			}
			for _, message := range messages {
				id, _ := message.Values["job_id"].(string)
				if id == "" {
					continue
				}
				select {
				case deliveries <- delivery{jobID: id, messageID: message.ID}:
					recovered++
				case <-ctx.Done():
					return nil
				}
			}
			if len(messages) == 0 || next == "0-0" || next == start {
				break
			}
			start = next
		}
	}
}

func (s *Scheduler) renewLease(ctx context.Context, jobID, token string, lease time.Duration) (bool, error) {
	result := make(chan leaseResult, 1)
	request := leaseRequest{jobID: jobID, token: token, lease: lease, renew: true, result: result}
	select {
	case s.leaseRequests <- request:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	select {
	case response := <-result:
		return response.ok, response.err
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func (s *Scheduler) checkCancellation(ctx context.Context, jobID, token string) (bool, error) {
	result := make(chan leaseResult, 1)
	request := leaseRequest{jobID: jobID, token: token, result: result}
	select {
	case s.leaseRequests <- request:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	select {
	case response := <-result:
		if response.err != nil {
			return errors.Is(response.err, ErrJobCancelled), response.err
		}
		if !response.ok {
			return false, ErrLeaseLost
		}
		return false, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}
func (s *Scheduler) leaseLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case first := <-s.leaseRequests:
			batch := []leaseRequest{first}
			timer := time.NewTimer(5 * time.Millisecond)
		drain:
			for len(batch) < s.config.Concurrency {
				select {
				case request := <-s.leaseRequests:
					batch = append(batch, request)
				case <-timer.C:
					break drain
				case <-ctx.Done():
					timer.Stop()
					return nil
				}
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			results := s.store.renewBatch(ctx, batch)
			for i, request := range batch {
				request.result <- results[i]
			}
		}
	}
}

func waitContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
func (s *Scheduler) operationContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), s.config.OperationTimeout)
}
func (s *Scheduler) observe(kind uint8, ctx context.Context, event Event) {
	defer func() { _ = recover() }()
	select {
	case s.observerEvents <- observedEvent{kind: kind, ctx: context.WithoutCancel(ctx), event: event}:
	default:
		s.observerDropped.Add(1)
	}
}
func (s *Scheduler) observerLoop() {
	defer s.observerWG.Done()
	for item := range s.observerEvents {
		func() {
			defer func() { _ = recover() }()
			switch item.kind {
			case eventEnqueued:
				s.config.Observer.Enqueued(item.ctx, item.event)
			case eventStarted:
				s.config.Observer.Started(item.ctx, item.event)
			case eventFinished:
				s.config.Observer.Finished(item.ctx, item.event)
			}
		}()
	}
}
func (s *Scheduler) stopObserver() {
	s.observerOnce.Do(func() { close(s.observerEvents) })
	done := make(chan struct{})
	go func() { s.observerWG.Wait(); close(done) }()
	timer := time.NewTimer(s.config.OperationTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		s.config.Logger.Error().Msg("observer flush timed out")
	}
}

// Close releases resources owned by a producer-only Scheduler.
func (s *Scheduler) Close() error {
	if s.running.Load() {
		return ErrAlreadyRun
	}
	if s.closed.Swap(true) {
		return nil
	}
	s.stopObserver()
	return nil
}
