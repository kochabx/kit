if redis.call('EXISTS', KEYS[1]) == 0 then
  return 0
end
local state = redis.call('HGET', KEYS[1], 'state')
if state == 'cancelled' then
  return -1
end
if state ~= 'active' and state ~= 'paused' and state ~= 'invalid' then
  return -1
end
if state == 'invalid' then
  state = 'active'
end
redis.call('HSET', KEYS[1], 'cron', ARGV[2], 'timezone', ARGV[3], 'next_at', ARGV[4], 'state', state)
redis.call('HDEL', KEYS[1], 'finished_at', 'last_error')
redis.call('ZREM', KEYS[3], ARGV[1])
if state == 'active' then
  redis.call('ZADD', KEYS[2], ARGV[4], ARGV[1])
else
  redis.call('ZREM', KEYS[2], ARGV[1])
end
return 1
