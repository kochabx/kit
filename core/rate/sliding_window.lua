-- KEYS[1]: weighted event sorted set
-- ARGV: window (seconds), limit, n, unique id
-- Member format is "<weight>:<id>"; one member is stored per allowed call.
-- Returns: allowed, limit, remaining, retry_after_ms, reset_at_ms
local window_ms = tonumber(ARGV[1]) * 1000
local limit = tonumber(ARGV[2])
local n = tonumber(ARGV[3])
local unique_id = ARGV[4]

local redis_time = redis.call("TIME")
local now_ms = redis_time[1] * 1000 + math.floor(redis_time[2] / 1000)
redis.call("ZREMRANGEBYSCORE", KEYS[1], "-inf", now_ms - window_ms)

local events = redis.call("ZRANGE", KEYS[1], 0, -1, "WITHSCORES")
local total = 0
local last_score = now_ms
for i = 1, #events, 2 do
    local weight = tonumber(string.match(events[i], "^(%d+):"))
    if not weight then
        return redis.error_reply("rate: malformed sliding-window member")
    end
    total = total + weight
    last_score = tonumber(events[i + 1])
end

local allowed = 0
if n <= limit and total + n <= limit then
    redis.call("ZADD", KEYS[1], now_ms, tostring(n) .. ":" .. unique_id)
    total = total + n
    last_score = now_ms
    allowed = 1
end

local retry_after_ms = 0
if allowed == 0 then
    if n > limit then
        retry_after_ms = math.max(1, window_ms)
    else
        local released = 0
        for i = 1, #events, 2 do
            released = released + tonumber(string.match(events[i], "^(%d+):"))
            if total - released + n <= limit then
                retry_after_ms = math.max(1, tonumber(events[i + 1]) + window_ms - now_ms)
                break
            end
        end
    end
end

local reset_at = last_score + window_ms
redis.call("PEXPIRE", KEYS[1], math.max(1, reset_at - now_ms + window_ms))
return {allowed, limit, limit - total, retry_after_ms, reset_at}
