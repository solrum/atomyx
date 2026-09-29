package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/wire"
)

// fakeIosAgentStatus and fakeAndroidAgentStatus are the minimal test
// doubles mirrorInputService depends on for readiness: a fixed state
// reported for every device id.
type fakeIosAgentStatus struct{ state IosAgentState }

func (f fakeIosAgentStatus) Status(udid string) IosAgentStatus {
	return IosAgentStatus{UDID: udid, State: f.state}
}

type fakeAndroidAgentStatus struct{ state AndroidAgentState }

func (f fakeAndroidAgentStatus) Status(serial string) AndroidAgentStatus {
	return AndroidAgentStatus{Serial: serial, State: f.state}
}

// selectMockDeviceIOS mirrors selectMockDevice but installs an iOS
// descriptor, so a test can exercise mirrorInputService's iOS
// readiness branch. MockDriverFactory ignores the descriptor's
// platform and always builds a *mock.Driver wired to a real
// Orchestra, so the returned driver behaves identically to the
// Android case.
func selectMockDeviceIOS(t *testing.T, ctx *Context) *mock.Driver {
	t.Helper()
	desc := device.Descriptor{ID: "ios-mock-1", Platform: device.PlatformIOS, Name: "Mock iOS Device", Kind: device.KindSimulator, State: device.StateOnline}
	probe := device.NewStaticProbe([]device.Descriptor{desc}, []device.Descriptor{desc})
	factory := device.NewMockDriverFactory()
	RegisterDeviceFeature(ctx, probe, factory)

	params, _ := json.Marshal(map[string]string{"id": desc.ID})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "selectDevice", params); err != nil {
		t.Fatalf("selectDevice failed: %v", err)
	}

	selection := ctx.Session.Device()
	if selection == nil {
		t.Fatalf("selectDevice did not install a session device")
	}
	drv, ok := selection.Driver.(*mock.Driver)
	if !ok {
		t.Fatalf("session device driver = %T, want *mock.Driver", selection.Driver)
	}
	return drv
}

// streamingCapableFakeDriver embeds a nil driver.Driver and overrides
// only ScreenSize and the three streaming touch methods, so it
// satisfies both driver.Driver and driver.StreamingTouchCapable
// without implementing dozens of methods mirrorInputService's
// streaming path never calls.
type streamingCapableFakeDriver struct {
	driver.Driver
	screen wire.Size
	calls  []streamingCall
}

type streamingCall struct {
	method string
	point  wire.Point
	id     int
}

func (f *streamingCapableFakeDriver) ScreenSize(context.Context) (wire.Size, error) {
	return f.screen, nil
}

func (f *streamingCapableFakeDriver) StreamingTouchDown(_ context.Context, point wire.Point, id int) error {
	f.calls = append(f.calls, streamingCall{method: "down", point: point, id: id})
	return nil
}

func (f *streamingCapableFakeDriver) StreamingTouchMove(_ context.Context, point wire.Point, id int) error {
	f.calls = append(f.calls, streamingCall{method: "move", point: point, id: id})
	return nil
}

func (f *streamingCapableFakeDriver) StreamingTouchUp(_ context.Context, point wire.Point, id int) error {
	f.calls = append(f.calls, streamingCall{method: "up", point: point, id: id})
	return nil
}

var _ driver.StreamingTouchCapable = (*streamingCapableFakeDriver)(nil)

// ---------------------------------------------------------------------------
// Readiness gate
// ---------------------------------------------------------------------------

func TestMirrorInput_TapRatio_IosAgentBuilding_ReturnsDriverNotReadyError(t *testing.T) {
	ctx := newTestContext()
	selectMockDeviceIOS(t, ctx)
	svc := newMirrorInputService(ctx.Session, fakeIosAgentStatus{state: IosAgentStateBuilding}, fakeAndroidAgentStatus{state: AndroidAgentStateReady})

	err := svc.TapRatio(context.Background(), "UDID-BUILDING", 0.5, 0.5, "")

	var notReady *DriverNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("TapRatio() error = %v, want *DriverNotReadyError", err)
	}
	wantSubstr := "state=building"
	if !strings.Contains(notReady.Message, wantSubstr) {
		t.Fatalf("TapRatio() error message = %q, want it to contain %q", notReady.Message, wantSubstr)
	}
}

func TestMirrorInput_TapRatio_AndroidAgentInstalling_ReturnsDriverNotReadyError(t *testing.T) {
	ctx := newTestContext()
	selectMockDevice(t, ctx) // Android platform, per testing_helpers_test.go.
	svc := newMirrorInputService(ctx.Session, fakeIosAgentStatus{state: IosAgentStateReady}, fakeAndroidAgentStatus{state: AndroidAgentStateInstalling})

	err := svc.TapRatio(context.Background(), "SERIAL-INSTALLING", 0.5, 0.5, "")

	var notReady *DriverNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("TapRatio() error = %v, want *DriverNotReadyError", err)
	}
	wantSubstr := "state=installing"
	if !strings.Contains(notReady.Message, wantSubstr) {
		t.Fatalf("TapRatio() error message = %q, want it to contain %q", notReady.Message, wantSubstr)
	}
}

