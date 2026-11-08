-- Shared by execution transitions. Receipt replay happens before ownership checks.
local function read_receipt(key, signature)
  local receipt = redis.call('HMGET', key, 'signature', 'result')
  if not receipt[1] then
    return nil
  end
  if receipt[1] ~= signature then
    return -3
  end
  return tonumber(receipt[2])
end

local function remember_result(key, signature, ttl, result)
  redis.call('HSET', key, 'signature', signature, 'result', result)
  redis.call('PEXPIRE', key, ttl)
  return result
end

local function owns_lease(job, token, now)
  return redis.call('HGET', job, 'lease_token') == token
    and tonumber(redis.call('HGET', job, 'lease_until') or '0') > now
end

local function ack_delivery(stream, group, message)
  redis.call('XACKDEL', stream, group, 'DELREF', 'IDS', 1, message)
end
