redis.call('XACKDEL', KEYS[1], ARGV[1], 'DELREF', 'IDS', 1, ARGV[2])
return 1
