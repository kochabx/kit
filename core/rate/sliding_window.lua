-- KEYS[1]: weighted event sorted set
-- ARGV: window (milliseconds), limit, n, unique id
-- Member format is "<weight>:<id>"; one member is stored per allowed call.
-- Returns: allowed, limit, remaining, retry_after_ms, reset_at_ms
local window_ms = assert(tonumber(ARGV[1]), 'rate: invalid window')
local limit = assert(tonumber(ARGV[2]), 'rate: invalid limit')
local n = assert(tonumber(ARGV[3]), 'rate: invalid n')
local unique_id = ARGV[4]

local redis_time = redis.call('TIME')
local now_ms = redis_time[1] * 1000 + math.floor(redis_time[2] / 1000)
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now_ms - window_ms)

local events = redis.call('ZRANGE', KEYS[1], 0, -1, 'WITHSCORES')
---@type number
local total = 0
---@type number
local last_score = now_ms
for i = 1, #events, 2 do
  local weight = tonumber(string.match(events[i], '^(%d+):'))
  if not weight then
    return redis.error_reply('rate: malformed sliding-window member')
  end
  total = total + weight
  last_score = assert(tonumber(events[i + 1]), 'rate: invalid event score')
end

local allowed = 0
if n <= limit and total + n <= limit then
  redis.call('ZADD', KEYS[1], now_ms, string.format('%.0f', n) .. ':' .. unique_id)
  total = total + n
  last_score = now_ms
  allowed = 1
end

---@type number
local retry_after_ms = 0
if allowed == 0 then
  if n > limit then
    retry_after_ms = math.max(1, window_ms)
  else
    ---@type number
    local released = 0
    for i = 1, #events, 2 do
      released = released
        + assert(
          tonumber(string.match(events[i], '^(%d+):')),
          'rate: malformed sliding-window member'
        )
      if total - released + n <= limit then
        retry_after_ms = math.max(
          1,
          assert(tonumber(events[i + 1]), 'rate: invalid event score') + window_ms - now_ms
        )
        break
      end
    end
  end
end

local reset_at = last_score + window_ms
redis.call('PEXPIRE', KEYS[1], math.max(1, reset_at - now_ms + window_ms))
return { allowed, limit, limit - total, retry_after_ms, reset_at }
