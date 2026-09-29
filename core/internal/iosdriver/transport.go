package iosdriver

import (
	"context"

	"dev.atomyx/internal/transport"
)

// Conn is the narrow slice of transport.TCPClient IosDriver depends
// on. Declaring it here, rather than depending on *transport.TCPClient
// directly, lets a test substitute a fake that implements only the
// operations this adapter calls.
type Conn interface {
	Connect(ctx context.Context) error
	Close() error
	IsConnected() bool
	CallInto(ctx context.Context, cmdType string, args map[string]any, out any) error
}

var _ Conn = (*transport.TCPClient)(nil)
