package mcp

import "context"

type Transport interface {
	Send(ctx context.Context, msg []byte) error
	Receive(ctx context.Context) ([]byte, error)
	Close() error
}
