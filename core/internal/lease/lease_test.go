package lease

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/flock"
)

func testHolder(pid int, command string) Holder {
	return Holder{PID: pid, Command: command, Role: "cli", StartedAt: time.Unix(1700000000, 0).UTC()}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	return newManagerAt(t.TempDir(), clock.NewFakeClock(0))
}

func TestKeyFor_IsStableSixteenHexAndPlatformScoped(t *testing.T) {
	a := KeyFor("android", "R58N90")
	if len(a) != 16 {
		t.Fatalf("key length = %d", len(a))
	}
	if a != KeyFor("android", "R58N90") {
		t.Fatal("key is not stable")
	}
	if a == KeyFor("ios", "R58N90") {
		t.Fatal("key ignores platform")
	}
}

func TestAcquire_WritesHolderAndReleaseFreesIt(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")

	l, err := m.Acquire(context.Background(), key, testHolder(os.Getpid(), "atomyx run"))
	if err != nil {
		t.Fatal(err)
	}
	h, ok := readHolder(m.LockPath(key))
	if !ok || h.PID != os.Getpid() || h.Command != "atomyx run" {
		t.Fatalf("holder on disk = %+v, ok=%v", h, ok)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if _, ok := readHolder(m.LockPath(key)); ok {
		t.Fatal("holder metadata survived Release")
	}
	if _, err := os.Stat(m.LockPath(key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock file must be unlinked by Release: %v", err)
	}
}

func TestAcquire_HeldByOtherReportsHolderAndDeviceHeldCode(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")
	first, err := m.Acquire(context.Background(), key, testHolder(4242, "atomyx run"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Release() }()

	_, err = m.Acquire(context.Background(), key, testHolder(os.Getpid(), "atomyx doctor"))

	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("err = %v, want HeldError", err)
	}
	if !held.Known || held.Holder.PID != 4242 || held.Holder.Command != "atomyx run" {
		t.Fatalf("holder = %+v known=%v", held.Holder, held.Known)
	}
	code, msg, remedy := diag.Describe(err)
	if code != diag.CodeDeviceHeld || code == diag.CodeDeviceBusy {
		t.Fatalf("code = %q", code)
	}
	if msg == "" || remedy == "" {
		t.Fatalf("message %q remedy %q", msg, remedy)
	}
}

func TestAcquire_RetriesAContendedFlockForTheRetryWindowBeforeConflict(t *testing.T) {
	clk := clock.NewFakeClock(0)
	m := newManagerAt(t.TempDir(), clk)
	key := KeyFor("android", "A")
	first, err := m.Acquire(context.Background(), key, testHolder(1, "a"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Release() }()

	if _, err := m.Acquire(context.Background(), key, testHolder(2, "b")); err == nil {
		t.Fatal("expected conflict")
	}
	if got := clk.NowMs(); got < AcquireRetryWindow.Milliseconds() {
		t.Fatalf("clock advanced %dms, want at least the %v retry window", got, AcquireRetryWindow)
	}
}

func TestAcquire_SucceedsWhenTheHolderReleasesInsideTheRetryWindow(t *testing.T) {
	clk := &releasingClock{FakeClock: clock.NewFakeClock(0)}
	m := newManagerAt(t.TempDir(), clk)
	key := KeyFor("android", "A")
	first, err := m.Acquire(context.Background(), key, testHolder(1, "a"))
	if err != nil {
		t.Fatal(err)
	}
	clk.onSleep = func() { _ = first.Release() }

	second, err := m.Acquire(context.Background(), key, testHolder(2, "b"))
	if err != nil {
		t.Fatalf("acquire after release inside window: %v", err)
	}
	_ = second.Release()
}

type releasingClock struct {
	*clock.FakeClock
	onSleep func()
}

func (c *releasingClock) Sleep(ctx context.Context, ms int64) error {
	if c.onSleep != nil {
		c.onSleep()
		c.onSleep = nil
	}
	return c.FakeClock.Sleep(ctx, ms)
}

func TestAcquire_ReacquireAfterRelease(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("ios", "U")
	for i := 0; i < 3; i++ {
		l, err := m.Acquire(context.Background(), key, testHolder(os.Getpid(), "x"))
		if err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		if err := l.Release(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAcquire_StaleLockFileFromDeadProcessIsReplaced(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")
	stale := `{"pid":999999,"command":"atomyx run","role":"cli","startedAt":"2023-11-14T22:13:20Z"}`
	if err := os.WriteFile(m.LockPath(key), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	l, err := m.Acquire(context.Background(), key, testHolder(os.Getpid(), "atomyx doctor"))
	if err != nil {
		t.Fatalf("stale metadata blocked acquire: %v", err)
	}
	defer func() { _ = l.Release() }()
	h, _ := readHolder(m.LockPath(key))
	if h.PID != os.Getpid() {
		t.Fatalf("holder pid = %d, want ours", h.PID)
	}
}

func replaceFile(t *testing.T, path string) {
	t.Helper()
	repl := path + ".new"
	if err := os.WriteFile(repl, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(repl, path); err != nil {
		t.Fatal(err)
	}
}

func TestAcquire_ReplacedBetweenOpenAndFlockRestartsAndSucceeds(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")
	m.afterOpen = func(attempt int) {
		if attempt == 0 {
			replaceFile(t, m.LockPath(key))
		}
	}

	l, err := m.Acquire(context.Background(), key, testHolder(os.Getpid(), "x"))
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer func() { _ = l.Release() }()
	if intact, _ := l.Verify(); !intact {
		t.Fatal("lease descriptor does not match the path after restart")
	}
}

func TestAcquire_PersistentReplacementReturnsTypedError(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")
	m.afterOpen = func(int) { replaceFile(t, m.LockPath(key)) }

	_, err := m.Acquire(context.Background(), key, testHolder(1, "x"))
	if !errors.Is(err, ErrLockFileReplaced) {
		t.Fatalf("err = %v, want ErrLockFileReplaced", err)
	}
}

func TestVerify_DetectsInodeReplacementWhileHeld(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")
	l, err := m.Acquire(context.Background(), key, testHolder(1, "x"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Release() }()
	if intact, _ := l.Verify(); !intact {
		t.Fatal("fresh lease reported replaced")
	}

	replaceFile(t, m.LockPath(key))

	if intact, err := l.Verify(); err != nil || intact {
		t.Fatalf("Verify = %v, %v, want not intact", intact, err)
	}
}

func TestRecheck_ReacquiresTheReplacedFile(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")
	l, _ := m.Acquire(context.Background(), key, testHolder(os.Getpid(), "x"))
	defer func() { _ = l.Release() }()
	replaceFile(t, m.LockPath(key))

	if err := l.Recheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	if intact, _ := l.Verify(); !intact {
		t.Fatal("lease still stale after Recheck")
	}
	h, ok := readHolder(m.LockPath(key))
	if !ok || h.PID != os.Getpid() {
		t.Fatalf("holder not rewritten: %+v", h)
	}
}

func TestRecheck_FailedReacquireReportsLost(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")
	l, _ := m.Acquire(context.Background(), key, testHolder(1, "x"))
	replaceFile(t, m.LockPath(key))
	other, err := os.OpenFile(m.LockPath(key), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()
	if err := flock.TryExclusive(other); err != nil {
		t.Fatal(err)
	}

	err = l.Recheck(context.Background())

	var held *HeldError
	if !errors.Is(err, ErrLost) || !errors.As(err, &held) {
		t.Fatalf("err = %v, want ErrLost wrapping HeldError", err)
	}
}

func TestWatch_ReportsLossAfterOnePeriod(t *testing.T) {
	mc := clock.NewManualClock(0)
	clk := &watchClock{ManualClock: mc}
	m := newManagerAt(t.TempDir(), clk)
	key := KeyFor("android", "A")
	l, err := m.Acquire(context.Background(), key, testHolder(1, "x"))
	if err != nil {
		t.Fatal(err)
	}
	lostCh := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		l.Watch(context.Background(), func(err error) { lostCh <- err })
		close(done)
	}()

	mc.AwaitSleepers(1)
	mc.Advance(InodeRecheckPeriod.Milliseconds())
	mc.AwaitSleepers(1)
	select {
	case err := <-lostCh:
		t.Fatalf("intact lease reported lost: %v", err)
	default:
	}

	replaceFile(t, m.LockPath(key))
	blocker, err := os.OpenFile(m.LockPath(key), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Close() }()
	if err := flock.TryExclusive(blocker); err != nil {
		t.Fatal(err)
	}

	mc.Advance(InodeRecheckPeriod.Milliseconds())
	if err := <-lostCh; !errors.Is(err, ErrLost) {
		t.Fatalf("err = %v, want ErrLost", err)
	}
	<-done
}

func TestWatch_StopsWhenContextEnds(t *testing.T) {
	mc := clock.NewManualClock(0)
	m := newManagerAt(t.TempDir(), mc)
	l, _ := m.Acquire(context.Background(), KeyFor("a", "b"), testHolder(1, "x"))
	defer func() { _ = l.Release() }()
	clk := mc
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		l.Watch(ctx, func(error) { t.Error("unexpected loss") })
		close(done)
	}()
	clk.AwaitSleepers(1)
	cancel()
	<-done
}

func TestUpdateHolder_RewritesIdleSince(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")
	l, _ := m.Acquire(context.Background(), key, testHolder(os.Getpid(), "x"))
	defer func() { _ = l.Release() }()
	now := time.Unix(1700000100, 0).UTC()

	if err := l.UpdateHolder(func(h *Holder) { h.IdleSince = &now }); err != nil {
		t.Fatal(err)
	}

	h, _ := readHolder(m.LockPath(key))
	if h.IdleSince == nil || !h.IdleSince.Equal(now) || h.State() != "idle" {
		t.Fatalf("holder = %+v state %s", h, h.State())
	}
}

func TestHolderState(t *testing.T) {
	if got := (Holder{PID: 1}).State(); got != "running" {
		t.Fatal(got)
	}
	if got := (Holder{PID: 1, RunID: "r"}).State(); got != "running (run open)" {
		t.Fatal(got)
	}
}

func TestUseAfterRelease(t *testing.T) {
	m := newTestManager(t)
	l, _ := m.Acquire(context.Background(), KeyFor("a", "b"), testHolder(1, "x"))
	_ = l.Release()
	if _, err := l.Verify(); !errors.Is(err, ErrReleased) {
		t.Fatalf("Verify err = %v", err)
	}
	if err := l.UpdateHolder(func(*Holder) {}); !errors.Is(err, ErrReleased) {
		t.Fatalf("UpdateHolder err = %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("second Release: %v", err)
	}
}

func TestProbe_NeverCreatesTheLockFile(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")

	st, err := m.Probe(context.Background(), key)
	if err != nil || st.Held {
		t.Fatalf("probe = %+v, %v", st, err)
	}
	if _, err := os.Stat(m.LockPath(key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("probe created the lock file: %v", err)
	}
}

func TestProbe_FreeFileIsNotWritten(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")
	content := []byte(`{"pid":7}`)
	if err := os.WriteFile(m.LockPath(key), content, 0o600); err != nil {
		t.Fatal(err)
	}

	st, err := m.Probe(context.Background(), key)
	if err != nil || st.Held {
		t.Fatalf("probe = %+v, %v", st, err)
	}
	got, _ := os.ReadFile(m.LockPath(key))
	if string(got) != string(content) {
		t.Fatalf("probe modified the file: %q", got)
	}
	l, err := m.Acquire(context.Background(), key, testHolder(1, "x"))
	if err != nil {
		t.Fatalf("probe left the flock taken: %v", err)
	}
	_ = l.Release()
}

func TestProbe_HeldReportsHolder(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")
	l, _ := m.Acquire(context.Background(), key, testHolder(77, "atomyx run"))
	defer func() { _ = l.Release() }()

	st, err := m.Probe(context.Background(), key)
	if err != nil || !st.Held || !st.HolderKnown || st.Holder.PID != 77 {
		t.Fatalf("probe = %+v, %v", st, err)
	}
	h, _ := readHolder(m.LockPath(key))
	if h.PID != 77 {
		t.Fatal("probe overwrote holder metadata")
	}
}

func TestSocketPath_SitsBesideTheLockFile(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("a", "b")
	if filepath.Dir(m.SocketPath(key)) != filepath.Dir(m.LockPath(key)) {
		t.Fatal("socket and lock directories differ")
	}
}

// watchClock lets the Watch period block until a test advances time,
// while the short acquire-retry sleeps advance the clock immediately.
type watchClock struct{ *clock.ManualClock }

func (c *watchClock) Sleep(ctx context.Context, ms int64) error {
	if ms == InodeRecheckPeriod.Milliseconds() {
		return c.ManualClock.Sleep(ctx, ms)
	}
	c.Advance(ms)
	return ctx.Err()
}

func TestWatch_StopsAfterRelease(t *testing.T) {
	mc := clock.NewManualClock(0)
	m := newManagerAt(t.TempDir(), &watchClock{ManualClock: mc})
	l, _ := m.Acquire(context.Background(), KeyFor("a", "b"), testHolder(1, "x"))
	done := make(chan struct{})
	go func() {
		l.Watch(context.Background(), func(error) { t.Error("unexpected loss") })
		close(done)
	}()
	mc.AwaitSleepers(1)
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	mc.Advance(InodeRecheckPeriod.Milliseconds())
	<-done
}

func TestAcquire_LockDescriptorIsCloseOnExec(t *testing.T) {
	m := newTestManager(t)
	l, err := m.Acquire(context.Background(), KeyFor("a", "b"), testHolder(1, "x"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Release() }()
	rc, err := l.file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var flags int
	var ferr error
	if err := rc.Control(func(fd uintptr) {
		r, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFD, 0)
		flags = int(r)
		if errno != 0 {
			ferr = errno
		}
	}); err != nil || ferr != nil {
		t.Fatalf("fcntl: %v %v", err, ferr)
	}
	if flags&syscall.FD_CLOEXEC == 0 {
		t.Fatal("lock descriptor is not close-on-exec")
	}
}

func TestTryAcquire_ReportsAHeldLeaseAtOnceWithoutRetrying(t *testing.T) {
	clk := clock.NewFakeClock(0)
	m := newManagerAt(t.TempDir(), clk)
	key := KeyFor("android", "A")
	first, err := m.Acquire(context.Background(), key, testHolder(1, "a"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Release() }()

	_, err = m.TryAcquire(context.Background(), key, testHolder(2, "b"))

	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("err = %v, want HeldError", err)
	}
	if got := clk.NowMs(); got != 0 {
		t.Fatalf("clock advanced %dms, want the attempt to return without waiting", got)
	}
}

func TestTryAcquire_TakesAFreeLease(t *testing.T) {
	m := newTestManager(t)
	key := KeyFor("android", "A")

	l, err := m.TryAcquire(context.Background(), key, testHolder(2, "b"))
	if err != nil {
		t.Fatalf("TryAcquire() error = %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.TryAcquire(context.Background(), key, testHolder(3, "c")); err != nil {
		t.Fatalf("TryAcquire() after release error = %v", err)
	}
}
