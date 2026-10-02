package mcp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/session/sessiontest"
)

const selectTestTimeout = 5 * time.Second

// sequencedFactory hands out one driver per Build call, in order. The
// first call blocks until release is closed, so a test can finish a
// later Select while the first is still building.
type sequencedFactory struct {
	drivers []*mockdriver.Driver
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (f *sequencedFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f *sequencedFactory) Build(ctx context.Context, desc device.Descriptor) (device.Built, error) {
	n := int(f.calls.Add(1)) - 1
	if n == 0 {
		close(f.started)
		<-f.release
	}
	return singleMockDriverFactory{driver: f.drivers[n]}.Build(ctx, desc)
}

func TestConcurrentSelectsRunOneAfterAnotherAndTheLaterOneWins(t *testing.T) {
	first, second := mockdriver.New(), mockdriver.New()
	factory := &sequencedFactory{
		drivers: []*mockdriver.Driver{first, second},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	session := newCloseTestServer(t, factory).session

	firstDone := make(chan error, 1)
	go func() {
		_, err := session.Select(context.Background(), androidInput())
		firstDone <- err
	}()
	<-factory.started
	secondDone := make(chan error, 1)
	go func() {
		_, err := session.Select(context.Background(), androidInput())
		secondDone <- err
	}()
	close(factory.release)

	for _, done := range []chan error{firstDone, secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Select: %v", err)
			}
		case <-time.After(selectTestTimeout):
			t.Fatal("a Select did not finish")
		}
	}

	if first.IsConnected() {
		t.Fatal("the driver the later Select displaced is still connected")
	}
	if !second.IsConnected() {
		t.Fatal("the current driver was disconnected")
	}
	current := session.Current()
	if current == nil {
		t.Fatal("no current device, want the second driver")
	}
	if got, _ := driver.As[*mockdriver.Driver](current.Driver); got != second {
		t.Fatalf("current device = %+v, want the second driver", current)
	}
}

func TestSelectOnAClosedSessionBuildsNothing(t *testing.T) {
	factory := &sequencedFactory{drivers: []*mockdriver.Driver{mockdriver.New()}}
	session := newCloseTestServer(t, factory).session
	session.Close()

	if _, err := session.Select(context.Background(), androidInput()); !errors.Is(err, errSessionClosed) {
		t.Fatalf("Select error = %v, want errSessionClosed", err)
	}
	if got := factory.calls.Load(); got != 0 {
		t.Fatalf("factory built %d drivers for a closed session, want 0", got)
	}
}
