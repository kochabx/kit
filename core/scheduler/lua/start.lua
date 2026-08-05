local state = redis.call('HGET', KEYS[1], 'state')
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
if state ~= 'ready' then
  if state ~= 'running' then return -1 end
  local lease_until = tonumber(redis.call('HGET', KEYS[1], 'lease_until') or '0')
  if lease_until > now then return 0 end
end
local attempt = tonumber(redis.call('HGET', KEYS[1], 'attempt') or '0') + 1
redis.call('HSET', KEYS[1], 'state', 'running', 'attempt', attempt,
  'lease_token', ARGV[1], 'worker_id', ARGV[2], 'started_at', now,
  'lease_until', now + tonumber(ARGV[3]))
redis.call('ZADD',KEYS[2],now,redis.call('HGET',KEYS[1],'id'))
return 1
