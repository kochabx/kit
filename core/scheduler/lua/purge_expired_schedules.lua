local t=redis.call('TIME')
local now=t[1]*1000+math.floor(t[2]/1000)
local ids=redis.call('ZRANGEBYSCORE',KEYS[1],'-inf',now,'LIMIT',0,ARGV[2])
for _,id in ipairs(ids) do
  redis.call('DEL',ARGV[1]..id)
  redis.call('ZREM',KEYS[2],id)
  redis.call('ZREM',KEYS[3],id)
  redis.call('ZREM',KEYS[1],id)
end
return #ids
