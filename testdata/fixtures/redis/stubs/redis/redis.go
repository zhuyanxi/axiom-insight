package redis

import (
	"context"
	"time"
)

type Client struct{}
type Cmd struct{}

func (*Client) Get(context.Context, string) *Cmd                             { return nil }
func (*Client) Set(context.Context, string, interface{}, time.Duration) *Cmd { return nil }
