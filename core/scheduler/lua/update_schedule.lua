if redis.call('EXISTS',KEYS[1])==0 then return 0 end
redis.call('HSET',KEYS[1],'cron',ARGV[2],'timezone',ARGV[3],'next_at',ARGV[4],'state','active')
redis.call('HDEL',KEYS[1],'finished_at')
redis.call('ZREM',KEYS[3],ARGV[1])
redis.call('ZADD',KEYS[2],ARGV[4],ARGV[1])
return 1
