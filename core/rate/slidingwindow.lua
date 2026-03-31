-- Sliding Window Rate Limiter (ZSET-based, millisecond precision)
-- KEYS[1]: sorted set key
-- ARGV[1]: window_ms (window size in ms)
-- ARGV[2]: limit (max requests per window)
-- ARGV[3]: now_ms (current timestamp in ms)
-- ARGV[4]: requested (number of requests)
-- ARGV[5]: unique_id (unique prefix to avoid ZSET member collision)
-- Returns: {allowed (0/1), current_count (requests in current window)}

local key = KEYS[1]
local window_ms = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local now_ms = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])
local unique_id = ARGV[5]

-- Remove expired entries outside the window
local window_start = now_ms - window_ms
redis.call("ZREMRANGEBYSCORE", key, "-inf", window_start)

-- Count requests in the current window
local count = redis.call("ZCARD", key)

-- Check if there is enough quota
if count + requested <= limit then
    -- Add requested entries, each with a unique member to avoid collision
    for i = 1, requested do
        redis.call("ZADD", key, now_ms, unique_id .. ":" .. i)
    end
    redis.call("PEXPIRE", key, window_ms)
    return {1, count + requested}
else
    -- Only refresh TTL, do not add entries
    redis.call("PEXPIRE", key, window_ms)
    return {0, count}
end
