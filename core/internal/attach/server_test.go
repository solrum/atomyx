package attach

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

func TestListenUnixRestrictsTheSocketToItsOwnerBeforeAcceptingAnyone(t *testing.T) {
	path := shortSocketPath(t)
	modes := make(chan os.FileMode, 1)
	cfg := ServerConfig{Path: path}
	var first sync.Once
	cfg.hooks.accept = func(ln *net.UnixListener) (*net.UnixConn, error) {
		first.Do(func() {
			info, err := os.Stat(path)
			if err != nil {
				modes <- 0
				return
			}
			modes <- info.Mode().Perm()
		})
		return ln.AcceptUnix()
	}
	startServer(t, cfg, nil)

	select {
	case got := <-modes:
		if got != 0o600 {
			t.Fatalf("socket mode when accepting began = %o, want 600", got)
		}
	case <-time.After(testGuard):
		t.Fatal("the server never started accepting")
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

func TestCloseLeavesNoGoroutineOrDescriptor(t *testing.T) {
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

	assertNoLeakedFDs(t, baseFDs)
}

type viewerEvent struct {
	attached bool
	info     ViewerInfo
}

type viewerRecorder struct{ events chan viewerEvent }

func newViewerRecorder() *viewerRecorder {
	return &viewerRecorder{events: make(chan viewerEvent, 16)}
}

func (r *viewerRecorder) ViewerAttached(info ViewerInfo) {
	r.events <- viewerEvent{attached: true, info: info}
}

func (r *viewerRecorder) ViewerDetached(info ViewerInfo) {
	r.events <- viewerEvent{info: info}
}

func (r *viewerRecorder) next(t *testing.T) viewerEvent {
	t.Helper()
	select {
	case e := <-r.events:
		return e
	case <-time.After(testGuard):
		t.Fatal("no viewer event arrived")
		return viewerEvent{}
	}
}

func (r *viewerRecorder) expectNone(t *testing.T) {
	t.Helper()
	select {
	case e := <-r.events:
		t.Fatalf("unexpected viewer event %+v", e)
	default:
	}
}

func helloAs(c *rawClient, name string) Frame {
	return c.call(TypeHello, Hello{Protocol: SupportedVersions, Client: name, Features: []string{}})
}

func TestViewerHandlerSeesEachObservingConnectionAttachAndDetachOnce(t *testing.T) {
	rec := newViewerRecorder()
	srv := startServer(t, ServerConfig{Handlers: Handlers{Viewers: rec, Observe: newFakeObserve()}}, nil)
	first := dialRaw(t, srv.Path())
	helloAs(first, "viewer one")
	first.call(TypeTreeLatest, nil)
	second := dialRaw(t, srv.Path())
	helloAs(second, "viewer two")
	second.call(TypeTreeLatest, nil)
	first.call(TypeScreenshotLatest, nil)

	a, b := rec.next(t), rec.next(t)
	if !a.attached || !b.attached || a.info.Client != "viewer one" || b.info.Client != "viewer two" {
		t.Fatalf("attach events = %+v then %+v", a, b)
	}
	if a.info.ID == b.info.ID {
		t.Fatalf("two connections share id %d", a.info.ID)
	}

	if ok := first.call(TypeBye, nil).Ok; ok == nil || !*ok {
		t.Fatal("bye was not acknowledged")
	}
	if gone := rec.next(t); gone.attached || gone.info != a.info {
		t.Fatalf("detach event = %+v, want viewer one", gone)
	}

	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	if gone := rec.next(t); gone.attached || gone.info != b.info {
		t.Fatalf("detach event on Close = %+v, want viewer two", gone)
	}
	rec.expectNone(t)
}

func TestConnectionThatOnlyYieldsOrSaysHelloIsNotAnnounced(t *testing.T) {
	rec := newViewerRecorder()
	srv := startServer(t, ServerConfig{Handlers: Handlers{Viewers: rec, Observe: newFakeObserve(), Control: &fakeControl{}}}, nil)
	client := dialRaw(t, srv.Path())
	helloAs(client, "atomyx run")
	if ok := client.call(TypeControlYield, ControlYield{Command: "atomyx run"}).Ok; ok == nil || !*ok {
		t.Fatal("yield was refused")
	}
	if ok := client.call(TypeBye, nil).Ok; ok == nil || !*ok {
		t.Fatal("bye was not acknowledged")
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	rec.expectNone(t)
}

func TestConnectionThatNeverSaidHelloIsNotAnnounced(t *testing.T) {
	rec := newViewerRecorder()
	srv := startServer(t, ServerConfig{Handlers: Handlers{Viewers: rec}}, func() time.Time { return time.Unix(0, 0) })
	dialRaw(t, srv.Path()).expectClosed()
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	rec.expectNone(t)
}

func TestClientNameOverTheBoundIsRefusedAndNotAnnounced(t *testing.T) {
	rec := newViewerRecorder()
	srv := startServer(t, ServerConfig{Handlers: Handlers{Viewers: rec, Observe: newFakeObserve()}}, nil)
	client := dialRaw(t, srv.Path())

	tooLong := strings.Repeat("n", MaxClientNameBytes+1)
	if got := failureCode(t, helloAs(client, tooLong)); got != diag.CodeInvalidArgument {
		t.Fatalf("code = %q, want %s", got, diag.CodeInvalidArgument)
	}
	rec.expectNone(t)

	atBound := strings.Repeat("n", MaxClientNameBytes)
	if ok := helloAs(client, atBound).Ok; ok == nil || !*ok {
		t.Fatal("a name of exactly the bound was refused")
	}
	client.call(TypeTreeLatest, nil)
	if e := rec.next(t); !e.attached || e.info.Client != atBound {
		t.Fatalf("attach event = %+v", e)
	}
}

func TestAcceptLoopSurvivesAcceptErrorsWithGrowingBoundedPauses(t *testing.T) {
	const firstRun, secondRun = 12, 2
	var attempts atomic.Int32
	var pauses []time.Duration
	cfg := ServerConfig{}
	cfg.hooks.accept = func(ln *net.UnixListener) (*net.UnixConn, error) {
		n := attempts.Add(1)
		if n <= firstRun || (n > firstRun+1 && n <= firstRun+1+secondRun) {
			return nil, syscall.EMFILE
		}
		return ln.AcceptUnix()
	}
	cfg.hooks.wait = func(_ context.Context, d time.Duration) bool {
		pauses = append(pauses, d)
		return true
	}
	srv := startServer(t, cfg, nil)

	for range 2 {
		client := dialRaw(t, srv.Path())
		if ok := client.hello().Ok; ok == nil || !*ok {
			t.Fatal("a viewer was not served after the accept errors")
		}
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}

	var want []time.Duration
	for _, run := range []int{firstRun, secondRun} {
		delay := acceptBackoffMin
		for range run {
			want = append(want, delay)
			delay = min(delay*2, acceptBackoffMax)
		}
	}
	if len(pauses) != len(want) {
		t.Fatalf("pauses = %v, want %v", pauses, want)
	}
	for i := range want {
		if pauses[i] != want[i] {
			t.Fatalf("pause %d = %v, want %v (all: %v)", i, pauses[i], want[i], pauses)
		}
	}
	if want[firstRun-1] != acceptBackoffMax {
		t.Fatalf("the first run never reached the bound %v", acceptBackoffMax)
	}
}

func TestListenUnixRefusesADirectoryOthersCanReach(t *testing.T) {
	path := shortSocketPath(t)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := listen(ServerConfig{Path: path}, time.Now); err == nil {
		t.Fatal("ListenUnix accepted a directory open to group and others")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a socket was created in the refused directory: %v", err)
	}
}

func TestCheckSocketDirRequiresTheOwningUser(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := checkSocketDir(dir, uint32(os.Getuid())); err != nil {
		t.Fatalf("an owned 0700 directory was refused: %v", err)
	}
	if err := checkSocketDir(dir, uint32(os.Getuid())+1); err == nil {
		t.Fatal("a directory owned by another user was accepted")
	}
}
