-- release_lock.lua
-- 释放分布式锁（确保只有持有锁的客户端才能释放）
-- KEYS[1]: 锁的key
-- ARGV[1]: 锁的value (worker_id)
-- 返回: 1表示成功, 0表示失败（锁不存在或value不匹配）

local lockKey = KEYS[1]
local workerID = ARGV[1]

-- 获取当前锁的值
local currentValue = redis.call('GET', lockKey)

-- 如果锁存在且值匹配，则删除
if currentValue == workerID then
    redis.call('DEL', lockKey)
    return 1
else
    return 0
end
