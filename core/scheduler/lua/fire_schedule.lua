if redis.call('HGET', KEYS[1], 'state') ~= 'active' then return 0 end
if redis.call('HGET', KEYS[1], 'next_at') ~= ARGV[1] then return 0 end
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
redis.call('HSET', KEYS[3], 'id', ARGV[3],
  'type', redis.call('HGET', KEYS[1], 'type'),
  'payload', redis.call('HGET', KEYS[1], 'payload'),
  'state', 'scheduled', 'run_at', ARGV[1], 'created_at', now,
  'attempt', 0, 'max_attempts', redis.call('HGET', KEYS[1], 'max_attempts'),
  'schedule_id', redis.call('HGET', KEYS[1], 'id'),
  'definition', redis.call('HGET', KEYS[1], 'definition'))
redis.call('ZADD', KEYS[4], ARGV[1], ARGV[3])
redis.call('HSET', KEYS[1], 'next_at', ARGV[2])
redis.call('HSET', KEYS[1], 'last_run_at', ARGV[1])
redis.call('ZADD', KEYS[2], ARGV[2], redis.call('HGET', KEYS[1], 'id'))
return 1
