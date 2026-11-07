local receipt = redis.call('GET', KEYS[5])
if receipt then
 if receipt ~= ARGV[16] then return { -1, ARGV[1], '' } end
 return { 0, ARGV[1], redis.call('HGET', KEYS[1], 'state') or '' }
end
local unique = KEYS[3]
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
if ARGV[6] ~= '' then
  local existing = redis.call('GET', unique)
  if existing then
    return { 0, existing, redis.call('HGET', ARGV[8] .. existing, 'state') or '' }
  end
end
if redis.call('EXISTS', KEYS[1]) == 1 then
  return { -1, ARGV[1], redis.call('HGET', KEYS[1], 'state') }
end
local run_at = tonumber(ARGV[4])
if run_at <= 0 then
  run_at = now + tonumber(ARGV[9])
end
local expires_at = tonumber(ARGV[11])
if expires_at <= 0 and tonumber(ARGV[12]) > 0 then
  expires_at = now + tonumber(ARGV[12])
end
local state = 'scheduled'
if run_at <= now then
  state = 'ready'
end
redis.call('HSET', KEYS[1],
  'queue', ARGV[13], 'id', ARGV[1], 'task', ARGV[2], 'payload', ARGV[3],
  'state', state, 'run_at', run_at,
  'created_at', now, 'attempt', 0, 'max_attempts', ARGV[5],
  'incarnation', ARGV[18], 'unique_key', ARGV[6], 'fingerprint', ARGV[10], 'version', ARGV[14], 'policy', ARGV[15], 'failure_count', 0, 'recovery_count', 0)
if expires_at > 0 then
  redis.call('HSET', KEYS[1], 'expires_at', expires_at)
end
if state == 'ready' then
  redis.call('XADD', KEYS[4], 'IDMP', 'scheduler', ARGV[18] .. ':0:0', '*', 'job_id', ARGV[1])
else
  local schedule_score = run_at
  if expires_at > 0 and expires_at < schedule_score then
    schedule_score = expires_at
  end
  redis.call('ZADD', KEYS[2], schedule_score, ARGV[1])
end
if ARGV[6] ~= '' then
  local ttl = tonumber(ARGV[7])
  if ttl > 0 then
    redis.call('SET', unique, ARGV[1], 'PX', ttl)
  else
    redis.call('SET', unique, ARGV[1])
  end
end
redis.call('SET', KEYS[5], ARGV[16], 'PX', ARGV[17])
return { 1, ARGV[1], state, run_at, now, expires_at }
