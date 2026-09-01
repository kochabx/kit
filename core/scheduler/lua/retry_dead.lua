if redis.call('HGET',KEYS[1],'state')~='dead' then return 0 end
local t=redis.call('TIME')
local now=t[1]*1000+math.floor(t[2]/1000)
redis.call('HSET',KEYS[1],'state','scheduled','run_at',now,'last_error','','attempt',0)
redis.call('HDEL',KEYS[1],'finished_at')
redis.call('ZREM',KEYS[3],ARGV[1])
redis.call('ZADD',KEYS[2],now,ARGV[1])
redis.call('PERSIST',KEYS[1])
return 1
