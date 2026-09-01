if redis.call('EXISTS',KEYS[1])==0 then return 0 end
redis.call('HSET',KEYS[1],'state',ARGV[2])
if ARGV[2]=='active' then
  redis.call('HDEL',KEYS[1],'finished_at')
  redis.call('ZREM',KEYS[3],ARGV[1])
  redis.call('HSET',KEYS[1],'next_at',ARGV[3])
  redis.call('ZADD',KEYS[2],ARGV[3],ARGV[1])
else
  redis.call('ZREM',KEYS[2],ARGV[1])
end
return 1
