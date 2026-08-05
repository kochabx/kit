if redis.call('EXISTS', KEYS[1]) == 0 then return -1 end
local state = redis.call('HGET', KEYS[1], 'state')
if state == 'running' then redis.call('HSET', KEYS[1], 'cancel_requested', 1); return 2 end
if state ~= 'scheduled' and state ~= 'ready' then return 0 end
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
redis.call('HSET', KEYS[1], 'state', 'cancelled', 'finished_at', now)
redis.call('ZREM', KEYS[2], redis.call('HGET', KEYS[1], 'id'))
redis.call('PEXPIRE', KEYS[1], ARGV[1])
return 1
