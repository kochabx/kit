local unique = KEYS[3]
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
if ARGV[6] ~= '' then
  local existing = redis.call('GET', unique)
  if existing then
    if redis.call('EXISTS', ARGV[8] .. existing) == 1 then return {0, existing, redis.call('HGET',ARGV[8]..existing,'state')} end
    redis.call('DEL', unique)
  end
end
if redis.call('EXISTS', KEYS[1]) == 1 then return {0, ARGV[1], redis.call('HGET',KEYS[1],'state')} end
local run_at = tonumber(ARGV[4])
if run_at <= 0 then run_at = now + tonumber(ARGV[9]) end
local state = 'scheduled'
if run_at <= now then state = 'ready' end
redis.call('HSET', KEYS[1],
  'id', ARGV[1], 'type', ARGV[2], 'payload', ARGV[3],
  'state', state, 'run_at', run_at,
  'created_at', now, 'attempt', 0, 'max_attempts', ARGV[5],
  'unique_key', ARGV[6], 'definition', ARGV[10])
if state == 'ready' then redis.call('XADD',KEYS[4],'*','job_id',ARGV[1]) else redis.call('ZADD', KEYS[2], run_at, ARGV[1]) end
if ARGV[6] ~= '' then
  local ttl = tonumber(ARGV[7])
  if ttl > 0 then redis.call('PSETEX', unique, ttl, ARGV[1]) else redis.call('SET', unique, ARGV[1]) end
end
return {1, ARGV[1], state, run_at, now}
