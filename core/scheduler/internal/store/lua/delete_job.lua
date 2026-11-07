if redis.call('EXISTS', KEYS[1]) == 0 then
  return 0
end
local state = redis.call('HGET', KEYS[1], 'state')
if state ~= 'dead' and state ~= 'succeeded' and state ~= 'cancelled' and state ~= 'expired' then
  return -1
end
if redis.call('HGET', KEYS[1], 'queue') ~= ARGV[2] then
  return -1
end
redis.call('ZREM', KEYS[2], ARGV[1])
redis.call('ZREM', KEYS[3], ARGV[1])
redis.call('ZREM', KEYS[4], ARGV[1])
return redis.call('DEL', KEYS[1])
