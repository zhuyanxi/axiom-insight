module example.com/p0-redis

go 1.26.1

require github.com/redis/go-redis/v9 v9.0.0

replace github.com/redis/go-redis/v9 => ./stubs/redis
