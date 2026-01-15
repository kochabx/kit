local key = KEYS[1]
local window = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

local start_time = now - window
local timestamps = redis.call('LRANGE', key, 0, -1)
local index = 0

for i, timestamp in ipairs(timestamps) do
    if tonumber(timestamp) >= start_time then
        index = i
        break
    end
end

if index > 1 then
    redis.call('LTRIM', key, index - 1, -1)
end

local count = redis.call('LLEN', key)

if count < limit then
    redis.call('RPUSH', key, now)
    redis.call('EXPIRE', key, window)
    return 1
else
    return 0
end
