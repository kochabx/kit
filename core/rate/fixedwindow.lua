-- Fixed Window Rate Limiter
-- KEYS[1]: counter key
-- ARGV[1]: window_sec (window size in seconds)
-- ARGV[2]: limit (max requests per window)
-- ARGV[3]: requested (number of requests)
-- Returns: {allowed (0/1), current_count (count after operation)}

local key = KEYS[1]
local window_sec = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local requested = tonumber(ARGV[3])

local current = tonumber(redis.call("GET", key)) or 0

if current + requested <= limit then
    local new_count = redis.call("INCRBY", key, requested)
    -- Only set expiry on first write (TTL == -1 means no expiry)
    if redis.call("TTL", key) == -1 then
        redis.call("EXPIRE", key, window_sec)
    end
    return {1, new_count}
else
    return {0, current}
end
