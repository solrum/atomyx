package mcp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
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
	session := newCloseTestServer(factory).session

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
	if current := session.Current(); current == nil || current.Driver != second {
		t.Fatalf("current device = %+v, want the second driver", current)
	}
}

func TestSelectOnAClosedSessionBuildsNothing(t *testing.T) {
	factory := &sequencedFactory{drivers: []*mockdriver.Driver{mockdriver.New()}}
	session := newCloseTestServer(factory).session
	session.Close()

	if _, err := session.Select(context.Background(), androidInput()); !errors.Is(err, errSessionClosed) {
		t.Fatalf("Select error = %v, want errSessionClosed", err)
	}
	if got := factory.calls.Load(); got != 0 {
		t.Fatalf("factory built %d drivers for a closed session, want 0", got)
	}
}
