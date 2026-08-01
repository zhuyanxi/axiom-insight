package redisfixture

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type localClient struct{}

func (localClient) Get(context.Context, string) {}

func Run(client *redis.Client) {
	client.Get(context.Background(), "orders")
	client.Set(context.Background(), "orders", "value", time.Minute)
	localClient{}.Get(context.Background(), "not-redis")
}
