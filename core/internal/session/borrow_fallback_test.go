package session_test

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
)

func TestBorrow_ABusyHolderFallsBackToDeviceHeldNamingIt(t *testing.T) {
	f := newBorrowFixture(t, &closingYielder{refuse: diag.NewHolderBusy("a run is open")})

	s, err := f.borrow(t)

	if s != nil {
		t.Fatal("Borrow() returned a session from a busy holder")
	}
	f.requireHeldBySource(t, err)
	if f.factory.buildCount() != 0 {
		t.Fatal("the borrower built a driver without the lease")
	}
}

func TestBorrow_AHolderWithoutTheControlFeatureFallsBackToDeviceHeld(t *testing.T) {
	f := newBorrowFixture(t, nil)

	s, err := f.borrow(t)

	if s != nil {
		t.Fatal("Borrow() returned a session from a holder that cannot yield")
	}
	f.requireHeldBySource(t, err)
}

func TestBorrow_AHolderThatCannotBeReachedFallsBackToDeviceHeld(t *testing.T) {
	f := newBorrowFixture(t, &closingYielder{})
	f.deps.RequestYield = func(_ context.Context, _ attach.ClientConfig, _ string) (attach.YieldResult, error) {
		return attach.YieldResult{}, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	}

	s, err := f.borrow(t)

	if s != nil {
		t.Fatal("Borrow() returned a session from a holder that was never asked")
	}
	f.requireHeldBySource(t, err)
}

func TestBorrow_AReacquireNeverBorrows(t *testing.T) {
	f := newBorrowFixture(t, &closingYielder{})
	f.deps.RequestYield = func(context.Context, attach.ClientConfig, string) (attach.YieldResult, error) {
		t.Error("a re-acquire asked the holder to yield")
		return attach.YieldResult{}, nil
	}

	_, err := session.Borrow(context.Background(), f.deps, session.Request{Descriptor: androidDesc("A"), Reacquire: true}, session.Acquisition{}, borrowCommand)

	if codeOf(err) != diag.CodeDeviceReleasedHeld {
		t.Fatalf("err = %v, want %s", err, diag.CodeDeviceReleasedHeld)
	}
}

type orderPreparer struct{ prepared atomic.Bool }

func (p *orderPreparer) Prepare(context.Context, device.Descriptor) error {
	p.prepared.Store(true)
	return nil
}

func TestBorrow_PreparesTheDeviceBeforeAskingTheHolderToYield(t *testing.T) {
	f := newBorrowFixture(t, &closingYielder{})
	preparer := &orderPreparer{}
	f.deps.Preparer = preparer
	f.deps.RequestYield = func(ctx context.Context, cfg attach.ClientConfig, command string) (attach.YieldResult, error) {
		if !preparer.prepared.Load() {
			t.Error("the holder was asked to yield before the device was prepared")
		}
		return attach.RequestYield(ctx, cfg, command)
	}

	if _, err := f.borrow(t); err != nil {
		t.Fatalf("Borrow() error = %v", err)
	}
}

func (f *borrowFixture) requireHeldBySource(t *testing.T, err error) {
	t.Helper()
	if codeOf(err) != diag.CodeDeviceHeld {
		t.Fatalf("err = %v, want %s", err, diag.CodeDeviceHeld)
	}
	if !strings.Contains(err.Error(), "pid "+strconv.Itoa(os.Getpid())) {
		t.Fatalf("err = %q, want it to name the source", err.Error())
	}
}

func TestBorrow_AHelloThatFailsBeforeTheRequestWasSentDoesNotWait(t *testing.T) {
	f := newBorrowFixture(t, &closingYielder{})
	f.deps.RequestYield = func(context.Context, attach.ClientConfig, string) (attach.YieldResult, error) {
		return attach.YieldResult{}, &attach.ConnectionError{Cause: io.ErrUnexpectedEOF}
	}

	s, err := f.borrow(t)

	if s != nil {
		t.Fatal("Borrow() returned a session from a holder that was never asked")
	}
	f.requireHeldBySource(t, err)
}

func TestBorrow_AnAcknowledgedRequestWhoseWaitIsCutShortStillAcquires(t *testing.T) {
	f := newBorrowFixture(t, &closingYielder{})
	f.deps.RequestYield = func(context.Context, attach.ClientConfig, string) (attach.YieldResult, error) {
		_ = f.source.Close() //nolint:contextcheck // the close runs after the reply, on its own goroutine
		return attach.YieldResult{Sent: true, Acknowledged: true}, context.DeadlineExceeded
	}

	s, err := f.borrow(t)

	if err != nil || s == nil {
		t.Fatalf("Borrow() = %v, %v, want a session after the holder acknowledged the request", s, err)
	}
}
