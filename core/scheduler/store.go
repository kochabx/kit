package scheduler

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// All scheduler keys contain the same Redis Cluster hash tag.
type keys struct{ prefix string }

func newKeys(namespace string) keys      { return keys{prefix: "scheduler:{" + namespace + "}:"} }
func (k keys) job(id string) string      { return k.prefix + "job:" + id }
func (k keys) scheduled() string         { return k.prefix + "scheduled" }
func (k keys) ready() string             { return k.prefix + "ready" }
func (k keys) dead() string              { return k.prefix + "dead" }
func (k keys) running() string           { return k.prefix + "running" }
func (k keys) schedules() string         { return k.prefix + "schedules" }
func (k keys) scheduleCatalog() string   { return k.prefix + "schedule:catalog" }
func (k keys) schedule(id string) string { return k.prefix + "schedule:" + id }
func (k keys) unique(v string) string    { return k.prefix + "unique:" + v }
func (k keys) group() string             { return k.prefix + "workers" }
func (k keys) maintenanceLease() string  { return k.prefix + "maintenance:lease" }

//go:embed lua/enqueue.lua
var enqueueScript string

//go:embed lua/dispatch.lua
var dispatchScript string

//go:embed lua/start.lua
var startScript string

//go:embed lua/complete.lua
var completeScript string

//go:embed lua/fail.lua
var failScript string

//go:embed lua/cancel.lua
var cancelScript string

//go:embed lua/discard.lua
var discardScript string

//go:embed lua/create_schedule.lua
var createScheduleScript string

//go:embed lua/fire_schedule.lua
var fireScheduleScript string

var (
	enqueueRedisScript        = redis.NewScript(enqueueScript)
	dispatchRedisScript       = redis.NewScript(dispatchScript)
	startRedisScript          = redis.NewScript(startScript)
	completeRedisScript       = redis.NewScript(completeScript)
	failRedisScript           = redis.NewScript(failScript)
	cancelRedisScript         = redis.NewScript(cancelScript)
	discardRedisScript        = redis.NewScript(discardScript)
	createScheduleRedisScript = redis.NewScript(createScheduleScript)
	fireScheduleRedisScript   = redis.NewScript(fireScheduleScript)
)

const renewScript = `if redis.call('HGET', KEYS[1], 'lease_token') ~= ARGV[1] then return 0 end;if redis.call('HGET',KEYS[1],'cancel_requested')=='1' then return -1 end;local t=redis.call('TIME'); local now=t[1]*1000+math.floor(t[2]/1000); redis.call('HSET', KEYS[1], 'lease_until', now+tonumber(ARGV[2])); return 1`
const checkCancellationScript = `if redis.call('HGET',KEYS[1],'lease_token')~=ARGV[1] then return 0 end;if redis.call('HGET',KEYS[1],'cancel_requested')=='1' then return -1 end;return 1`

type store struct {
	client redis.UniversalClient
	keys   keys
}

// timestamp returns the Redis server clock as a Unix millisecond timestamp.
// Redis is the shared ordering authority, so scheduler nodes never compare
// distributed deadlines using their local wall clocks.
func (s *store) timestamp(ctx context.Context) (int64, error) {
	now, err := s.client.Time(ctx).Result()
	if err != nil {
		return 0, err
	}
	return now.UnixMilli(), nil
}

type enqueueRecord struct {
	id, typ     string
	payload     []byte
	runAt       time.Time
	delay       time.Duration
	maxAttempts int
	uniqueKey   string
	uniqueTTL   time.Duration
	definition  string
}

