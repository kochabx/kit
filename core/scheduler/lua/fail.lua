if redis.call('HGET', KEYS[1], 'lease_token') ~= ARGV[1] then return 0 end
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
redis.call('HSET', KEYS[1], 'last_error', ARGV[2], 'finished_at', now)
redis.call('HDEL', KEYS[1], 'lease_token', 'lease_until')
if tonumber(ARGV[4]) == 1 then
  local retry_at = now + tonumber(ARGV[3])
  redis.call('HSET', KEYS[1], 'state', 'scheduled', 'run_at', retry_at)
  redis.call('HDEL', KEYS[1], 'finished_at', 'started_at', 'worker_id')
  redis.call('ZADD', KEYS[3], retry_at, redis.call('HGET', KEYS[1], 'id'))
else
  redis.call('HSET', KEYS[1], 'state', 'dead')
  redis.call('ZADD', KEYS[4], now + tonumber(ARGV[7]), redis.call('HGET', KEYS[1], 'id'))
  redis.call('PEXPIRE', KEYS[1], ARGV[7])
end
redis.call('XACK', KEYS[2], ARGV[5], ARGV[6])
redis.call('XDEL', KEYS[2], ARGV[6])
redis.call('ZREM',KEYS[5],redis.call('HGET',KEYS[1],'id'))
return 1
