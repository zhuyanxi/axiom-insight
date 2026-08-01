module example.com/p0-grpc

go 1.26.1

require example.com/api v0.0.0

replace example.com/api => ./stubs/api
