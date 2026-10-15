package device

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"testing"
)

// TestMain occupies the fixed iOS control port with a listener that drops
// every connection, then fails the run when anything dialed it. A unit
// test reaching the fixed port would otherwise pass or fail depending on
// whether a real runner happens to be served on the host. When the port is
// already taken, the guard stands down instead of failing the run.
func TestMain(m *testing.M) {
	hostile, stray := occupyControlPort()
	code := m.Run()
	if hostile != nil {
		_ = hostile.Close()
		if n := stray.Load(); n > 0 && code == 0 {
			fmt.Fprintf(os.Stderr, "device tests dialed the fixed control port %d %d time(s), inject a port instead\n", iosControlPort, n)
			code = 1
		}
	}
	os.Exit(code)
}

func occupyControlPort() (net.Listener, *atomic.Int64) {
	var stray atomic.Int64
	l, err := new(net.ListenConfig).Listen(context.Background(), "tcp", fmt.Sprintf("127.0.0.1:%d", iosControlPort))
	if err != nil {
		return nil, &stray
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			stray.Add(1)
			_ = c.Close()
		}
	}()
	return l, &stray
}

// unusedPort returns a loopback port nothing listens on, for a test that
// needs a descriptor port without touching the fixed control port.
func unusedPort(t *testing.T) int {
	t.Helper()
	port, err := operatingSystemFreePort()
	if err != nil {
		t.Fatal(err)
	}
	return port
}
