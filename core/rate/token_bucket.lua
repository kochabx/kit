-- KEYS[1]: bucket key
-- ARGV: rate (tokens per second), burst, n
-- Returns: allowed, limit, remaining, retry_after_ms, reset_at_ms
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local n = tonumber(ARGV[3])

local redis_time = redis.call("TIME")
local now_ms = redis_time[1] * 1000 + math.floor(redis_time[2] / 1000)
local max_balance = burst

local state = redis.call("HMGET", KEYS[1], "balance", "updated_at")
local balance = tonumber(state[1]) or max_balance
local updated_at = tonumber(state[2]) or now_ms
local elapsed = math.max(0, now_ms - updated_at)
balance = math.min(max_balance, balance + elapsed * rate / 1000)

local allowed = 0
if n <= burst and balance >= n then
    balance = balance - n
    allowed = 1
end

redis.call("HSET", KEYS[1], "balance", balance, "updated_at", now_ms)
local full_after_ms = math.ceil((max_balance - balance) * 1000 / rate)
local ttl_ms = math.max(1, full_after_ms + 1000)
redis.call("PEXPIRE", KEYS[1], ttl_ms)

local retry_after_ms = 0
if allowed == 0 then
    if n > burst then
        -- This request can never fit. Return a positive, bounded hint while
        -- leaving the decision deterministic for callers.
        retry_after_ms = 1000
    else
        retry_after_ms = math.max(1, math.ceil((n - balance) * 1000 / rate))
    end
end

local remaining = math.floor(balance)
return {allowed, burst, remaining, retry_after_ms, now_ms + full_after_ms}
