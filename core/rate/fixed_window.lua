-- KEYS[1]: window state key
-- ARGV: window (seconds), limit, n
-- Returns: allowed, limit, remaining, retry_after_ms, reset_at_ms
local window_ms = tonumber(ARGV[1]) * 1000
local limit = tonumber(ARGV[2])
local n = tonumber(ARGV[3])

local redis_time = redis.call("TIME")
local now_ms = redis_time[1] * 1000 + math.floor(redis_time[2] / 1000)
local window_id = math.floor(now_ms / window_ms)
local reset_at = (window_id + 1) * window_ms

local state = redis.call("HMGET", KEYS[1], "window", "count")
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

redis.call("HSET", KEYS[1], "window", window_id, "count", count)
redis.call("PEXPIREAT", KEYS[1], reset_at + window_ms)

local retry_after_ms = 0
if allowed == 0 then
    retry_after_ms = math.max(1, reset_at - now_ms)
end
return {allowed, limit, limit - count, retry_after_ms, reset_at}
