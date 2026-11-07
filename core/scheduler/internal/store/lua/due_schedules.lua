local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local ids = redis.call('ZRANGE', KEYS[1], '-inf', now, 'BYSCORE', 'LIMIT', 0, ARGV[1])
local result = { now, #ids }
for _, id in ipairs(ids) do
  local key = ARGV[2] .. id
  if redis.call('EXISTS', key) == 0 then
    redis.call('ZREM', KEYS[1], id)
  else
    local fields = redis.call('HMGET', key, 'cron', 'timezone', 'next_at', 'state', 'last_run_at', 'queue')
    if fields[6] ~= ARGV[3] then
      redis.call('ZREM', KEYS[1], id)
    else
      table.insert(result, { id, fields[1] or '', fields[2] or '', fields[3] or '0', fields[4] or '', fields[5] or '0' })
    end
  end
end
return result
