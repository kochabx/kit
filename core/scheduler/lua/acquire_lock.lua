-- acquire_lock.lua
-- 获取分布式锁
-- KEYS[1]: 锁的key
-- ARGV[1]: 锁的value (worker_id)
-- ARGV[2]: TTL (秒)
-- 返回: 1表示成功, 0表示失败

local lockKey = KEYS[1]
local workerID = ARGV[1]
local ttl = tonumber(ARGV[2])

-- SET NX EX 原子操作
local result = redis.call('SET', lockKey, workerID, 'NX', 'EX', ttl)

if result then
    return 1
else
    return 0
end
