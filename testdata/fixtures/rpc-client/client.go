package rpcclientfixture

import (
	"context"

	"example.com/api"
	"google.golang.org/grpc"
)

func Run() {
	connection, _ := grpc.Dial("dns:///orders")
	client := api.NewGreeterClient(connection)
	client.SayHello(context.Background(), &api.HelloRequest{})
}
