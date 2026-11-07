local replay = read_receipt(KEYS[6], ARGV[10])
if replay then return replay end
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
if not owns_lease(KEYS[1], ARGV[1], now) then return 0 end
if redis.call('HGET', KEYS[1], 'cancel_requested') == '1' then
  local id = redis.call('HGET', KEYS[1], 'id')
  append_attempt(KEYS[1], KEYS[7], ARGV[11], ARGV[12], now, 'cancelled', ARGV[2])
  redis.call('HSET', KEYS[1], 'state', 'cancelled', 'finished_at', now)
  redis.call('HDEL', KEYS[1], 'lease_token', 'lease_until', 'cancel_requested')
  ack_delivery(KEYS[2], ARGV[5], ARGV[6])
  redis.call('ZREM', KEYS[3], id)
  redis.call('ZREM', KEYS[5], id)
  if tonumber(ARGV[8]) > 0 then
    redis.call('PEXPIRE', KEYS[1], ARGV[8])
  else
    redis.call('DEL', KEYS[1])
  end
  return remember_result(KEYS[6], ARGV[10], ARGV[9], 2)
end
redis.call('HINCRBY', KEYS[1], 'failure_count', 1)
redis.call('HSET', KEYS[1], 'last_error', ARGV[2], 'finished_at', now)
local failures = tonumber(redis.call('HGET', KEYS[1], 'failure_count') or '0')
local maximum = tonumber(redis.call('HGET', KEYS[1], 'max_attempts') or '0')
if tonumber(ARGV[4]) == 1 and (maximum == 0 or failures < maximum) then
  local retry_at = now + tonumber(ARGV[3])
  append_attempt(KEYS[1], KEYS[7], ARGV[11], ARGV[12], now, 'retried', ARGV[2])
  redis.call('HSET', KEYS[1], 'state', 'scheduled', 'run_at', retry_at)
  redis.call('HDEL', KEYS[1], 'finished_at', 'started_at', 'worker_id')
  redis.call('ZADD', KEYS[3], retry_at, redis.call('HGET', KEYS[1], 'id'))
else
  append_attempt(KEYS[1], KEYS[7], ARGV[11], ARGV[12], now, 'dead', ARGV[2])
  redis.call('HSET', KEYS[1], 'state', 'dead')
  redis.call('ZADD', KEYS[4], now + tonumber(ARGV[7]), redis.call('HGET', KEYS[1], 'id'))
  redis.call('PEXPIRE', KEYS[1], ARGV[7])
end
redis.call('HDEL', KEYS[1], 'lease_token', 'lease_until')
ack_delivery(KEYS[2], ARGV[5], ARGV[6])
redis.call('ZREM', KEYS[5], redis.call('HGET', KEYS[1], 'id'))
return remember_result(KEYS[6], ARGV[10], ARGV[9], 1)
