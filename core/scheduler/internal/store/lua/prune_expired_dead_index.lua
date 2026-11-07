local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local ids = redis.call('ZRANGE', KEYS[1], '-inf', now, 'BYSCORE', 'LIMIT', 0, ARGV[1])
if #ids > 0 then
  redis.call('ZREM', KEYS[1], unpack(ids))
end
return #ids
