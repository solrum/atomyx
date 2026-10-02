package sidecar

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/session/sessiontest"
)

// orderRecordingFactory wraps a fresh mock.Driver per Build call in an
// orderRecordingDriver, appending "build" and "dispose" markers to a
// shared, mutex-guarded log. A test uses the log to assert the
// relative order of two Select calls' Build and Dispose, which a
// mock-vs-mock differential cannot observe because it never
// contends over a live driver resource.
type orderRecordingFactory struct {
	mu  sync.Mutex
	log *[]string
}

func newOrderRecordingFactory(log *[]string) *orderRecordingFactory {
	return &orderRecordingFactory{log: log}
}

var _ device.Factory = (*orderRecordingFactory)(nil)

func (f *orderRecordingFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f *orderRecordingFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	f.mu.Lock()
	*f.log = append(*f.log, "build")
	f.mu.Unlock()

	drv := mock.New()
	if err := drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	wrapped := &orderRecordingDriver{Driver: drv, factory: f}
	return device.Built{
		Driver:    wrapped,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: orchestra.SystemClock{}}),
	}, nil
}

// orderRecordingDriver embeds *mock.Driver so it satisfies
// driver.Driver through method promotion, overriding only Disconnect
// to record when teardown actually runs.
type orderRecordingDriver struct {
	*mock.Driver
	factory *orderRecordingFactory
}

func (d *orderRecordingDriver) Disconnect() error {
	d.factory.mu.Lock()
	*d.factory.log = append(*d.factory.log, "dispose")
	d.factory.mu.Unlock()
	return d.Driver.Disconnect()
}

// TestDevice_SelectDevice_SwitchingWhileConnected_DisposesPreviousBeforeBuildingNext
// guards against building the replacement driver while the outgoing
// one is still holding its platform control channel open. The two
// must never overlap, since an Android or iOS agent accepts only one
// live connection.
func TestDevice_SelectDevice_SwitchingWhileConnected_DisposesPreviousBeforeBuildingNext(t *testing.T) {
	ctx := newTestContext()
	var log []string
	factory := newOrderRecordingFactory(&log)

	first := device.Descriptor{ID: "device-1", Platform: device.PlatformAndroid, Name: "Device 1", Kind: device.KindDevice, State: device.StateOnline}
	second := device.Descriptor{ID: "device-2", Platform: device.PlatformAndroid, Name: "Device 2", Kind: device.KindDevice, State: device.StateOnline}
	probe := device.NewStaticProbe([]device.Descriptor{first, second})
	RegisterDeviceFeature(ctx, probe, factory)

	selectDevice := func(id string) {
		t.Helper()
		params, _ := json.Marshal(map[string]string{"id": id})
		if _, err := ctx.Dispatcher.Dispatch(context.Background(), "selectDevice", params); err != nil {
			t.Fatalf("selectDevice(%q): %v", id, err)
		}
	}

	selectDevice(first.ID)
	selectDevice(second.ID)

	want := []string{"build", "dispose", "build"}
	if len(log) != len(want) {
		t.Fatalf("log = %v, want %v", log, want)
	}
	for i, event := range want {
		if log[i] != event {
			t.Fatalf("log = %v, want %v", log, want)
		}
	}
}

// blockingFactory blocks every Build call on release, letting a test
// hold two concurrent selectDevice calls open at once and count how
// many actually entered Build.
type blockingFactory struct {
	buildCount int32
	entered    chan struct{}
	enteredOne sync.Once
	release    chan struct{}
}

func newBlockingFactory() *blockingFactory {
	return &blockingFactory{entered: make(chan struct{}), release: make(chan struct{})}
}

var _ device.Factory = (*blockingFactory)(nil)

func (f *blockingFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
}

func (f *blockingFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	atomic.AddInt32(&f.buildCount, 1)
	f.enteredOne.Do(func() { close(f.entered) })
	<-f.release

	drv := mock.New()
	if err := drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	return device.Built{
		Driver:    drv,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: drv, Clock: orchestra.SystemClock{}}),
	}, nil
}

// TestDevice_SelectDevice_ConcurrentSameID_BuildsExactlyOnce guards
// against the specific leak two concurrent selectDevice calls for the
// same id can otherwise cause: both passing the "already selected"
// check before either has installed its selection, so both call
// factory.Build and the losing call's live driver connection is never
// disposed. With the id's selection made atomic, the second call must
// block behind the first rather than building its own driver.
func TestDevice_SelectDevice_ConcurrentSameID_BuildsExactlyOnce(t *testing.T) {
	ctx := newTestContext()
	desc := device.Descriptor{ID: "device-1", Platform: device.PlatformAndroid, Name: "Device 1", Kind: device.KindDevice, State: device.StateOnline}
	probe := device.NewStaticProbe([]device.Descriptor{desc})
	factory := newBlockingFactory()
	RegisterDeviceFeature(ctx, probe, factory)

	selectDevice := func() error {
		params, _ := json.Marshal(map[string]string{"id": desc.ID})
		_, err := ctx.Dispatcher.Dispatch(context.Background(), "selectDevice", params)
		return err
	}

	firstDone := make(chan error, 1)
	go func() { firstDone <- selectDevice() }()

	select {
	case <-factory.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first selectDevice never reached factory.Build")
	}

	secondDone := make(chan error, 1)
	go func() { secondDone <- selectDevice() }()

	// Give the second call every chance to race into its own
	// factory.Build before the first releases: with atomic selection
	// it instead blocks behind the first call's lock, so buildCount
	// must still read 1 here.
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&factory.buildCount); got != 1 {
		t.Fatalf("factory.Build was called %d times concurrently for the same id, want 1 (a second concurrent selectDevice raced into its own Build instead of blocking behind the first)", got)
	}

	close(factory.release)

	if err := <-firstDone; err != nil {
		t.Fatalf("first selectDevice: Dispatch() error = %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second selectDevice: Dispatch() error = %v", err)
	}

	if got := atomic.LoadInt32(&factory.buildCount); got != 1 {
		t.Fatalf("factory.Build was called %d times total for concurrent selects of the same id, want 1", got)
	}
}
