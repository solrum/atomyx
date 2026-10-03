package attach

import (
	"os"
	"runtime"
	"testing"
)

func openFDs(t *testing.T) int {
	t.Helper()
	dir := "/proc/self/fd"
	if runtime.GOOS == "darwin" {
		dir = "/dev/fd"
	}
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	names, err := f.Readdirnames(-1)
	if err != nil {
		t.Fatal(err)
	}
	return len(names)
}

// assertNoLeakedFDs compares the open descriptor count with a baseline
// taken before the server and its clients existed. Call it only after the
// server's Close has returned and every client is closed, because Close
// returns once all server goroutines have ended and released their
// sockets, so no waiting is needed.
func assertNoLeakedFDs(t *testing.T, baseFDs int) {
	t.Helper()
	if got := openFDs(t); got > baseFDs {
		t.Fatalf("leak: %d open descriptors, baseline %d", got, baseFDs)
	}
}

// assertClientStopped checks that both of a closed client's goroutines
// have ended, using the channels they close on exit.
func assertClientStopped(t *testing.T, c *Client) {
	t.Helper()
	for name, ch := range map[string]chan struct{}{"read loop": c.done, "delivery": c.delivered} {
		select {
		case <-ch:
		default:
			t.Fatalf("the client %s goroutine was still running after Close", name)
		}
	}
}