// TestMirrorInput_TapRatio_Ready_DispatchesTapAtScaledPoint covers
// both routing (the driver actually receives a Tap call) and the
// ratio-to-pixel conversion for the classified touch path.
func TestMirrorInput_TapRatio_Ready_DispatchesTapAtScaledPoint(t *testing.T) {
	ctx := newTestContext()
	drv := selectMockDevice(t, ctx)
	drv.Screen = wire.Size{Width: 400, Height: 800}
	svc := newMirrorInputService(ctx.Session, fakeIosAgentStatus{state: IosAgentStateReady}, fakeAndroidAgentStatus{state: AndroidAgentStateReady})

	if err := svc.TapRatio(context.Background(), "mock-1", 0.5, 0.5, ""); err != nil {
		t.Fatalf("TapRatio() error = %v", err)
	}

	var got wire.Point
	var found bool
	for _, c := range drv.Calls {
		if c.Method == "Tap" {
			found = true
			got = c.Args[0].(wire.Point)
		}
	}
	if !found {
		t.Fatalf("driver.Calls = %+v, want a Tap call", drv.Calls)
	}
	want := wire.Point{X: 200, Y: 400}
	if got != want {
		t.Fatalf("Tap point = %+v, want %+v", got, want)
	}
}

// ---------------------------------------------------------------------------
// Streaming touch capability gate
// ---------------------------------------------------------------------------

func TestMirrorInput_StreamingDown_DriverNotStreamingCapable_ReturnsStreamingTouchNotSupportedError(t *testing.T) {
	ctx := newTestContext()
	selectMockDeviceIOS(t, ctx) // *mock.Driver does not implement driver.StreamingTouchCapable.
	svc := newMirrorInputService(ctx.Session, fakeIosAgentStatus{state: IosAgentStateReady}, fakeAndroidAgentStatus{state: AndroidAgentStateReady})

	err := svc.StreamingDown(context.Background(), "UDID-NO-STREAMING", 0.5, 0.5, 1)

	var notSupported *StreamingTouchNotSupportedError
	if !errors.As(err, &notSupported) {
		t.Fatalf("StreamingDown() error = %v, want *StreamingTouchNotSupportedError", err)
	}
}

func TestMirrorInput_StreamingDown_AgentNotReady_ReturnsDriverNotReadyErrorBeforeCapabilityCheck(t *testing.T) {
	ctx := newTestContext()
	selectMockDeviceIOS(t, ctx)
	svc := newMirrorInputService(ctx.Session, fakeIosAgentStatus{state: IosAgentStateBuilding}, fakeAndroidAgentStatus{state: AndroidAgentStateReady})

	err := svc.StreamingDown(context.Background(), "UDID-AGENT-NOT-READY", 0.5, 0.5, 1)

	var notReady *DriverNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("StreamingDown() error = %v, want *DriverNotReadyError", err)
	}
}

// TestMirrorInput_StreamingDown_ConvertsRatioToPixel_NotDoubleNormalized
// is the regression test for the streaming double-normalize bug: the
// service must hand the driver a pixel point, not the raw ratio.
func TestMirrorInput_StreamingDown_ConvertsRatioToPixel_NotDoubleNormalized(t *testing.T) {
	ctx := newTestContext()
	fake := &streamingCapableFakeDriver{screen: wire.Size{Width: 400, Height: 800}}
	ctx.Session.SetDevice(&DeviceSelection{ID: "UDID-WITH-STREAMING", Platform: "ios", Driver: fake})
	svc := newMirrorInputService(ctx.Session, fakeIosAgentStatus{state: IosAgentStateReady}, fakeAndroidAgentStatus{state: AndroidAgentStateReady})

	if err := svc.StreamingDown(context.Background(), "UDID-WITH-STREAMING", 0.5, 0.5, 2); err != nil {
		t.Fatalf("StreamingDown() error = %v", err)
	}

	if len(fake.calls) != 1 {
		t.Fatalf("fake.calls = %+v, want exactly one call", fake.calls)
	}
	got := fake.calls[0]
	// A double-normalized bug would hand the driver the raw ratio
	// (0.5, 0.5) instead of the pixel point (200, 400) on a 400x800
	// screen.
	want := streamingCall{method: "down", point: wire.Point{X: 200, Y: 400}, id: 2}
	if got != want {
		t.Fatalf("streaming call = %+v, want %+v", got, want)
	}
}
