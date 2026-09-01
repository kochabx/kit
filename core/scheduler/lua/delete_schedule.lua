local n=redis.call('DEL',KEYS[1])
redis.call('ZREM',KEYS[2],ARGV[1])
redis.call('ZREM',KEYS[3],ARGV[1])
redis.call('ZREM',KEYS[4],ARGV[1])
return n
