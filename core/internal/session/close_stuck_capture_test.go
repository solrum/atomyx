package session_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/session/sessiontest"
	"dev.atomyx/internal/wire"
)

const stuckCaptureGuard = 5 * time.Second

// stuckHierarchyDriver blocks Hierarchy, ignoring its context, until
// Disconnect is called, like a driver call wedged on a dead device.
type stuckHierarchyDriver struct {
	*mockdriver.Driver
	entered      chan struct{}
	enterOnce    sync.Once
	disconnected chan struct{}
	closeOnce    sync.Once
}

func (d *stuckHierarchyDriver) Hierarchy(context.Context) (wire.TreeNodeWire, error) {
	d.enterOnce.Do(func() { close(d.entered) })
	<-d.disconnected
	return wire.TreeNodeWire{}, context.Canceled
}

func (d *stuckHierarchyDriver) Disconnect() error {
	d.closeOnce.Do(func() { close(d.disconnected) })
	return d.Driver.Disconnect()
}

type stuckHierarchyFactory struct{ drv *stuckHierarchyDriver }

func (f *stuckHierarchyFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f *stuckHierarchyFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	if err := f.drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	return device.Built{
		Driver:    f.drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: f.drv, Clock: orchestra.SystemClock{}}),
	}, nil
}

func TestClose_DisconnectsEvenWhenAViewerCaptureIsStuckInTheDriver(t *testing.T) {
	m := newSocketManager(t, clock.SystemClock{})
	drv := &stuckHierarchyDriver{Driver: mockdriver.New(), entered: make(chan struct{}), disconnected: make(chan struct{})}
	s, err := session.Open(context.Background(), baseDeps(t, &stuckHierarchyFactory{drv: drv}, m), session.Request{Descriptor: androidDesc("A")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetIdle(); err != nil {
		t.Fatal(err)
	}
	viewer := sessiontest.DialViewer(t, socketPathFor(m), "stuck")
	go func() { _ = viewer.TreeFresh() }()
	select {
	case <-drv.entered:
	case <-time.After(stuckCaptureGuard):
		t.Fatal("the fresh capture never reached the driver")
	}

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case <-closed:
	case <-time.After(stuckCaptureGuard):
		t.Fatal("Close did not return while a capture was stuck in the driver")
	}
	select {
	case <-drv.disconnected:
	default:
		t.Fatal("Close returned without calling Disconnect")
	}
}
