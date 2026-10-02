package mcp

import (
	"context"
	"testing"

	"dev.atomyx/internal/device"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/storage"
	"dev.atomyx/internal/wire"
)

// clockedMockDriverFactory is a device.Factory that always returns
// the same pre-staged mock.Driver, wired to a caller-supplied clock
// instead of the real-time SystemClock. A test uses a FakeClock here
// so tap_and_wait_transition's poll loop advances deterministically
// instead of waiting on real time.
type clockedMockDriverFactory struct {
	driver *mockdriver.Driver
	clock  orchestra.Clock
}

func (f clockedMockDriverFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	if err := f.driver.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	return device.Built{
		Driver:    f.driver,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: f.driver, Clock: f.clock}),
	}, nil
}

// loginButtonNode's bounds sit inside the mock driver's default
// screen and viewport-inset window (60pt top, 50pt bottom, on a
// 430x932 screen), so the scroll-into-view pass that precedes every
// selector-based tap treats it as already visible.
func loginButtonNode() wire.TreeNodeWire {
	enabled, clickable := true, true
	return wire.TreeNodeWire{
		Attributes: map[string]string{"role": "button", "id": "login-button", "text": "Log in", "bounds": "0,100,100,150"},
		Enabled:    &enabled,
		Clickable:  &clickable,
	}
}

func TestTapAndWaitTransitionResolvesWhenAppearMatches(t *testing.T) {
	before := wire.TreeNodeWire{
		Attributes: map[string]string{"role": "container"},
		Children:   []wire.TreeNodeWire{loginButtonNode()},
	}
	after := wire.TreeNodeWire{
		Attributes: map[string]string{"role": "container"},
		Children: []wire.TreeNodeWire{
			loginButtonNode(),
			{Attributes: map[string]string{"role": "text", "id": "welcome", "text": "Welcome", "bounds": "0,200,100,250"}},
		},
	}

	drv := mockdriver.New()
	// Repeating "before" covers every hierarchy read the handler takes
	// ahead of the poll loop (the pre-tap snapshot, the tap target's
	// pre-state, and the tap pipeline's own read); "after" then sticks
	// for every read from that point on, so the poll resolves as soon
	// as it reaches it regardless of exactly how many reads preceded it.
	drv.StageHierarchyRepeated(before, 5).StageHierarchy(after)
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: clockedMockDriverFactory{driver: drv, clock: orchestra.NewFakeClock(0)},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "tap_and_wait_transition", map[string]any{
		"selector":      map[string]any{"id": "login-button"},
		"waitForAppear": map[string]any{"id": "welcome"},
	})
	if result.IsError {
		t.Fatalf("tap_and_wait_transition: unexpected isError: %v", decoded)
	}
	if decoded["ok"] != true {
		t.Fatalf("tap_and_wait_transition: got %v, want ok:true", decoded)
	}
	if decoded["resolvedBy"] != "id" {
		t.Fatalf("tap_and_wait_transition resolvedBy: got %v, want \"id\"", decoded["resolvedBy"])
	}
	if _, ok := decoded["waitedMs"].(float64); !ok {
		t.Fatalf("tap_and_wait_transition waitedMs: got %v, want a number", decoded["waitedMs"])
	}
	tapped := false
	for _, c := range drv.Calls {
		if c.Method == "Tap" {
			tapped = true
		}
	}
	if !tapped {
		t.Fatal("tap_and_wait_transition: driver did not record a Tap call")
	}
}

func TestTapAndWaitTransitionTimesOutWithoutLoadingSignal(t *testing.T) {
	tree := wire.TreeNodeWire{
		Attributes: map[string]string{"role": "container"},
		Children:   []wire.TreeNodeWire{loginButtonNode()},
	}

	drv := mockdriver.New()
	drv.StageHierarchy(tree)
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: clockedMockDriverFactory{driver: drv, clock: orchestra.NewFakeClock(0)},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "tap_and_wait_transition", map[string]any{
		"selector":      map[string]any{"id": "login-button"},
		"waitForAppear": map[string]any{"id": "welcome"},
		"timeoutMs":     float64(100),
		"maxTimeoutMs":  float64(100),
		"intervalMs":    float64(50),
	})
	if result.IsError {
		t.Fatalf("tap_and_wait_transition (timeout): unexpected isError: %v", decoded)
	}
	if decoded["ok"] != false {
		t.Fatalf("tap_and_wait_transition (timeout): got %v, want ok:false", decoded)
	}
	if decoded["waitedMs"] != float64(100) {
		t.Fatalf("tap_and_wait_transition (timeout) waitedMs: got %v, want 100 (the hard deadline)", decoded["waitedMs"])
	}
	if decoded["classification"] != "partial_transition" {
		t.Fatalf("tap_and_wait_transition (timeout) classification: got %v, want partial_transition", decoded["classification"])
	}
}

func TestTapAndWaitTransitionExtendsDeadlineWhileLoadingPersists(t *testing.T) {
	tree := wire.TreeNodeWire{
		Attributes: map[string]string{"role": "container"},
		Children: []wire.TreeNodeWire{
			loginButtonNode(),
			{Attributes: map[string]string{"role": "progressbar", "bounds": "10,110,20,120"}},
		},
	}

	drv := mockdriver.New()
	drv.StageHierarchy(tree)
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: clockedMockDriverFactory{driver: drv, clock: orchestra.NewFakeClock(0)},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "tap_and_wait_transition", map[string]any{
		"selector":      map[string]any{"id": "login-button"},
		"waitForAppear": map[string]any{"id": "welcome"},
		"timeoutMs":     float64(100),
		"maxTimeoutMs":  float64(300),
		"intervalMs":    float64(50),
	})
	if result.IsError {
		t.Fatalf("tap_and_wait_transition (extends deadline): unexpected isError: %v", decoded)
	}
	if decoded["ok"] != false {
		t.Fatalf("tap_and_wait_transition (extends deadline): got %v, want ok:false", decoded)
	}
	if decoded["waitedMs"] != float64(300) {
		t.Fatalf("tap_and_wait_transition (extends deadline) waitedMs: got %v, want 300 (the extended wait ran to the hard deadline)", decoded["waitedMs"])
	}
	if decoded["classification"] != "still_loading" {
		t.Fatalf("tap_and_wait_transition (extends deadline) classification: got %v, want still_loading", decoded["classification"])
	}
}

func TestTapAndWaitTransitionFlagsASecureAppearedElementWithoutItsText(t *testing.T) {
	password := map[string]string{
		"role": "text-field", "id": "pw", "label": "Password", "text": "hunter2", "secure": "true", "bounds": "0,200,100,250",
	}
	appeared := tapAndWaitAppearedPayload(orchestra.DiffAppeared(nil, orchestra.TreeNodeToCompactElements(
		wire.TreeNodeWire{Children: []wire.TreeNodeWire{{Attributes: password}}},
	)))

	if len(appeared) != 1 || appeared[0]["secure"] != true {
		t.Fatalf("appeared = %v, want one entry flagged secure", appeared)
	}
	selector := appeared[0]["selector"].(map[string]string)
	if _, has := selector["text"]; has || selector["resourceId"] != "pw" {
		t.Fatalf("selector = %v, want the id and no text", selector)
	}
}
