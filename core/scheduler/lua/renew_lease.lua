if redis.call('HGET',KEYS[1],'lease_token')~=ARGV[1] then return 0 end
if redis.call('HGET',KEYS[1],'cancel_requested')=='1' then return -1 end
local t=redis.call('TIME')
local now=t[1]*1000+math.floor(t[2]/1000)
redis.call('HSET',KEYS[1],'lease_until',now+tonumber(ARGV[2]))
return 1
