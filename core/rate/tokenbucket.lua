-- Token Bucket Rate Limiter (millisecond precision)
-- KEYS[1]: bucket hash key
-- ARGV[1]: capacity (max tokens)
-- ARGV[2]: rate (tokens refilled per second)
-- ARGV[3]: now_ms (current timestamp in ms)
-- ARGV[4]: requested (tokens requested)
-- Returns: {allowed (0/1), remaining (tokens left)}

local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local rate_per_sec = tonumber(ARGV[2])
local now_ms = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

-- TTL = 2x full refill time (seconds), minimum 1s
local fill_time_sec = capacity / rate_per_sec
local ttl = math.max(1, math.ceil(fill_time_sec * 2))

-- Read last state from HASH
local data = redis.call("HMGET", key, "tokens", "ts")
local last_tokens = tonumber(data[1]) or capacity
local last_ts = tonumber(data[2]) or 0

-- Calculate elapsed time and refill tokens (ms to sec conversion)
local delta_ms = math.max(0, now_ms - last_ts)
local filled_tokens = math.min(capacity, last_tokens + (delta_ms * rate_per_sec / 1000))

-- Determine whether to allow
local allowed = 0
local new_tokens = filled_tokens
if filled_tokens >= requested then
    new_tokens = filled_tokens - requested
    allowed = 1
end

-- Atomically write back to HASH and set expiry
redis.call("HSET", key, "tokens", new_tokens, "ts", now_ms)
redis.call("EXPIRE", key, ttl)

return {allowed, math.floor(new_tokens)}
