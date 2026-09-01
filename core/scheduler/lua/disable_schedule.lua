if redis.call('HGET',KEYS[1],'next_at')~=ARGV[2] then return 0 end
local t=redis.call('TIME')
local now=t[1]*1000+math.floor(t[2]/1000)
redis.call('HSET',KEYS[1],'state','invalid','last_error',ARGV[3],'finished_at',now)
redis.call('ZREM',KEYS[2],ARGV[1])
redis.call('ZADD',KEYS[3],now+tonumber(ARGV[4]),ARGV[1])
return 1
