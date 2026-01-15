local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

local fill_time = capacity / rate
local ttl = math.floor(fill_time * 2)

local last_tokens = tonumber(redis.call("get", key)) or capacity
local last_refreshed = tonumber(redis.call("get", key .. ":ts")) or 0

local delta = math.max(0, now - last_refreshed)
local filled_tokens = math.min(capacity, last_tokens + (delta * rate))

local allowed = filled_tokens >= requested
local new_tokens = allowed and (filled_tokens - requested) or filled_tokens

redis.call("setex", key, ttl, new_tokens)
redis.call("setex", key .. ":ts", ttl, now)

return allowed
