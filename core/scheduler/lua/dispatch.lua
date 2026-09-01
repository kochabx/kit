local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
local ids = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', now, 'LIMIT', 0, ARGV[2])
for _, id in ipairs(ids) do
  local job = ARGV[1] .. id
  if redis.call('HGET', job, 'state') == 'scheduled' then
    local expires_at = tonumber(redis.call('HGET',job,'expires_at') or '0')
    if expires_at > 0 and expires_at <= now then
      redis.call('HSET',job,'state','expired','finished_at',now)
      redis.call('PEXPIRE',job,ARGV[3])
    else
      local run_at = tonumber(redis.call('HGET',job,'run_at') or '0')
      if run_at <= now then
        redis.call('HSET', job, 'state', 'ready')
        redis.call('XADD', KEYS[2], '*', 'job_id', id)
      else
        redis.call('ZADD',KEYS[1],run_at,id)
      end
    end
  end
  if redis.call('ZSCORE',KEYS[1],id) and tonumber(redis.call('ZSCORE',KEYS[1],id)) <= now then
    redis.call('ZREM', KEYS[1], id)
  end
end
return #ids
