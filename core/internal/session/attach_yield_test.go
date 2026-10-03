package session_test

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
)

type closingYielder struct {
	refuse   error
	borrower atomic.Value
	closeFn  func()
}

func (y *closingYielder) Yield(_ context.Context, borrower string) (func(), error) {
	y.borrower.Store(borrower)
	if y.refuse != nil {
		return nil, y.refuse
	}
	return y.closeFn, nil
}

func TestAttachHelloAdvertisesControlOnlyWhenTheHostInjectedAYielder(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	without, err := session.Open(context.Background(), socketDeps(t, &recordingFactory{events: &eventLog{}}, m),
		session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(dialHolder(t, socketPathFor(m)).Features(), attach.FeatureControl) {
		t.Fatal("a session without a yielder advertises control")
	}
	_ = without.Close()

	with, err := session.Open(context.Background(), socketDeps(t, &recordingFactory{events: &eventLog{}}, m),
		session.Request{Descriptor: androidDesc("A"), Yielder: &closingYielder{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = with.Close() })
	if !slices.Contains(dialHolder(t, socketPathFor(m)).Features(), attach.FeatureControl) {
		t.Fatal("a session with a yielder lacks the control feature")
	}
}

func TestAttachYieldRefusalReachesTheBorrower(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	yielder := &closingYielder{refuse: diag.NewHolderBusy("a run is open")}
	s, err := session.Open(context.Background(), socketDeps(t, &recordingFactory{events: &eventLog{}}, m),
		session.Request{Descriptor: androidDesc("A"), Yielder: yielder})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	err = attach.RequestYield(context.Background(), attach.ClientConfig{Path: socketPathFor(m), Client: "test"}, "atomyx run login.yml")
	if code, _, _ := diag.Describe(err); code != diag.CodeHolderBusy {
		t.Fatalf("err = %v, want %s", err, diag.CodeHolderBusy)
	}
	if got := yielder.borrower.Load(); got != "atomyx run login.yml" {
		t.Fatalf("borrower = %v", got)
	}
}

func TestAttachYieldLetsTheHostCloseItsSessionAfterTheReply(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	yielder := &closingYielder{}
	s, err := session.Open(context.Background(), socketDeps(t, &recordingFactory{events: &eventLog{}}, m),
		session.Request{Descriptor: androidDesc("A"), Yielder: yielder})
	if err != nil {
		t.Fatal(err)
	}
	yielder.closeFn = func() { _ = s.Close() }
	t.Cleanup(func() { _ = s.Close() })

	if err := attach.RequestYield(context.Background(), attach.ClientConfig{Path: socketPathFor(m), Client: "test"}, "atomyx run login.yml"); err != nil {
		t.Fatalf("RequestYield() error = %v, want ok before the connection ends", err)
	}
}
