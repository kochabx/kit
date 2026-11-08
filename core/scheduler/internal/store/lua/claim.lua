local function snapshot()
  local fields = redis.call(
    'HMGET',
    KEYS[1],
    'task',
    'payload',
    'state',
    'attempt',
    'max_attempts',
    'run_at',
    'created_at',
    'started_at',
    'finished_at',
    'expires_at',
    'unique_key',
    'fingerprint',
    'last_error',
    'queue',
    'version',
    'policy',
    'recovery_count',
    'failure_count',
    'generation'
  )
  for i = 1, 19 do
    if not fields[i] then
      fields[i] = ''
    end
  end
  return fields
end
-- Terminal state changes and delivery cleanup share this atomic operation.
local function discard()
  redis.call('XACKDEL', KEYS[3], ARGV[7], 'DELREF', 'IDS', 1, ARGV[8])
end
if redis.call('HGET', KEYS[1], 'queue') ~= ARGV[5] then
  discard()
  return -3
end
local state = redis.call('HGET', KEYS[1], 'state')
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
local id = redis.call('HGET', KEYS[1], 'id')
local function finish(state, retention, reason)
  append_attempt(KEYS[1], KEYS[5], ARGV[10], ARGV[11], now, state, reason)
  redis.call('HSET', KEYS[1], 'state', state, 'finished_at', now, 'last_error', reason)
  redis.call('HDEL', KEYS[1], 'lease_token', 'lease_until', 'cancel_requested')
  redis.call('ZREM', KEYS[2], id)
  if state == 'dead' then
    redis.call('ZADD', KEYS[4], now + tonumber(retention), id)
  end
  discard()
  if tonumber(retention) > 0 then
    redis.call('PEXPIRE', KEYS[1], retention)
  else
    redis.call('DEL', KEYS[1])
  end
end
if state ~= 'ready' and state ~= 'running' then
  discard()
  return -1
end
if state == 'running' and tonumber(redis.call('HGET', KEYS[1], 'lease_until') or '0') > now then
  if
    redis.call('HGET', KEYS[1], 'lease_token') == ARGV[1]
    and redis.call('HGET', KEYS[1], 'worker_id') == ARGV[2]
  then
    return snapshot()
  end
  return 0
end
if redis.call('HGET', KEYS[1], 'cancel_requested') == '1' then
  finish('cancelled', ARGV[6], 'cancelled')
  return -4
end
local expires_at = tonumber(redis.call('HGET', KEYS[1], 'expires_at') or '0')
if state == 'ready' and expires_at > 0 and expires_at <= now then
  finish('expired', ARGV[4], 'expired')
  return -2
end
local raw = redis.call('HGET', KEYS[1], 'policy')
local ok, policy = pcall(cjson.decode, raw or '')
if
  not ok
  or not policy.max_executions
  or not policy.max_recoveries
  or not policy.timeout
  or policy.timeout <= 0
then
  finish('dead', ARGV[9], 'invalid execution policy')
  return -5
end
local attempt = tonumber(redis.call('HGET', KEYS[1], 'attempt') or '0')
local recoveries = tonumber(redis.call('HGET', KEYS[1], 'recovery_count') or '0')
if
  attempt >= policy.max_executions or (state == 'running' and recoveries >= policy.max_recoveries)
then
  finish('dead', ARGV[9], 'execution or recovery budget exhausted')
  return -5
end
if state == 'running' then
  append_attempt(KEYS[1], KEYS[5], ARGV[10], ARGV[11], now, 'lease_lost', 'lease expired')
  recoveries = recoveries + 1
end
redis.call(
  'HSET',
  KEYS[1],
  'state',
  'running',
  'attempt',
  attempt + 1,
  'recovery_count',
  recoveries,
  'lease_token',
  ARGV[1],
  'worker_id',
  ARGV[2],
  'started_at',
  now,
  'lease_until',
  now + tonumber(ARGV[3])
)
redis.call('ZADD', KEYS[2], now, id)
return snapshot()
