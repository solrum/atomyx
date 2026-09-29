package iossimdriver

import (
	"context"
	"fmt"
	"net"
)

// Dialer opens the byte-stream connection HidClient upgrades to a
// WebSocket. Production code dials a real loopback TCP connection to
// the atomyx-sim-hid helper's handshake port; a test substitutes one
// end of an in-memory net.Pipe so the WebSocket handshake and frame
// codec run against a fake helper with no real socket involved.
type Dialer interface {
	Dial(ctx context.Context, port int) (net.Conn, error)
}

// tcpDialer is the production Dialer, connecting to the helper on
// loopback because the helper only ever listens on 127.0.0.1.
type tcpDialer struct{}

// NewDialer returns the production Dialer that opens a real TCP
// connection to 127.0.0.1:port.
func NewDialer() Dialer { return tcpDialer{} }

func (tcpDialer) Dial(ctx context.Context, port int) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
}