func (s *store) enqueue(ctx context.Context, r enqueueRecord) (*Job, error) {
	unique := s.keys.prefix + "unique:none"
	if r.uniqueKey != "" {
		unique = s.keys.unique(r.uniqueKey)
	}
	result, err := enqueueRedisScript.Run(ctx, s.client,
		[]string{s.keys.job(r.id), s.keys.scheduled(), unique, s.keys.ready()},
		r.id, r.typ, r.payload, r.runAt.UnixMilli(), r.maxAttempts, r.uniqueKey, r.uniqueTTL.Milliseconds(), s.keys.prefix+"job:", r.delay.Milliseconds(), r.definition).Result()
	if err != nil {
		return nil, fmt.Errorf("enqueue job: %w", err)
	}
	values, ok := result.([]any)
	if !ok || len(values) < 3 {
		return nil, fmt.Errorf("enqueue job: unexpected redis response")
	}
	code, _ := values[0].(int64)
	id, _ := values[1].(string)
	if code == 0 {
		job, getErr := s.get(ctx, id)
		if getErr != nil {
			return nil, errors.Join(ErrDuplicate, getErr)
		}
		return job, ErrDuplicate
	}
	if len(values) < 5 {
		return nil, fmt.Errorf("enqueue job: incomplete redis response")
	}
	state, _ := values[2].(string)
	runAt, _ := values[3].(int64)
	createdAt, _ := values[4].(int64)
	return &Job{ID: id, Type: r.typ, State: State(state), Payload: r.payload, MaxAttempts: r.maxAttempts, RunAt: time.UnixMilli(runAt), CreatedAt: time.UnixMilli(createdAt), UniqueKey: r.uniqueKey, Definition: r.definition}, nil
}

func (s *store) batchEnqueue(ctx context.Context, records []enqueueRecord) ([]string, []State, []time.Time, []time.Time, []error) {
	pipe := s.client.Pipeline()
	cmds := make([]*redis.Cmd, len(records))
	for i, r := range records {
		unique := s.keys.prefix + "unique:none"
		if r.uniqueKey != "" {
			unique = s.keys.unique(r.uniqueKey)
		}
		// Script.Run uses EVALSHA in a pipeline and cannot transparently recover
		// from NOSCRIPT. EVAL keeps cold starts and Redis failovers reliable.
		cmds[i] = pipe.Eval(ctx, enqueueScript, []string{s.keys.job(r.id), s.keys.scheduled(), unique, s.keys.ready()}, r.id, r.typ, r.payload, r.runAt.UnixMilli(), r.maxAttempts, r.uniqueKey, r.uniqueTTL.Milliseconds(), s.keys.prefix+"job:", r.delay.Milliseconds(), r.definition)
	}
	_, execErr := pipe.Exec(ctx)
	ids := make([]string, len(records))
	states := make([]State, len(records))
	runAts := make([]time.Time, len(records))
	createdAts := make([]time.Time, len(records))
	errs := make([]error, len(records))
	for i, cmd := range cmds {
		result, err := cmd.Result()
		if err != nil {
			errs[i] = fmt.Errorf("enqueue job: %w", err)
			continue
		}
		values, ok := result.([]any)
		if !ok || len(values) < 3 {
			errs[i] = fmt.Errorf("enqueue job: unexpected redis response")
			continue
		}
		code, _ := values[0].(int64)
		ids[i], _ = values[1].(string)
		state, _ := values[2].(string)
		states[i] = State(state)
		if len(values) >= 5 {
			runAt, _ := values[3].(int64)
			createdAt, _ := values[4].(int64)
			runAts[i] = time.UnixMilli(runAt)
			createdAts[i] = time.UnixMilli(createdAt)
		}
		if code == 0 {
			errs[i] = ErrDuplicate
		}
	}
	if execErr != nil {
		for i := range errs {
			if errs[i] == nil {
				errs[i] = fmt.Errorf("enqueue batch: %w", execErr)
			}
		}
	}
	return ids, states, runAts, createdAts, errs
}

func (s *store) dispatch(ctx context.Context, batch int64) (int64, error) {
	result, err := dispatchRedisScript.Run(ctx, s.client,
		[]string{s.keys.scheduled(), s.keys.ready()},
		s.keys.prefix+"job:", batch).Int64()
	if err != nil {
		return 0, fmt.Errorf("dispatch jobs: %w", err)
	}
	return result, nil
}

func (s *store) cleanupExpiredDead(ctx context.Context, batch int64) (int64, error) {
	result, err := s.client.Eval(ctx, `local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000);local ids=redis.call('ZRANGEBYSCORE',KEYS[1],'-inf',now,'LIMIT',0,ARGV[1]);if #ids>0 then redis.call('ZREM',KEYS[1],unpack(ids)) end;return #ids`, []string{s.keys.dead()}, batch).Int64()
	if err != nil {
		return 0, fmt.Errorf("cleanup expired dead jobs: %w", err)
	}
	return result, nil
}

