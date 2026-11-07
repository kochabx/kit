local replay = read_receipt(KEYS[4], ARGV[7])
if replay then return replay end
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
if not owns_lease(KEYS[1], ARGV[1], now) then return 0 end
local id = redis.call('HGET', KEYS[1], 'id')
if redis.call('HGET', KEYS[1], 'cancel_requested') == '1' then
  append_attempt(KEYS[1], KEYS[5], ARGV[8], ARGV[9], now, 'cancelled', '')
  redis.call('HSET', KEYS[1], 'state', 'cancelled', 'finished_at', now)
  redis.call('HDEL', KEYS[1], 'lease_token', 'lease_until', 'cancel_requested')
  ack_delivery(KEYS[2], ARGV[2], ARGV[3])
  redis.call('ZREM', KEYS[3], id)
  if tonumber(ARGV[5]) > 0 then
    redis.call('PEXPIRE', KEYS[1], ARGV[5])
  else
    redis.call('DEL', KEYS[1])
  end
  return remember_result(KEYS[4], ARGV[7], ARGV[6], 2)
end
append_attempt(KEYS[1], KEYS[5], ARGV[8], ARGV[9], now, 'succeeded', '')
redis.call('HSET', KEYS[1], 'state', 'succeeded', 'finished_at', now)
redis.call('HDEL', KEYS[1], 'lease_token', 'lease_until')
ack_delivery(KEYS[2], ARGV[2], ARGV[3])
redis.call('ZREM', KEYS[3], id)
if tonumber(ARGV[4]) > 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[4])
else
  redis.call('DEL', KEYS[1])
end
return remember_result(KEYS[4], ARGV[7], ARGV[6], 1)
