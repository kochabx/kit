-- KEYS[1]: window state key
-- ARGV: window (milliseconds), limit, n
-- Returns: allowed, limit, remaining, retry_after_ms, reset_at_ms
local window_ms = assert(tonumber(ARGV[1]), 'rate: invalid window')
local limit = assert(tonumber(ARGV[2]), 'rate: invalid limit')
local n = assert(tonumber(ARGV[3]), 'rate: invalid n')

local redis_time = redis.call('TIME')
local now_ms = redis_time[1] * 1000 + math.floor(redis_time[2] / 1000)
local window_id = math.floor(now_ms / window_ms)
local reset_at = (window_id + 1) * window_ms

local state = redis.call('HMGET', KEYS[1], 'window', 'count')
local stored_window = tonumber(state[1])
local count = tonumber(state[2]) or 0
if stored_window ~= window_id then
  count = 0
end

local allowed = 0
if n <= limit and count + n <= limit then
  count = count + n
  allowed = 1
end

redis.call('HSET', KEYS[1], 'window', window_id, 'count', count)
redis.call('PEXPIREAT', KEYS[1], reset_at + window_ms)

---@type number
local retry_after_ms = 0
if allowed == 0 then
  retry_after_ms = math.max(1, reset_at - now_ms)
end
return { allowed, limit, limit - count, retry_after_ms, reset_at }