func (s *store) acquireMaintenance(ctx context.Context, owner string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, s.keys.maintenanceLease(), owner, ttl).Result()
}

func (s *store) cleanupOrphanedJobs(ctx context.Context, cursor uint64, batch int64) (uint64, int64, error) {
	return s.cleanupOrphanedIndex(ctx, s.keys.scheduled(), s.keys.job, cursor, batch)
}

func (s *store) cleanupOrphanedSchedules(ctx context.Context, cursor uint64, batch int64) (uint64, int64, error) {
	return s.cleanupOrphanedIndex(ctx, s.keys.scheduleCatalog(), s.keys.schedule, cursor, batch, s.keys.schedules())
}

func (s *store) cleanupOrphanedIndex(ctx context.Context, index string, entityKey func(string) string, cursor uint64, batch int64, secondaryIndexes ...string) (uint64, int64, error) {
	entries, next, err := s.client.ZScan(ctx, index, cursor, "*", batch).Result()
	if err != nil {
		return cursor, 0, err
	}
	ids := make([]string, 0, len(entries)/2)
	pipe := s.client.Pipeline()
	checks := make([]*redis.IntCmd, 0, len(entries)/2)
	for i := 0; i+1 < len(entries); i += 2 {
		ids = append(ids, entries[i])
		checks = append(checks, pipe.Exists(ctx, entityKey(entries[i])))
	}
	if len(checks) == 0 {
		return next, 0, nil
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return cursor, 0, err
	}
	orphans := make([]any, 0)
	for i, check := range checks {
		if check.Val() == 0 {
			orphans = append(orphans, ids[i])
		}
	}
	if len(orphans) == 0 {
		return next, 0, nil
	}
	removed, err := s.client.ZRem(ctx, index, orphans...).Result()
	if err != nil {
		return cursor, 0, err
	}
	for _, secondary := range secondaryIndexes {
		if err := s.client.ZRem(ctx, secondary, orphans...).Err(); err != nil {
			return cursor, removed, err
		}
	}
	return next, removed, nil
}

func (s *store) cleanupStaleConsumers(ctx context.Context, idle time.Duration, limit int64) (int64, error) {
	consumers, err := s.client.XInfoConsumers(ctx, s.keys.ready(), s.keys.group()).Result()
	if err != nil {
		if stringsContains(err.Error(), "NOGROUP") || stringsContains(err.Error(), "no such key") {
			return 0, nil
		}
		return 0, err
	}
	var removed int64
	for _, consumer := range consumers {
		if removed >= limit {
			break
		}
		if consumer.Pending != 0 || consumer.Idle < idle {
			continue
		}
		count, err := s.client.XGroupDelConsumer(ctx, s.keys.ready(), s.keys.group(), consumer.Name).Result()
		if err != nil {
			return removed, err
		}
		if count == 0 {
			removed++
		}
	}
	return removed, nil
}

func (s *store) ensureGroups(ctx context.Context) error {
	err := s.client.XGroupCreateMkStream(ctx, s.keys.ready(), s.keys.group(), "0").Err()
	if err != nil && !stringsContains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create consumer group: %w", err)
	}
	return nil
}

func stringsContains(s, part string) bool {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return true
		}
	}
	return false
}

type delivery struct {
	jobID, messageID string
}

func (s *store) receive(ctx context.Context, consumer string, block time.Duration, count int64) ([]delivery, error) {
	streams := []string{s.keys.ready(), ">"}
	result, err := s.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: s.keys.group(), Consumer: consumer, Streams: streams, Count: count, Block: block}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	deliveries := make([]delivery, 0, len(result))
	for _, stream := range result {
		if len(stream.Messages) == 0 {
			continue
		}
		for _, message := range stream.Messages {
			id, _ := message.Values["job_id"].(string)
			if id != "" {
				deliveries = append(deliveries, delivery{jobID: id, messageID: message.ID})
			}
		}
	}
	return deliveries, nil
}

func (s *store) start(ctx context.Context, d delivery, token, worker string, lease time.Duration) (*Job, error) {
	result, err := startRedisScript.Run(ctx, s.client, []string{s.keys.job(d.jobID), s.keys.running()}, token, worker, lease.Milliseconds()).Int64()
	if err != nil {
		return nil, err
	}
	if result < 0 {
		if discardErr := s.discard(ctx, d); discardErr != nil {
			return nil, discardErr
		}
		return nil, ErrInvalidState
	}
	if result == 0 {
		return nil, ErrLeaseLost
	}
	return s.get(ctx, d.jobID)
}

