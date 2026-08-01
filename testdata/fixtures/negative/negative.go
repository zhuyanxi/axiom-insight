package negative

import (
	"context"
	"net/http"
)

type Router struct{}

type localHTTPClient struct{}
type localRedisClient struct{}
type localProducer struct{}
type localConsumer struct{}
type localDB struct{}
type localRPCClient struct{}
type localCron struct{}
type fakeServer struct{}
type message struct{}
type reply struct{}

func Handler(http.ResponseWriter, *http.Request)                           {}
func (Router) HandleFunc(string, func(http.ResponseWriter, *http.Request)) {}
func (localHTTPClient) Get(string)                                         {}
func (localRedisClient) Get(context.Context, string)                       {}
func (localProducer) SendMessage(*message)                                 {}
func (localConsumer) ConsumePartition(string, int32, int64)                {}
func (localDB) Query(string)                                               {}
func (localRPCClient) SayHello(context.Context) (reply, error)             { return reply{}, nil }
func (localCron) AddFunc(string, func())                                   {}
func (fakeServer) SayHello()                                               {}
func RegisterFake(interface{}, interface{ SayHello() })                    {}
func Job()                                                                 {}

func Exercise() {
	Router{}.HandleFunc("/health", Handler)
	localHTTPClient{}.Get("https://orders.example.test")
	localRedisClient{}.Get(context.Background(), "orders")
	localProducer{}.SendMessage(&message{})
	localConsumer{}.ConsumePartition("orders", 0, 0)
	localDB{}.Query("SELECT 1")
	localRPCClient{}.SayHello(context.Background())
	scheduler := localCron{}
	scheduler.AddFunc("@hourly", Job)
	RegisterFake(nil, fakeServer{})
}
