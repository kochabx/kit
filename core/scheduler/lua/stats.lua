local stream=redis.call('XLEN',KEYS[2])
local pending=0
local p=redis.pcall('XPENDING',KEYS[2],ARGV[1])
if type(p)=='table' then pending=tonumber(p[1]) or 0 end
local ready=stream-pending
if ready<0 then ready=0 end
return {redis.call('ZCARD',KEYS[1]),ready,pending,redis.call('ZCARD',KEYS[3]),redis.call('ZCARD',KEYS[4]),redis.call('ZCARD',KEYS[5])}