func (s *store) renewBatch(ctx context.Context, requests []leaseRequest) []leaseResult {
	pipe := s.client.Pipeline()
	cmds := make([]*redis.Cmd, len(requests))
	for i, r := range requests {
		if r.renew {
			cmds[i] = pipe.Eval(ctx, renewScript, []string{s.keys.job(r.jobID)}, r.token, r.lease.Milliseconds())
		} else {
			cmds[i] = pipe.Eval(ctx, checkCancellationScript, []string{s.keys.job(r.jobID)}, r.token)
		}
	}
	_, execErr := pipe.Exec(ctx)
	out := make([]leaseResult, len(requests))
	for i, cmd := range cmds {
		value, err := cmd.Int64()
		if err == nil && execErr != nil {
			err = execErr
		}
		if value == -1 && err == nil {
			err = ErrJobCancelled
		}
		out[i] = leaseResult{ok: value == 1, err: err}
	}
	return out
}

func (s *store) complete(ctx context.Context, d delivery, token string, retention time.Duration) error {
	result, err := completeRedisScript.Run(ctx, s.client, []string{s.keys.job(d.jobID), s.keys.ready(), s.keys.running()}, token, s.keys.group(), d.messageID, retention.Milliseconds()).Int64()
	if err != nil {
		return err
	}
	if result == 0 {
		return ErrLeaseLost
	}
	if result == 2 {
		return ErrJobCancelled
	}
	return nil
}

func (s *store) fail(ctx context.Context, d delivery, token, message string, retryDelay time.Duration, retry bool, retention time.Duration) error {
	retryFlag := 0
	if retry {
		retryFlag = 1
	}
	result, err := failRedisScript.Run(ctx, s.client, []string{s.keys.job(d.jobID), s.keys.ready(), s.keys.scheduled(), s.keys.dead(), s.keys.running()}, token, message, retryDelay.Milliseconds(), retryFlag, s.keys.group(), d.messageID, retention.Milliseconds()).Int64()
	if err != nil {
		return err
	}
	if result == 0 {
		return ErrLeaseLost
	}
	return nil
}

func (s *store) discard(ctx context.Context, d delivery) error {
	return discardRedisScript.Run(ctx, s.client, []string{s.keys.ready()}, s.keys.group(), d.messageID).Err()
}

func (s *store) cancel(ctx context.Context, id string, retention time.Duration) error {
	result, err := cancelRedisScript.Run(ctx, s.client, []string{s.keys.job(id), s.keys.scheduled()}, retention.Milliseconds()).Int64()
	if err != nil {
		return err
	}
	switch result {
	case -1:
		return ErrNotFound
	case 0:
		return ErrInvalidState
	default:
		return nil
	}
}

func (s *store) get(ctx context.Context, id string) (*Job, error) {
	m, err := s.client.HGetAll(ctx, s.keys.job(id)).Result()
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, ErrNotFound
	}
	return jobFromMap(id, m), nil
}

func jobFromMap(id string, m map[string]string) *Job {
	j := &Job{ID: id, Type: m["type"], State: State(m["state"]), Payload: []byte(m["payload"]), LastError: m["last_error"], UniqueKey: m["unique_key"], Definition: m["definition"]}
	j.Attempt = parseInt(m["attempt"])
	j.MaxAttempts = parseInt(m["max_attempts"])
	j.RunAt = millisTime(m["run_at"])
	j.CreatedAt = millisTime(m["created_at"])
	j.StartedAt = millisTime(m["started_at"])
	j.FinishedAt = millisTime(m["finished_at"])
	return j
}

func parseInt(v string) int { n, _ := strconv.Atoi(v); return n }
func millisTime(v string) time.Time {
	n, _ := strconv.ParseInt(v, 10, 64)
	if n == 0 {
		return time.Time{}
	}
	return time.UnixMilli(n)
}

func (s *store) ping(ctx context.Context) error { return s.client.Ping(ctx).Err() }

