package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/storage"
)

func newCloseTestServer(factory device.Factory) *Server {
	return NewServer(Info{Name: "atomyx-test", Version: "0.0.0-test"}, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: factory,
		Storage: storage.NewInMemoryStorage(),
	})
}

func androidInput() SelectDeviceInput {
	return SelectDeviceInput{Platform: device.PlatformAndroid, ID: "emulator-5554"}
}

func TestServerCloseDisconnectsTheActiveDevice(t *testing.T) {
	drv := mockdriver.New()
	server := newCloseTestServer(singleMockDriverFactory{driver: drv})
	if _, err := server.session.Select(context.Background(), androidInput()); err != nil {
		t.Fatalf("Select: %v", err)
	}
	if !drv.IsConnected() {
		t.Fatal("driver is not connected after Select")
	}

	server.Close()

	if drv.IsConnected() {
		t.Fatal("driver is still connected after Close")
	}
	if server.session.Current() != nil {
		t.Fatal("session still reports an active device after Close")
	}
	server.Close()
}

// gatedFactory holds Build open until release is closed, modeling a
// device selection that is still launching when the server stops.
type gatedFactory struct {
	driver  *mockdriver.Driver
	started chan struct{}
	release chan struct{}
}

func (f gatedFactory) Build(ctx context.Context, desc device.Descriptor) (device.Built, error) {
	close(f.started)
	<-f.release
	return singleMockDriverFactory{driver: f.driver}.Build(ctx, desc)
}

func TestServerCloseDuringSelectReleasesTheLateDriver(t *testing.T) {
	drv := mockdriver.New()
	factory := gatedFactory{driver: drv, started: make(chan struct{}), release: make(chan struct{})}
	server := newCloseTestServer(factory)

	selectErr := make(chan error, 1)
	go func() {
		_, err := server.session.Select(context.Background(), androidInput())
		selectErr <- err
	}()
	<-factory.started

	server.Close()
	close(factory.release)

	select {
	case err := <-selectErr:
		if !errors.Is(err, errSessionClosed) {
			t.Fatalf("Select error = %v, want errSessionClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Select did not return after the build was released")
	}
	if drv.IsConnected() {
		t.Fatal("driver built after Close was left connected")
	}
	if server.session.Current() != nil {
		t.Fatal("session bound a device after Close")
	}
}
