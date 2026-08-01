module example.com/p0-rpc-client

go 1.26.1

require (
	google.golang.org/grpc v0.0.0
	example.com/api v0.0.0
)

replace google.golang.org/grpc => ./stubs/grpc
replace example.com/api => ./stubs/api
