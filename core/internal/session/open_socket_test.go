package session_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/session"
)

const maxSocketPathBytes = 104

// newSocketManager returns a lease manager whose runtime directory sits
// under a short path, so the socket path stays within the unix limit.
func newSocketManager(t *testing.T, clk clock.Clock) *lease.Manager {
	t.Helper()
	root, err := os.MkdirTemp("", "a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	m, err := lease.NewManager(lease.Environment{
		GOOS:          "linux",
		Getenv:        func(k string) string { return map[string]string{"XDG_RUNTIME_DIR": root}[k] },
		UserCacheDir:  func() (string, error) { return root, nil },
		Getuid:        os.Getuid,
		IsLocalFS:     func(string) (bool, error) { return true, nil },
		FallbackRoot:  root,
		MaxSocketPath: 4096,
	}, clk)
	if err != nil {
		t.Fatal(err)
	}
	path := m.SocketPath(lease.KeyFor("android", "A"))
	if len(path) >= maxSocketPathBytes {
		t.Fatalf("socket path %q is %d bytes, want under %d", path, len(path), maxSocketPathBytes)
	}
	return m
}

func socketPathFor(m *lease.Manager) string { return m.SocketPath(lease.KeyFor("android", "A")) }

func dialHolder(t *testing.T, path string) *attach.Client {
	t.Helper()
	c, err := attach.Dial(context.Background(), attach.ClientConfig{Path: path, Client: "test"})
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func requireNoFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Lstat(%s) error = %v, want the file gone", path, err)
	}
}

func TestOpen_BindsTheHolderSocketAndRecordsItInTheLease(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	s, err := session.Open(context.Background(), socketDeps(t, &recordingFactory{events: &eventLog{}}, m), session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	want := socketPathFor(m)
	if got := s.Info().Holder.Socket; got != want {
		t.Fatalf("Info().Holder.Socket = %q, want %q", got, want)
	}
	status, err := m.Probe(context.Background(), lease.KeyFor("android", "A"))
	if err != nil || status.Holder.Socket != want {
		t.Fatalf("Probe() holder socket = %q, %v, want %q", status.Holder.Socket, err, want)
	}
	info, err := os.Stat(want)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket stat = %v, %v, want mode 0600", info, err)
	}
	if c := dialHolder(t, want); c.Protocol() != attach.ProtocolMajor {
		t.Fatalf("protocol = %d, want %d", c.Protocol(), attach.ProtocolMajor)
	}
}

func TestOpen_AFailedBuildLeavesNoListenerAndNoSocketFile(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	factory := &recordingFactory{events: &eventLog{}, err: errors.New("build failed")}
	if _, err := session.Open(context.Background(), socketDeps(t, factory, m), session.Request{Descriptor: androidDesc("A")}); err == nil {
		t.Fatal("Open() succeeded, want the build error")
	}

	path := socketPathFor(m)
	requireNoFile(t, path)
	if _, err := rawDial(path); err == nil {
		t.Fatal("a listener answers after a failed build")
	}
}

