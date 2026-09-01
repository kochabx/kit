if redis.call('HGET',KEYS[1],'lease_token')~=ARGV[1] then return 0 end
if redis.call('HGET',KEYS[1],'cancel_requested')=='1' then return -1 end
return 1