func (s *store) cleanupConsumer(ctx context.Context, consumer string) error {
	pending, err := s.client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: s.keys.ready(), Group: s.keys.group(), Start: "-", End: "+", Count: 1, Consumer: consumer}).Result()
	if err != nil && !stringsContains(err.Error(), "NOGROUP") {
		return err
	}
	if len(pending) != 0 {
		return nil
	}
	return s.client.XGroupDelConsumer(ctx, s.keys.ready(), s.keys.group(), consumer).Err()
}

func (s *store) createSchedule(ctx context.Context, schedule CronSchedule, payload []byte, maxAttempts int, definition string) error {
	result, err := createScheduleRedisScript.Run(ctx, s.client, []string{s.keys.schedule(schedule.ID), s.keys.schedules(), s.keys.scheduleCatalog()}, schedule.ID, schedule.Type, payload, schedule.Expression, schedule.Location, schedule.NextAt.UnixMilli(), maxAttempts, definition).Int64()
	if err != nil {
		return fmt.Errorf("create cron schedule: %w", err)
	}
	if result == 0 {
		return ErrDuplicate
	}
	return nil
}

type scheduleRecord struct {
	CronSchedule
	payload     []byte
	maxAttempts int
	definition  string
}

func (s *store) dueSchedules(ctx context.Context, limit int64) ([]scheduleRecord, error) {
	ids, err := s.client.Eval(ctx, `local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000);return redis.call('ZRANGEBYSCORE',KEYS[1],'-inf',now,'LIMIT',0,ARGV[1])`, []string{s.keys.schedules()}, limit).StringSlice()
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	pipe := s.client.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, 0, len(ids))
	for _, id := range ids {
		cmds = append(cmds, pipe.HGetAll(ctx, s.keys.schedule(id)))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	records := make([]scheduleRecord, 0, len(ids))
	orphans := make([]any, 0)
	for i := range cmds {
		m, cmdErr := cmds[i].Result()
		if cmdErr != nil || len(m) == 0 {
			if cmdErr == nil {
				orphans = append(orphans, ids[i])
			}
			continue
		}
		records = append(records, scheduleRecord{CronSchedule: CronSchedule{ID: ids[i], Type: m["type"], Expression: m["cron"], Location: m["timezone"], NextAt: millisTime(m["next_at"]), State: ScheduleState(m["state"]), LastRunAt: millisTime(m["last_run_at"])}, payload: []byte(m["payload"]), maxAttempts: parseInt(m["max_attempts"]), definition: m["definition"]})
	}
	if len(orphans) > 0 {
		if err := s.client.ZRem(ctx, s.keys.schedules(), orphans...).Err(); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func (s *store) disableSchedule(ctx context.Context, schedule scheduleRecord, reason string) error {
	_, err := s.client.Eval(ctx, `if redis.call('HGET',KEYS[1],'next_at')~=ARGV[2] then return 0 end;redis.call('HSET',KEYS[1],'state','invalid','last_error',ARGV[3]);redis.call('ZREM',KEYS[2],ARGV[1]);return 1`, []string{s.keys.schedule(schedule.ID), s.keys.schedules()}, schedule.ID, schedule.NextAt.UnixMilli(), reason).Result()
	if err != nil {
		return fmt.Errorf("disable invalid cron schedule: %w", err)
	}
	return nil
}

func (s *store) fireSchedule(ctx context.Context, schedule scheduleRecord, next int64) (bool, error) {
	instanceID := schedule.ID + ":" + strconv.FormatInt(schedule.NextAt.UnixMilli(), 10)
	result, err := fireScheduleRedisScript.Run(ctx, s.client, []string{s.keys.schedule(schedule.ID), s.keys.schedules(), s.keys.job(instanceID), s.keys.scheduled()}, schedule.NextAt.UnixMilli(), next, instanceID).Int64()
	return result == 1, err
}

func (s *store) cancelSchedule(ctx context.Context, id string) error {
	result, err := s.client.Eval(ctx, `if redis.call('EXISTS', KEYS[1]) == 0 then return -1 end redis.call('HSET', KEYS[1], 'state', 'cancelled'); redis.call('ZREM', KEYS[2], ARGV[1]); return 1`, []string{s.keys.schedule(id), s.keys.schedules()}, id).Int64()
	if err != nil {
		return err
	}
	if result == -1 {
		return ErrNotFound
	}
	return nil
}

func scheduleFromMap(id string, m map[string]string) *CronSchedule {
	return &CronSchedule{ID: id, Type: m["type"], Expression: m["cron"], Location: m["timezone"], NextAt: millisTime(m["next_at"]), State: ScheduleState(m["state"]), LastRunAt: millisTime(m["last_run_at"])}
}
func (s *store) getSchedule(ctx context.Context, id string) (*CronSchedule, error) {
	m, err := s.client.HGetAll(ctx, s.keys.schedule(id)).Result()
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, ErrNotFound
	}
	return scheduleFromMap(id, m), nil
}
func (s *store) getScheduleRecord(ctx context.Context, id string) (scheduleRecord, error) {
	m, err := s.client.HGetAll(ctx, s.keys.schedule(id)).Result()
	if err != nil {
		return scheduleRecord{}, err
	}
	if len(m) == 0 {
		return scheduleRecord{}, ErrNotFound
	}
	return scheduleRecord{CronSchedule: *scheduleFromMap(id, m), payload: []byte(m["payload"]), maxAttempts: parseInt(m["max_attempts"]), definition: m["definition"]}, nil
}
func (s *store) listSchedules(ctx context.Context, query ScheduleQuery) ([]*CronSchedule, error) {
	const scanBatch int64 = 256
	out := make([]*CronSchedule, 0, query.Limit)
	var catalogOffset, matched int64
	for int64(len(out)) < query.Limit {
		ids, err := s.client.ZRange(ctx, s.keys.scheduleCatalog(), catalogOffset, catalogOffset+scanBatch-1).Result()
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			break
		}
		catalogOffset += int64(len(ids))
		pipe := s.client.Pipeline()
		cmds := make([]*redis.MapStringStringCmd, len(ids))
		for i, id := range ids {
			cmds[i] = pipe.HGetAll(ctx, s.keys.schedule(id))
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, err
		}
		for i, cmd := range cmds {
			m := cmd.Val()
			if len(m) == 0 || (query.State != "" && ScheduleState(m["state"]) != query.State) {
				continue
			}
			if matched < query.Offset {
				matched++
				continue
			}
			out = append(out, scheduleFromMap(ids[i], m))
			if int64(len(out)) == query.Limit {
				break
			}
		}
		if len(ids) < int(scanBatch) {
			break
		}
	}
	return out, nil
}
func (s *store) setScheduleState(ctx context.Context, id string, active bool, next time.Time) error {
	state := "paused"
	score := int64(0)
	if active {
		state = "active"
		score = next.UnixMilli()
	}
	result, err := s.client.Eval(ctx, `if redis.call('EXISTS',KEYS[1])==0 then return 0 end redis.call('HSET',KEYS[1],'state',ARGV[2]);if ARGV[2]=='active' then redis.call('HSET',KEYS[1],'next_at',ARGV[3]);redis.call('ZADD',KEYS[2],ARGV[3],ARGV[1]) else redis.call('ZREM',KEYS[2],ARGV[1]) end;return 1`, []string{s.keys.schedule(id), s.keys.schedules()}, id, state, score).Int64()
	if err != nil {
		return err
	}
	if result == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *store) deleteSchedule(ctx context.Context, id string) error {
	result, err := s.client.Eval(ctx, `local n=redis.call('DEL',KEYS[1]);redis.call('ZREM',KEYS[2],ARGV[1]);redis.call('ZREM',KEYS[3],ARGV[1]);return n`, []string{s.keys.schedule(id), s.keys.schedules(), s.keys.scheduleCatalog()}, id).Int64()
	if err != nil {
		return err
	}
	if result == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *store) updateSchedule(ctx context.Context, id, expression, location string, next time.Time) error {
	result, err := s.client.Eval(ctx, `if redis.call('EXISTS',KEYS[1])==0 then return 0 end;redis.call('HSET',KEYS[1],'cron',ARGV[2],'timezone',ARGV[3],'next_at',ARGV[4],'state','active');redis.call('ZADD',KEYS[2],ARGV[4],ARGV[1]);return 1`, []string{s.keys.schedule(id), s.keys.schedules()}, id, expression, location, next.UnixMilli()).Int64()
	if err != nil {
		return err
	}
	if result == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *store) deadJobs(ctx context.Context, offset, count int64) ([]*Job, error) {
	ids, err := s.client.ZRevRange(ctx, s.keys.dead(), offset, offset+count-1).Result()
	if err != nil {
		return nil, err
	}
	jobs := make([]*Job, 0, len(ids))
	for _, id := range ids {
		job, getErr := s.get(ctx, id)
		if getErr == nil {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

func (s *store) queryDead(ctx context.Context, query DeadQuery) ([]*Job, error) {
	if query.Type == "" {
		return s.deadJobs(ctx, query.Offset, query.Limit)
	}
	const scanBatch int64 = 256
	out := make([]*Job, 0, query.Limit)
	var indexOffset, matched int64
	for int64(len(out)) < query.Limit {
		ids, err := s.client.ZRevRange(ctx, s.keys.dead(), indexOffset, indexOffset+scanBatch-1).Result()
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			break
		}
		indexOffset += int64(len(ids))
		pipe := s.client.Pipeline()
		cmds := make([]*redis.MapStringStringCmd, len(ids))
		for i, id := range ids {
			cmds[i] = pipe.HGetAll(ctx, s.keys.job(id))
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return nil, err
		}
		for i, cmd := range cmds {
			m := cmd.Val()
			if len(m) == 0 || m["type"] != query.Type {
				continue
			}
			if matched < query.Offset {
				matched++
				continue
			}
			out = append(out, jobFromMap(ids[i], m))
			if int64(len(out)) == query.Limit {
				break
			}
		}
		if len(ids) < int(scanBatch) {
			break
		}
	}
	return out, nil
}
func (s *store) runningJobs(ctx context.Context, offset, count int64) ([]*Job, error) {
	ids, err := s.client.ZRange(ctx, s.keys.running(), offset, offset+count-1).Result()
	if err != nil {
		return nil, err
	}
	jobs := make([]*Job, 0, len(ids))
	for _, id := range ids {
		job, getErr := s.get(ctx, id)
		if getErr == nil {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

func (s *store) retryDead(ctx context.Context, id string) error {
	result, err := s.client.Eval(ctx, `if redis.call('HGET',KEYS[1],'state')~='dead' then return 0 end;local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000);redis.call('HSET',KEYS[1],'state','scheduled','run_at',now,'last_error','','attempt',0);redis.call('HDEL',KEYS[1],'finished_at');redis.call('ZREM',KEYS[3],ARGV[1]);redis.call('ZADD',KEYS[2],now,ARGV[1]);redis.call('PERSIST',KEYS[1]);return 1`, []string{s.keys.job(id), s.keys.scheduled(), s.keys.dead()}, id).Int64()
	if err != nil {
		return err
	}
	if result == 0 {
		return ErrInvalidState
	}
	return nil
}
func (s *store) deleteDead(ctx context.Context, id string) error {
	result, err := s.client.Eval(ctx, `redis.call('ZREM',KEYS[2],ARGV[1]);return redis.call('DEL',KEYS[1])`, []string{s.keys.job(id), s.keys.dead()}, id).Int64()
	if err != nil {
		return err
	}
	if result == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *store) stats(ctx context.Context) (Stats, error) {
	result, err := s.client.Eval(ctx, `local stream=redis.call('XLEN',KEYS[2]);local pending=0;local p=redis.pcall('XPENDING',KEYS[2],ARGV[1]);if type(p)=='table' then pending=tonumber(p[1]) or 0 end;local ready=stream-pending;if ready<0 then ready=0 end;return {redis.call('ZCARD',KEYS[1]),ready,pending,redis.call('ZCARD',KEYS[3]),redis.call('ZCARD',KEYS[4]),redis.call('ZCARD',KEYS[5])}`, []string{s.keys.scheduled(), s.keys.ready(), s.keys.running(), s.keys.dead(), s.keys.scheduleCatalog()}, s.keys.group()).Int64Slice()
	if err != nil {
		return Stats{}, err
	}
	if len(result) != 6 {
		return Stats{}, fmt.Errorf("scheduler stats: unexpected redis response")
	}
	return Stats{Scheduled: result[0], Ready: result[1], Pending: result[2], Running: result[3], Dead: result[4], CronSchedules: result[5]}, nil
}
