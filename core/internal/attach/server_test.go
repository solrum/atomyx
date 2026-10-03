package attach

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
)

// testGuard bounds every blocking read in a test, so a regression shows
// as a failure and never as a hang.
const testGuard = 10 * time.Second

func startServer(t *testing.T, cfg ServerConfig, now func() time.Time) *Server {
	t.Helper()
	if cfg.Path == "" {
		cfg.Path = shortSocketPath(t)
	}
	if now == nil {
		now = time.Now
	}
	srv, err := listen(cfg, now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

type rawClient struct {
	t      *testing.T
	nc     net.Conn
	reader *FrameReader
	nextID int64
}

func dialRaw(t *testing.T, path string) *rawClient {
	t.Helper()
	nc, err := (&net.Dialer{}).DialContext(context.Background(), "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nc.Close() })
	if err := nc.SetDeadline(time.Now().Add(testGuard)); err != nil {
		t.Fatal(err)
	}
	return &rawClient{t: t, nc: nc, reader: NewFrameReader(nc)}
}

func (c *rawClient) send(messageType string, data any) int64 {
	c.t.Helper()
	c.nextID++
	f, err := NewRequest(c.nextID, messageType, data)
	if err != nil {
		c.t.Fatal(err)
	}
	_ = WriteFrame(c.nc, f)
	return c.nextID
}

func (c *rawClient) read() Frame {
	c.t.Helper()
	f, err := c.reader.ReadFrame()
	if err != nil {
		c.t.Fatalf("reading a frame: %v", err)
	}
	return f
}

func (c *rawClient) call(messageType string, data any) Frame {
	c.t.Helper()
	id := c.send(messageType, data)
	f := c.read()
	if f.ID != id {
		c.t.Fatalf("response id = %d, want %d", f.ID, id)
	}
	return f
}

func (c *rawClient) hello() Frame {
	c.t.Helper()
	return c.call(TypeHello, Hello{Protocol: SupportedVersions, Client: "test", Features: []string{}})
}

func (c *rawClient) expectClosed() {
	c.t.Helper()
	f, err := c.reader.ReadFrame()
	if err == nil {
		c.t.Fatalf("connection stayed open and delivered %+v", f)
	}
	if !errors.Is(err, io.EOF) && !errors.Is(err, syscall.ECONNRESET) {
		c.t.Fatalf("connection end = %v, want EOF or reset", err)
	}
}

func failureCode(t *testing.T, f Frame) string {
	t.Helper()
	if f.Ok == nil || *f.Ok {
		t.Fatalf("frame %+v is not a failure response", f)
	}
	return f.Code
}

func TestListenRestrictsTheSocketToItsOwner(t *testing.T) {
	srv := startServer(t, ServerConfig{}, nil)
	info, err := os.Stat(srv.Path())
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("socket mode = %o, want 600", got)
	}
}

func TestServerRefusesAPeerWithAnotherUID(t *testing.T) {
	srv := startServer(t, ServerConfig{
		PeerUID: func(*net.UnixConn) (uint32, error) { return uint32(os.Getuid()) + 1, nil },
	}, nil)
	client := dialRaw(t, srv.Path())
	client.send(TypeHello, Hello{Protocol: SupportedVersions})
	client.expectClosed()
}

func TestServerRefusesAPeerWhoseCredentialsCannotBeRead(t *testing.T) {
	srv := startServer(t, ServerConfig{
		PeerUID: func(*net.UnixConn) (uint32, error) { return 0, errors.New("no credentials") },
	}, nil)
	client := dialRaw(t, srv.Path())
	client.send(TypeHello, Hello{Protocol: SupportedVersions})
	client.expectClosed()
}

func TestServerClosesAConnectionThatSendsNoHelloByTheDeadline(t *testing.T) {
	past := func() time.Time { return time.Unix(0, 0) }
	srv := startServer(t, ServerConfig{}, past)
	client := dialRaw(t, srv.Path())
	client.expectClosed()
}

func TestServerCapsConcurrentConnectionsWithATypedRefusal(t *testing.T) {
	srv := startServer(t, ServerConfig{MaxConnections: 2}, nil)
	for range 2 {
		client := dialRaw(t, srv.Path())
		if ok := client.hello().Ok; ok == nil || !*ok {
			t.Fatal("hello within the cap was refused")
		}
	}
	over := dialRaw(t, srv.Path())
	over.send(TypeHello, Hello{Protocol: SupportedVersions})
	refusal := over.read()
	if got := failureCode(t, refusal); got != diag.CodeHolderBusy {
		t.Fatalf("code = %q, want %s", got, diag.CodeHolderBusy)
	}
	if refusal.ID != 0 {
		t.Fatalf("a refusal belongs to the connection, got id %d", refusal.ID)
	}
	over.expectClosed()
}

func TestUnknownTypeIsUnsupportedAndTheConnectionStaysUsable(t *testing.T) {
	srv := startServer(t, ServerConfig{}, nil)
	client := dialRaw(t, srv.Path())
	client.hello()

	for range 2 {
		if got := failureCode(t, client.call("control.borrow", nil)); got != diag.CodeUnsupported {
			t.Fatalf("code = %q, want %s", got, diag.CodeUnsupported)
		}
	}
	if ok := client.call(TypeBye, nil).Ok; ok == nil || !*ok {
		t.Fatal("bye after UNSUPPORTED failed")
	}
}

func TestNonOverlappingVersionRangeIsUnsupportedVersion(t *testing.T) {
	srv := startServer(t, ServerConfig{}, nil)
	client := dialRaw(t, srv.Path())

	future := client.call(TypeHello, Hello{Protocol: VersionRange{Min: 2, Max: 3}, Client: "future"})
	if got := failureCode(t, future); got != diag.CodeUnsupportedVersion {
		t.Fatalf("code = %q, want %s", got, diag.CodeUnsupportedVersion)
	}
	var result HelloResult
	if err := client.hello().Decode(&result); err != nil || result.Protocol != 1 {
		t.Fatalf("hello after the refusal = %+v, %v", result, err)
	}
}

func TestRequestsBeforeHelloAreRefusedUntilHelloArrives(t *testing.T) {
	srv := startServer(t, ServerConfig{}, nil)
	client := dialRaw(t, srv.Path())
	if got := failureCode(t, client.call(TypeStatus, nil)); got != diag.CodeInvalidArgument {
		t.Fatalf("code = %q, want %s", got, diag.CodeInvalidArgument)
	}
	if ok := client.hello().Ok; ok == nil || !*ok {
		t.Fatal("hello after the refusal failed")
	}
}

func TestSecondHelloIsRefused(t *testing.T) {
	srv := startServer(t, ServerConfig{}, nil)
	client := dialRaw(t, srv.Path())
	client.hello()
	if got := failureCode(t, client.hello()); got != diag.CodeInvalidArgument {
		t.Fatalf("code = %q, want %s", got, diag.CodeInvalidArgument)
	}
}

func TestByeAnswersThenClosesTheConnection(t *testing.T) {
	srv := startServer(t, ServerConfig{}, nil)
	client := dialRaw(t, srv.Path())
	client.hello()
	if ok := client.call(TypeBye, nil).Ok; ok == nil || !*ok {
		t.Fatal("bye was not acknowledged")
	}
	client.expectClosed()
}

func TestMalformedFrameEndsTheConnectionWithAnError(t *testing.T) {
	srv := startServer(t, ServerConfig{}, nil)
	client := dialRaw(t, srv.Path())
	if _, err := client.nc.Write([]byte("not json\n")); err != nil {
		t.Fatal(err)
	}
	if got := failureCode(t, client.read()); got != diag.CodeInvalidArgument {
		t.Fatalf("code = %q, want %s", got, diag.CodeInvalidArgument)
	}
	client.expectClosed()
}

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

func TestCloseLeavesNoGoroutineOrDescriptor(t *testing.T) {
	baseGoroutines := runtime.NumGoroutine()
	baseFDs := openFDs(t)

	srv := startServer(t, ServerConfig{}, nil)
	clients := make([]*rawClient, 3)
	for i := range clients {
		clients[i] = dialRaw(t, srv.Path())
		clients[i].hello()
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	for _, c := range clients {
		c.expectClosed()
		_ = c.nc.Close()
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}

	const attempts = 500
	for range attempts {
		if runtime.NumGoroutine() <= baseGoroutines && openFDs(t) <= baseFDs {
			return
		}
		<-time.After(10 * time.Millisecond)
	}
	t.Fatalf("after Close: %d goroutines (base %d), %d descriptors (base %d)",
		runtime.NumGoroutine(), baseGoroutines, openFDs(t), baseFDs)
}
