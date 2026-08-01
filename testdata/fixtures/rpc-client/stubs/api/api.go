package api

import (
	"context"

	"google.golang.org/grpc"
)

type HelloRequest struct{}
type HelloReply struct{}

type GreeterClient interface {
	SayHello(context.Context, *HelloRequest, ...grpc.CallOption) (*HelloReply, error)
}

type greeterClient struct{}

func (*greeterClient) SayHello(context.Context, *HelloRequest, ...grpc.CallOption) (*HelloReply, error) {
	return nil, nil
}

func NewGreeterClient(*grpc.ClientConn) GreeterClient { return &greeterClient{} }
