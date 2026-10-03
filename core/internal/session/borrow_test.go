package session_test

import (
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/session"
)

const (
	borrowerPID   = 5001
	thirdPID      = 5002
	borrowCommand = "atomyx run login.yml"

	// borrowFallbackBound is far below the yield budget, so a fallback that
	// waited the whole budget out fails its test.
	borrowFallbackBound = 3 * time.Second
)

type fixedOwners struct{ owner device.RunnerOwner }

func (f fixedOwners) ResolveOwner(int) (device.RunnerOwner, error) { return f.owner, nil }

// borrowFixture is one device held by a source session that can yield,
// and the dependencies of a borrower that stands in for another process.
type borrowFixture struct {
	manager *lease.Manager
	source  *session.Session
	yielder *closingYielder
	factory *recordingFactory
	deps    session.Deps
	owner   device.RunnerOwner
}

func newBorrowFixture(t *testing.T, yielder *closingYielder) *borrowFixture {
	t.Helper()
	m := newSocketManager(t, clock.SystemClock{})
	req := session.Request{Descriptor: androidDesc("A")}
	if yielder != nil {
		req.Yielder = yielder
	}
	source, err := session.Open(context.Background(), socketDeps(t, &recordingFactory{events: &eventLog{}}, m), req)
	if err != nil {
		t.Fatal(err)
	}
	if yielder != nil {
		yielder.closeFn = func() { _ = source.Close() }
	}
	t.Cleanup(func() { _ = source.Close() })

	factory := &recordingFactory{events: &eventLog{}}
	deps := socketDeps(t, factory, m)
	deps.PID = borrowerPID
	deps.Command = "atomyx run"
	owner := device.RunnerOwner{Pid: os.Getpid(), StartTime: time.Unix(1_700_000_000, 0).UTC()}
	deps.Owners = fixedOwners{owner: owner}
	return &borrowFixture{manager: m, source: source, yielder: yielder, factory: factory, deps: deps, owner: owner}
}

func (f *borrowFixture) borrow(t *testing.T) (*session.Session, error) {
	t.Helper()
	started := time.Now()
	s, err := session.Borrow(context.Background(), f.deps, session.Request{Descriptor: androidDesc("A")}, session.Acquisition{}, borrowCommand)
	if err != nil && time.Since(started) > borrowFallbackBound {
		t.Fatalf("Borrow() took %s to give up, want a fallback well inside the %d ms yield budget", time.Since(started), session.YieldBudgetMs)
	}
	if s != nil {
		t.Cleanup(func() { _ = s.Close() })
	}
	return s, err
}

func TestBorrow_AnIdleHolderYieldsAndTheBorrowerHoldsTheLeaseAsItsBorrower(t *testing.T) {
	f := newBorrowFixture(t, &closingYielder{})

	_, err := f.borrow(t)
	if err != nil {
		t.Fatalf("Borrow() error = %v", err)
	}

	if got := f.yielder.borrower.Load(); got != borrowCommand {
		t.Fatalf("the holder was told the borrower is %v, want %q", got, borrowCommand)
	}
	status, err := f.manager.Probe(context.Background(), lease.KeyFor("android", "A"))
	if err != nil || !status.Held || status.Holder.PID != borrowerPID {
		t.Fatalf("probe = %+v, %v, want the lease held by the borrower", status, err)
	}
	f.factory.mu.Lock()
	ctxs := f.factory.ctxs
	f.factory.mu.Unlock()
	if len(ctxs) != 1 || device.LenderFrom(ctxs[0]) != f.owner {
		t.Fatalf("lender in the build context = %+v over %d builds, want %+v on one build", device.LenderFrom(ctxs[len(ctxs)-1]), len(ctxs), f.owner)
	}
}

type gatedAcquirer struct {
	session.LeaseAcquirer
	gate  chan struct{}
	calls atomic.Int32
}

func (g *gatedAcquirer) Acquire(ctx context.Context, key lease.Key, holder lease.Holder) (*lease.Lease, error) {
	if g.calls.Add(1) > 1 {
		select {
		case <-g.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return g.LeaseAcquirer.Acquire(ctx, key, holder)
}

func TestBorrow_AThirdProcessThatWinsTheGapIsNamedInDeviceHeld(t *testing.T) {
	yielder := &closingYielder{}
	f := newBorrowFixture(t, yielder)
	gate := make(chan struct{})
	f.deps.Leases = &gatedAcquirer{LeaseAcquirer: f.manager, gate: gate}
	thirdDeps := socketDeps(t, &recordingFactory{events: &eventLog{}}, f.manager)
	thirdDeps.PID = thirdPID
	thirdDeps.Command = "atomyx inspect"
	yielder.closeFn = func() {
		_ = f.source.Close()
		third, err := session.Open(context.Background(), thirdDeps, session.Request{Descriptor: androidDesc("A")})
		if err == nil {
			t.Cleanup(func() { _ = third.Close() })
		}
		close(gate)
	}

	s, err := f.borrow(t)

	if s != nil {
		t.Fatal("Borrow() returned a session although a third process holds the device")
	}
	if codeOf(err) != diag.CodeDeviceHeld || !strings.Contains(err.Error(), "pid "+strconv.Itoa(thirdPID)) {
		t.Fatalf("err = %v, want %s naming pid %d", err, diag.CodeDeviceHeld, thirdPID)
	}
}

func TestBorrow_AConnectionThatEndsAfterTheRequestStillAcquiresWhenTheHolderReleased(t *testing.T) {
	f := newBorrowFixture(t, &closingYielder{})
	f.deps.RequestYield = func(_ context.Context, _ attach.ClientConfig, _ string) error {
		_ = f.source.Close() //nolint:contextcheck // the close runs after the reply, on its own goroutine
		return &attach.ConnectionError{Cause: io.ErrUnexpectedEOF}
	}

	s, err := f.borrow(t)

	if err != nil || s == nil {
		t.Fatalf("Borrow() = %v, %v, want a session after a yield that may have happened", s, err)
	}
}
