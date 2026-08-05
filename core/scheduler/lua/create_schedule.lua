if redis.call('EXISTS', KEYS[1]) == 1 then return 0 end
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
redis.call('HSET', KEYS[1], 'id', ARGV[1], 'type', ARGV[2], 'payload', ARGV[3],
  'cron', ARGV[4], 'timezone', ARGV[5], 'next_at', ARGV[6],
  'max_attempts', ARGV[7], 'definition', ARGV[8], 'state', 'active', 'created_at', now)
redis.call('ZADD', KEYS[2], ARGV[6], ARGV[1])
redis.call('ZADD', KEYS[3], now, ARGV[1])
return 1
