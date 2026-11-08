if redis.call('HGET', KEYS[1], 'queue') ~= ARGV[4] then
  return 0
end
if redis.call('HGET', KEYS[1], 'state') ~= 'active' then
  return 0
end
if redis.call('HGET', KEYS[1], 'next_at') ~= ARGV[1] then
  return 0
end
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
redis.call(
  'HSET',
  KEYS[3],
  'id',
  ARGV[3],
  'queue',
  redis.call('HGET', KEYS[1], 'queue'),
  'task',
  redis.call('HGET', KEYS[1], 'task'),
  'payload',
  redis.call('HGET', KEYS[1], 'payload'),
  'state',
  'ready',
  'run_at',
  ARGV[1],
  'created_at',
  now,
  'attempt',
  0,
  'max_attempts',
  redis.call('HGET', KEYS[1], 'max_attempts'),
  'incarnation',
  ARGV[3],
  'schedule_id',
  redis.call('HGET', KEYS[1], 'id'),
  'fingerprint',
  redis.call('HGET', KEYS[1], 'fingerprint'),
  'version',
  redis.call('HGET', KEYS[1], 'version'),
  'policy',
  redis.call('HGET', KEYS[1], 'policy'),
  'failure_count',
  0,
  'recovery_count',
  0
)
redis.call('XADD', KEYS[4], 'IDMP', 'scheduler', ARGV[3] .. ':0:0', '*', 'job_id', ARGV[3])
redis.call('HSET', KEYS[1], 'next_at', ARGV[2])
redis.call('HSET', KEYS[1], 'last_run_at', ARGV[1])
redis.call('ZADD', KEYS[2], ARGV[2], redis.call('HGET', KEYS[1], 'id'))
return 1
