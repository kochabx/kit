redis.call('ZREM',KEYS[2],ARGV[1])
return redis.call('DEL',KEYS[1])
