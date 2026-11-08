-- Used only after ownership checks, in the same transition as the state update.
local function append_attempt(job, history, limit, ttl, now, outcome, message)
  if tonumber(limit) <= 0 then
    return
  end
  local fields = redis.call('HMGET', job, 'id', 'lease_token', 'worker_id', 'attempt', 'started_at')
  if not fields[2] then
    return
  end
  redis.call(
    'LPUSH',
    history,
    cjson.encode({
      job_id = fields[1],
      token = fields[2],
      worker_id = fields[3] or '',
      number = tonumber(fields[4]) or 0,
      started_at = tonumber(fields[5]) or 0,
      finished_at = now,
      outcome = outcome,
      error = message or '',
    })
  )
  redis.call('LTRIM', history, 0, tonumber(limit) - 1)
  redis.call('PEXPIRE', history, ttl)
end
