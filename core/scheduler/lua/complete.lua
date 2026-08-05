if redis.call('HGET', KEYS[1], 'lease_token') ~= ARGV[1] then return 0 end
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
if redis.call('HGET',KEYS[1],'cancel_requested') == '1' then
  redis.call('HSET',KEYS[1],'state','cancelled','finished_at',now)
  redis.call('HDEL',KEYS[1],'lease_token','lease_until','cancel_requested')
  redis.call('PEXPIRE',KEYS[1],ARGV[4]);redis.call('XACK',KEYS[2],ARGV[2],ARGV[3]);redis.call('XDEL',KEYS[2],ARGV[3]);redis.call('ZREM',KEYS[3],redis.call('HGET',KEYS[1],'id'));return 2
end
redis.call('HSET', KEYS[1], 'state', 'succeeded', 'finished_at', now)
redis.call('HDEL', KEYS[1], 'lease_token', 'lease_until')
redis.call('PEXPIRE', KEYS[1], ARGV[4])
redis.call('XACK', KEYS[2], ARGV[2], ARGV[3])
redis.call('XDEL', KEYS[2], ARGV[3])
redis.call('ZREM',KEYS[3],redis.call('HGET',KEYS[1],'id'))
return 1
