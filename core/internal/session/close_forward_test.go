package session_test

import (
	"context"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/session/sessiontest"
)

const closeBoundGuard = 5 * time.Second

// slowTeardownDriver blocks its context-bound disconnect until the
// context ends, like a command that runs on after the close gave up on it.
type slowTeardownDriver struct {
	*mockdriver.Driver
	started chan struct{}
	ended   chan struct{}
}

func (d *slowTeardownDriver) DisconnectContext(ctx context.Context) error {
	close(d.started)
	<-ctx.Done()
	close(d.ended)
	return nil
}

type slowTeardownFactory struct{ drv *slowTeardownDriver }

func (f *slowTeardownFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f *slowTeardownFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	if err := f.drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	return device.Built{
		Driver:    f.drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: f.drv, Clock: orchestra.SystemClock{}}),
	}, nil
}

func TestClose_ADisconnectThatOutlivesTheGraceIsCanceledBeforeTheLeaseIsReleased(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	drv := &slowTeardownDriver{Driver: mockdriver.New(), started: make(chan struct{}), ended: make(chan struct{})}
	deps := socketDeps(t, &slowTeardownFactory{drv: drv}, m)
	deps.Clock = &recordingClock{}
	s, err := session.Open(context.Background(), deps, session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()

	select {
	case <-drv.ended:
	case <-time.After(closeBoundGuard):
		t.Fatal("the disconnect was never canceled after the grace period")
	}
	<-closed
}

type closeValueKey struct{}

// valueRecordingDriver notes the value its disconnect context carries.
type valueRecordingDriver struct {
	*mockdriver.Driver
	seen chan any
}

func (d *valueRecordingDriver) DisconnectContext(ctx context.Context) error {
	d.seen <- ctx.Value(closeValueKey{})
	return nil
}

type valueRecordingFactory struct{ drv *valueRecordingDriver }

func (f *valueRecordingFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f *valueRecordingFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	if err := f.drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	return device.Built{
		Driver:    f.drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: f.drv, Clock: orchestra.SystemClock{}}),
	}, nil
}

func TestCloseContext_TheDriverDisconnectsUnderTheCallersValues(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	drv := &valueRecordingDriver{Driver: mockdriver.New(), seen: make(chan any, 1)}
	s, err := session.Open(context.Background(), socketDeps(t, &valueRecordingFactory{drv: drv}, m), session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.CloseContext(context.WithValue(context.Background(), closeValueKey{}, "caller")); err != nil {
		t.Fatalf("CloseContext() error = %v", err)
	}

	if got := <-drv.seen; got != "caller" {
		t.Fatalf("disconnect context value = %v, want the caller's value", got)
	}
}
