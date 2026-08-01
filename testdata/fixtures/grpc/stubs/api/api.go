package api

type Registrar interface{}

type GreeterServer interface {
	SayHello()
}

func RegisterGreeterServer(Registrar, GreeterServer) {}
