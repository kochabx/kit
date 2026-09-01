if redis.call('EXISTS',KEYS[1])==0 then return -1 end
local t=redis.call('TIME')
local now=t[1]*1000+math.floor(t[2]/1000)
redis.call('HSET',KEYS[1],'state','cancelled','finished_at',now)
redis.call('ZREM',KEYS[2],ARGV[1])
redis.call('ZADD',KEYS[3],now+tonumber(ARGV[2]),ARGV[1])
return 1
