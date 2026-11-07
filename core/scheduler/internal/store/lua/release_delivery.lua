local rows = redis.call('XPENDING', KEYS[1], ARGV[1], ARGV[2], ARGV[2], 1)
if #rows == 0 or rows[1][2] ~= ARGV[3] then return 0 end
return redis.call('XNACK', KEYS[1], ARGV[1], 'SILENT', 'IDS', 1, ARGV[2])
