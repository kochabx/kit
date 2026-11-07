local pending = redis.call('XPENDING', KEYS[1], ARGV[1], '-', '+', 1, ARGV[2])
if #pending > 0 then
  return 0
end
redis.call('XGROUP', 'DELCONSUMER', KEYS[1], ARGV[1], ARGV[2])
return 1
