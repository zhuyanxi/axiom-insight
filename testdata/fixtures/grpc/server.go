package grpcfixture

import "example.com/api"

type server struct{}

func (server) SayHello() {}

func Register(registrar api.Registrar) {
	api.RegisterGreeterServer(registrar, &server{})
}
