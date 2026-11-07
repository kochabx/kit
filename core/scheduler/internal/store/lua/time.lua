local clock = redis.call('TIME')
return clock[1] * 1000 + math.floor(clock[2] / 1000)
