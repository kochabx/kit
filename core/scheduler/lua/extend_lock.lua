-- extend_lock.lua
-- 中文说明:
-- 扩展分布式锁的TTL（确保只有持有锁的客户端才能扩展）
-- KEYS[1]: lock key
-- ARGV[1]: expected value
-- ARGV[2]: TTL in seconds
-- Returns: 1 if extended, 0 if not (value mismatch or lock not exists)

if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("expire", KEYS[1], ARGV[2])
else
    return 0
end
