local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
local stream = redis.call('XLEN', KEYS[2])
local pending = 0
local idle = 0
local summary = redis.pcall('XPENDING', KEYS[2], ARGV[1])
if summary.err then
  if not string.find(summary.err, 'NOGROUP', 1, true) then
    return redis.error_reply(summary.err)
  end
else
  pending = tonumber(summary[1]) or 0
  if pending > 0 then
    local head = redis.call('XPENDING', KEYS[2], ARGV[1], '-', '+', 1)
    if #head > 0 then
      idle = tonumber(head[1][3])
    end
  end
end
local oldest = redis.call('ZRANGE', KEYS[1], 0, 0, 'WITHSCORES')
local lag = 0
if #oldest > 0 then
  lag = math.max(0, now - tonumber(oldest[2]))
end
return {
  redis.call('ZCARD', KEYS[1]), math.max(0, stream - pending), pending,
  redis.call('ZCARD', KEYS[3]), redis.call('ZCARD', KEYS[4]),
  redis.call('ZCARD', KEYS[5]), lag, idle
}
