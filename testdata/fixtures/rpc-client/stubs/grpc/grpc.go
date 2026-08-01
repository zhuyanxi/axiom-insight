package grpc

type ClientConn struct{}
type CallOption struct{}
type DialOption struct{}

func Dial(string, ...DialOption) (*ClientConn, error) { return nil, nil }
