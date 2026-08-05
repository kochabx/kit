local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
local ids = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', now, 'LIMIT', 0, ARGV[2])
local moved = 0
for _, id in ipairs(ids) do
  local job = ARGV[1] .. id
  if redis.call('HGET', job, 'state') == 'scheduled' then
    redis.call('HSET', job, 'state', 'ready')
    redis.call('XADD', KEYS[2], '*', 'job_id', id)
    moved = moved + 1
  end
  redis.call('ZREM', KEYS[1], id)
end
return moved