func TestOpen_ReplacesAStaleSocketFileLeftByAnEarlierHolder(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	path := socketPathFor(m)
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	_ = stale.Close()

	s, err := session.Open(context.Background(), socketDeps(t, &recordingFactory{events: &eventLog{}}, m), session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	dialHolder(t, path)
	if got := s.Info().Holder.Socket; got != path {
		t.Fatalf("Info().Holder.Socket = %q, want %q", got, path)
	}
}

func TestClose_RemovesTheSocketFileAndEndsEveryViewerConnection(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	s, err := session.Open(context.Background(), socketDeps(t, &recordingFactory{events: &eventLog{}}, m), session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	path := socketPathFor(m)
	viewer := dialHolder(t, path)

	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	<-viewer.Done()
	requireNoFile(t, path)
	if _, err := rawDial(path); err == nil {
		t.Fatal("a listener answers after Close")
	}
}

func TestClose_LeavesASocketAnotherProcessBoundAtTheSamePath(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	s, err := session.Open(context.Background(), socketDeps(t, &recordingFactory{events: &eventLog{}}, m), session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	path := socketPathFor(m)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	other, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()

	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("Lstat() error = %v, want the other holder's socket kept", err)
	}
	conn, err := rawDial(path)
	if err != nil {
		t.Fatalf("Dial() error = %v, want the other holder's listener to answer", err)
	}
	_ = conn.Close()
}

func TestSession_ALostSessionStopsItsServerAndLeavesTheSocketPathToTheNewHolder(t *testing.T) {
	manual := clock.NewManualClock(0)
	m := newSocketManager(t, manual)
	s, err := session.Open(context.Background(), socketDeps(t, &recordingFactory{events: &eventLog{}}, m), session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	viewer := dialHolder(t, socketPathFor(m))

	lockPath := m.LockPath(lease.KeyFor("android", "A"))
	replacement := filepath.Join(filepath.Dir(lockPath), "replacement")
	if err := os.WriteFile(replacement, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, lockPath); err != nil {
		t.Fatal(err)
	}
	intruder, err := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = intruder.Close() }()
	if err := lockExclusive(intruder); err != nil {
		t.Fatal(err)
	}
	manual.AwaitSleepers(1)
	manual.Advance(lease.InodeRecheckPeriod.Milliseconds())
	for range int(lease.AcquireRetryWindow / lease.AcquirePollInterval) {
		manual.AwaitSleepers(1)
		manual.Advance(lease.AcquirePollInterval.Milliseconds())
	}
	<-s.Lost()

	<-viewer.Done()
	if _, err := rawDial(socketPathFor(m)); err == nil {
		t.Fatal("a lost session still answers on its socket")
	}
}

func rawDial(path string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(context.Background(), "unix", path)
}

func TestOpen_ABindFailureIsRecordedInTheLeaseWithoutAPath(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	path := socketPathFor(m)
	if err := os.Chmod(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	s, err := session.Open(context.Background(), socketDeps(t, &recordingFactory{events: &eventLog{}}, m), session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	status, err := m.Probe(context.Background(), lease.KeyFor("android", "A"))
	if err != nil {
		t.Fatal(err)
	}
	const want = "runtime directory is not private"
	if status.Holder.Socket != "" || status.Holder.SocketError != want {
		t.Fatalf("holder socket = %q, socketError = %q, want no socket and %q", status.Holder.Socket, status.Holder.SocketError, want)
	}
	if strings.Contains(status.Holder.SocketError, filepath.Dir(path)) {
		t.Fatalf("socketError %q names a path", status.Holder.SocketError)
	}
}

func TestOpen_ABoundSocketLeavesTheLeaseSocketErrorEmpty(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	s, err := session.Open(context.Background(), socketDeps(t, &recordingFactory{events: &eventLog{}}, m), session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	status, err := m.Probe(context.Background(), lease.KeyFor("android", "A"))
	if err != nil || status.Holder.Socket == "" || status.Holder.SocketError != "" {
		t.Fatalf("holder = %+v, %v, want a socket and no socketError", status.Holder, err)
	}
}

// socketDeps is baseDeps with the lease manager also naming the socket,
// as every production composition root does.
func socketDeps(t *testing.T, f device.WrappingFactory, m *lease.Manager) session.Deps {
	t.Helper()
	deps := baseDeps(t, f, m)
	deps.Sockets = m
	return deps
}

// wrappingAcquirer hides the manager's other methods behind the plain
// LeaseAcquirer contract, like a decorator that adds logging would.
type wrappingAcquirer struct{ inner session.LeaseAcquirer }

func (w wrappingAcquirer) Acquire(ctx context.Context, key lease.Key, holder lease.Holder) (*lease.Lease, error) {
	return w.inner.Acquire(ctx, key, holder)
}

func (w wrappingAcquirer) Dir() string { return w.inner.Dir() }

func TestOpen_AWrappedAcquirerStillServesTheSocketTheDepsName(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	deps := socketDeps(t, &recordingFactory{events: &eventLog{}}, m)
	deps.Leases = wrappingAcquirer{inner: m}
	s, err := session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	if got, want := s.Info().Holder.Socket, socketPathFor(m); got != want {
		t.Fatalf("Info().Holder.Socket = %q, want %q", got, want)
	}
	dialHolder(t, socketPathFor(m))
}

func TestOpen_WithoutSocketsTheHolderServesNoSocket(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	s, err := session.Open(context.Background(), baseDeps(t, &recordingFactory{events: &eventLog{}}, m), session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	if got := s.Info().Holder.Socket; got != "" {
		t.Fatalf("Info().Holder.Socket = %q, want none without Deps.Sockets", got)
	}
	requireNoFile(t, socketPathFor(m))
}
