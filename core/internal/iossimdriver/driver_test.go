package iossimdriver_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/drivertest"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/iossimdriver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/version"
	"dev.atomyx/internal/wire"
)

// wireBuildPoint constructs a wire.Point, shortening the call sites
// that build one throughout this file.
func wireBuildPoint(x, y float64) wire.Point { return wire.Point{X: x, Y: y} }

// almostEqual asserts got is within a small tolerance of want,
// absorbing floating-point division rounding from the normalize
// step.
func almostEqual(t *testing.T, got any, want float64) {
	t.Helper()
	gotF, ok := got.(float64)
	if !ok {
		t.Fatalf("value = %v (%T), want a float64 close to %v", got, got, want)
	}
	const epsilon = 1e-9
	diff := gotF - want
	if diff < -epsilon || diff > epsilon {
		t.Fatalf("value = %v, want %v within %v", gotF, want, epsilon)
	}
}

// fakeConn implements iosdriver.Conn over an in-memory command table,
// mirroring the fake iosdriver_test uses for its own contract suite,
// so IosSimDriver's embedded IosDriver delegate is exercised without
// any real XCUITest transport.
type fakeConn struct {
	mu sync.Mutex

	connected bool
	handlers  map[string]func(args map[string]any) (map[string]any, error)
	calls     []fakeCall
}

type fakeCall struct {
	cmdType string
	args    map[string]any
}

// newFakeConn returns a conn whose ping reports the host's protocol
// version, so Connect succeeds unless a test overrides the reply.
func newFakeConn() *fakeConn {
	c := &fakeConn{handlers: map[string]func(args map[string]any) (map[string]any, error){}}
	c.Respond("ping", func(map[string]any) (map[string]any, error) {
		return map[string]any{"protocolVersion": version.AgentProtocolVersion, "capabilities": map[string]any{}}, nil
	})
	return c
}

func (f *fakeConn) Respond(cmdType string, handler func(args map[string]any) (map[string]any, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[cmdType] = handler
}

func (f *fakeConn) Calls() []fakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeCall(nil), f.calls...)
}

func (f *fakeConn) Connect(context.Context) error {
	f.mu.Lock()
	f.connected = true
	f.mu.Unlock()
	return nil
}

func (f *fakeConn) Close() error {
	f.mu.Lock()
	f.connected = false
	f.mu.Unlock()
	return nil
}

func (f *fakeConn) IsConnected() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connected
}

func (f *fakeConn) CallInto(_ context.Context, cmdType string, args map[string]any, out any) error {
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{cmdType: cmdType, args: args})
	handler := f.handlers[cmdType]
	f.mu.Unlock()

	var data map[string]any
	if handler != nil {
		var err error
		data, err = handler(args)
		if err != nil {
			return err
		}
	}
	if out == nil {
		return nil
	}
	return remarshal(data, out)
}

var _ iosdriver.Conn = (*fakeConn)(nil)

// newDefaultFakeConn wires the responses the driver contract suite
// needs from any driver: a positive screen size, since IosSimDriver's
// touch path also depends on it for normalization.
func newDefaultFakeConn() *fakeConn {
	c := newFakeConn()
	c.Respond("getScreenSize", func(map[string]any) (map[string]any, error) {
		return map[string]any{"width": 390.0, "height": 844.0}, nil
	})
	c.Respond("dispatchPointer", func(args map[string]any) (map[string]any, error) {
		if !dispatchPointerArgsHaveCapture(args) {
			return map[string]any{}, nil
		}
		return map[string]any{
			"capturedImages": []map[string]any{
				{"base64": base64.StdEncoding.EncodeToString(fakeCapturedImagePNG), "mimeType": "image/png"},
			},
		}, nil
	})
	return c
}

// fakeCapturedImagePNG stands in for the runner's real screenshot
// bytes in tests that only assert a capture waypoint round-trips
// into a non-empty GestureResult.CapturedImages entry.
var fakeCapturedImagePNG = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

// dispatchPointerArgsHaveCapture reports whether a dispatchPointer
// call's args contain at least one waypoint whose phase is "capture",
// mirroring the shape IosDriver.DispatchGesture builds.
func dispatchPointerArgsHaveCapture(args map[string]any) bool {
	pointers, _ := args["pointers"].([]map[string]any)
	for _, p := range pointers {
		waypoints, _ := p["waypoints"].([]map[string]any)
		for _, wp := range waypoints {
			if wp["phase"] == string(driver.GesturePhaseCapture) {
				return true
			}
		}
	}
	return false
}

// simContractRunner is a stateful process.Runner test double tracking
// installed app ids across install and uninstall calls, the way a
// real simulator does, so the contract suite's install and uninstall
// roundtrip observes consistent state.
type simContractRunner struct {
	mu        sync.Mutex
	installed map[string]bool
}

func newSimContractRunner() *simContractRunner {
	return &simContractRunner{installed: map[string]bool{}}
}

func (r *simContractRunner) Run(ctx context.Context, name string, args ...string) (process.Result, error) {
	if err := ctx.Err(); err != nil {
		return process.Result{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case name == "xcrun" && len(args) == 4 && args[0] == "simctl" && args[1] == "install":
		r.installed[args[3]] = true
		return process.Result{}, nil
	case name == "xcrun" && len(args) == 4 && args[0] == "simctl" && args[1] == "uninstall":
		delete(r.installed, args[3])
		return process.Result{}, nil
	case name == "xcrun" && len(args) == 5 && args[0] == "simctl" && args[1] == "get_app_container":
		appID := args[3]
		if r.installed[appID] {
			return process.Result{Stdout: "/path/to/App.app"}, nil
		}
		return process.Result{Stderr: "get_app_container: is not installed"}, errors.New("exit status 2")
	}
	return process.Result{}, nil
}

var _ process.Runner = (*simContractRunner)(nil)

// newContractSimDriver constructs an IosSimDriver wired to a fresh
// fakeConn, simContractRunner, and an in-memory HID connection (via
// fakeHidServer over a net.Pipe) for one drivertest sub-test. The
// device-free HID server always replies ok:true, so the contract
// suite's touch-related context-cancellation assertions exercise
// IosSimDriver.Tap/LongPress/Swipe for real, without ever opening a
// socket. It also returns the fakeConn backing the driver's embedded
// IosDriver delegate, so a caller can reconfigure its command
// responses (for example, to fail the next "ping" round trip).
func newContractSimDriver(t *testing.T) (driver.Driver, *fakeConn) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	srv := newFakeHidServer(serverConn)
	go srv.run()
	t.Cleanup(func() { _ = clientConn.Close() })

	conn := newDefaultFakeConn()
	d, err := iossimdriver.New(conn, newSimContractRunner(), &fakeDialer{conn: clientConn}, iossimdriver.Options{
		Options: iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator},
		HidPort: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return d, conn
}

func TestIosSimDriver_SatisfiesContract(t *testing.T) {
	var conn *fakeConn
	drivertest.Run(t, func() driver.Driver {
		var d driver.Driver
		d, conn = newContractSimDriver(t)
		return d
	}, drivertest.Hooks{
		DeviceCalls: func() int { return len(conn.Calls()) },
		FailPing: func() {
			conn.Respond("ping", func(map[string]any) (map[string]any, error) {
				return nil, errors.New("drivertest: simulated unreachable agent")
			})
		},
	})
}

func TestIosSimDriver_CurrentForeground_DelegatesUnsupportedToXCUITest(t *testing.T) {
	d, _ := newContractSimDriver(t)
	if d.Capabilities().CanQueryForeground {
		t.Fatal("Capabilities().CanQueryForeground = true, want false")
	}
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := d.CurrentForeground(context.Background()); err == nil {
		t.Fatal("CurrentForeground succeeded, want the unsupported error delegated from the embedded driver")
	}
}

func TestNew_RequiresHidPort(t *testing.T) {
	_, err := iossimdriver.New(newFakeConn(), process.NewFake(), iossimdriver.NewDialer(), iossimdriver.Options{
		Options: iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator},
	})
	if err == nil {
		t.Fatal("New() error = nil, want an error when HidPort is zero")
	}
}

func TestIosSimDriver_Tap_RoutesThroughHidNotXCUITest(t *testing.T) {
	conn := newDefaultFakeConn()
	clientConn, serverConn := net.Pipe()
	srv := newFakeHidServer(serverConn)
	go srv.run()
	defer func() { _ = clientConn.Close() }()

	d, err := iossimdriver.New(conn, process.NewFake(), &fakeDialer{conn: clientConn}, iossimdriver.Options{
		Options: iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator},
		HidPort: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	if err := d.Tap(context.Background(), wireBuildPoint(195, 422)); err != nil {
		t.Fatalf("Tap() error = %v", err)
	}

	for _, call := range conn.Calls() {
		if call.cmdType == "tapAt" {
			t.Fatalf("XCUITest conn received %+v, want Tap to route through HID only", call)
		}
	}
	received := srv.Received()
	if len(received) != 1 || received[0]["type"] != "tap" {
		t.Fatalf("HID server received = %+v, want one tap message", received)
	}
	// Screen size is 390x844; a tap at (195, 422) is the exact center.
	if received[0]["x"] != 0.5 || received[0]["y"] != 0.5 {
		t.Fatalf("tap message = %+v, want x=0.5 y=0.5 after normalization", received[0])
	}
}

// TestIosSimDriver_Tap_RefetchesScreenSizeAfterTTLElapses guards
// against a stale cached screen size surviving a simulator rotation.
// The first tap populates the cache; a second tap issued before the
// TTL elapses must reuse it; once the fake clock advances well past
// any plausible TTL, a third tap must re-fetch and normalize against
// the new (here, swapped) dimensions instead of the stale ones.
func TestIosSimDriver_Tap_RefetchesScreenSizeAfterTTLElapses(t *testing.T) {
	var getScreenSizeCalls int
	conn := newFakeConn()
	conn.Respond("getScreenSize", func(map[string]any) (map[string]any, error) {
		getScreenSizeCalls++
		if getScreenSizeCalls == 1 {
			return map[string]any{"width": 390.0, "height": 844.0}, nil
		}
		// Simulate the simulator having rotated to landscape between
		// the cache's first population and this re-fetch.
		return map[string]any{"width": 844.0, "height": 390.0}, nil
	})
	clientConn, serverConn := net.Pipe()
	srv := newFakeHidServer(serverConn)
	go srv.run()
	defer func() { _ = clientConn.Close() }()

	fakeClock := clock.NewFakeClock(0)
	d, err := iossimdriver.New(conn, process.NewFake(), &fakeDialer{conn: clientConn}, iossimdriver.Options{
		Options: iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator, Clock: fakeClock},
		HidPort: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	if err := d.Tap(context.Background(), wireBuildPoint(195, 422)); err != nil {
		t.Fatalf("Tap() error = %v", err)
	}
	if getScreenSizeCalls != 1 {
		t.Fatalf("getScreenSize calls = %d, want 1 after the first Tap", getScreenSizeCalls)
	}

	if err := d.Tap(context.Background(), wireBuildPoint(195, 422)); err != nil {
		t.Fatalf("Tap() error = %v", err)
	}
	if getScreenSizeCalls != 1 {
		t.Fatalf("getScreenSize calls = %d, want still 1 within the TTL window", getScreenSizeCalls)
	}

	// Advance well past any plausible TTL rather than asserting the
	// exact bound, so this test does not need to know the driver's
	// internal constant.
	const wellPastAnyTTLMs = 3_600_000
	fakeClock.Advance(wellPastAnyTTLMs)

	if err := d.Tap(context.Background(), wireBuildPoint(195, 422)); err != nil {
		t.Fatalf("Tap() error = %v", err)
	}
	if getScreenSizeCalls != 2 {
		t.Fatalf("getScreenSize calls = %d, want 2 once the TTL has elapsed", getScreenSizeCalls)
	}

	received := srv.Received()
	if len(received) != 3 {
		t.Fatalf("HID server received = %+v, want 3 tap messages", received)
	}
	// The same absolute point normalizes against the swapped 844x390
	// landscape size, not the stale 390x844 portrait size.
	almostEqual(t, received[2]["x"], 195.0/844.0)
	almostEqual(t, received[2]["y"], 422.0/390.0)
}

func TestIosSimDriver_LongPress_UsesDurationAsHoldMs(t *testing.T) {
	conn := newDefaultFakeConn()
	clientConn, serverConn := net.Pipe()
	srv := newFakeHidServer(serverConn)
	go srv.run()
	defer func() { _ = clientConn.Close() }()

	d, err := iossimdriver.New(conn, process.NewFake(), &fakeDialer{conn: clientConn}, iossimdriver.Options{
		Options: iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator},
		HidPort: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := d.LongPress(context.Background(), wireBuildPoint(0, 0), 1200); err != nil {
		t.Fatalf("LongPress() error = %v", err)
	}
	received := srv.Received()
	if len(received) != 1 || received[0]["type"] != "tap" || received[0]["holdMs"] != float64(1200) {
		t.Fatalf("HID server received = %+v, want a tap message with holdMs=1200", received)
	}
}

func TestIosSimDriver_Swipe_NormalizesBothEndpoints(t *testing.T) {
	conn := newDefaultFakeConn()
	clientConn, serverConn := net.Pipe()
	srv := newFakeHidServer(serverConn)
	go srv.run()
	defer func() { _ = clientConn.Close() }()

	d, err := iossimdriver.New(conn, process.NewFake(), &fakeDialer{conn: clientConn}, iossimdriver.Options{
		Options: iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator},
		HidPort: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	// Screen 390x844: (39, 84.4) -> (0.1, 0.1); (351, 759.6) -> (0.9, 0.9).
	if err := d.Swipe(context.Background(), wireBuildPoint(39, 84.4), wireBuildPoint(351, 759.6), 160); err != nil {
		t.Fatalf("Swipe() error = %v", err)
	}
	msg := srv.Received()[0]
	if msg["type"] != "swipe" {
		t.Fatalf("HID server received = %+v, want a swipe message", msg)
	}
	almostEqual(t, msg["x1"], 0.1)
	almostEqual(t, msg["y1"], 0.1)
	almostEqual(t, msg["x2"], 0.9)
	almostEqual(t, msg["y2"], 0.9)
}

func TestIosSimDriver_DispatchGesture_DelegatesToXCUITest(t *testing.T) {
	conn := newDefaultFakeConn()
	clientConn, serverConn := net.Pipe()
	srv := newFakeHidServer(serverConn)
	go srv.run()
	defer func() { _ = clientConn.Close() }()

	d, err := iossimdriver.New(conn, process.NewFake(), &fakeDialer{conn: clientConn}, iossimdriver.Options{
		Options: iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator},
		HidPort: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{{ID: "a"}, {ID: "b"}}}
	if _, err := d.DispatchGesture(context.Background(), gesture); err == nil {
		t.Fatal("DispatchGesture() error = nil, want the delegate's multi-pointer capability rejection")
	}
	if len(srv.Received()) != 0 {
		t.Fatalf("HID server received = %+v, want DispatchGesture to never reach HID", srv.Received())
	}
}

// TestIosSimDriver_DispatchGesture_ForwardsCaptureThroughDelegate
// asserts a mid-gesture capture waypoint routes through the embedded
// IosDriver delegate the same way a plain gesture does: the decoded
// image comes back on GestureResult unchanged, and the gesture never
// reaches the HID server, which has no capture primitive of its own.
func TestIosSimDriver_DispatchGesture_ForwardsCaptureThroughDelegate(t *testing.T) {
	imageBytes := []byte("fake-png-bytes")
	conn := newDefaultFakeConn()
	conn.Respond("dispatchPointer", func(map[string]any) (map[string]any, error) {
		return map[string]any{
			"capturedImages": []map[string]any{
				{"base64": base64.StdEncoding.EncodeToString(imageBytes), "mimeType": "image/png"},
			},
		}, nil
	})
	clientConn, serverConn := net.Pipe()
	srv := newFakeHidServer(serverConn)
	go srv.run()
	defer func() { _ = clientConn.Close() }()

	d, err := iossimdriver.New(conn, process.NewFake(), &fakeDialer{conn: clientConn}, iossimdriver.Options{
		Options: iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator},
		HidPort: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{
		{ID: "single", Waypoints: []driver.GestureWaypoint{
			{Phase: driver.GesturePhaseDown, Point: wire.Point{X: 1, Y: 1}},
			{Phase: driver.GesturePhaseCapture, Point: wire.Point{X: 1, Y: 1}},
			{Phase: driver.GesturePhaseUp, Point: wire.Point{X: 1, Y: 1}},
		}},
	}}
	result, err := d.DispatchGesture(context.Background(), gesture)
	if err != nil {
		t.Fatalf("DispatchGesture() error = %v", err)
	}
	if len(result.CapturedImages) != 1 || string(result.CapturedImages[0].Data) != string(imageBytes) {
		t.Fatalf("CapturedImages = %+v, want one entry with data %q", result.CapturedImages, imageBytes)
	}
	if len(srv.Received()) != 0 {
		t.Fatalf("HID server received = %+v, want DispatchGesture to never reach HID", srv.Received())
	}
}

func TestIosSimDriver_StreamingTouch_RoutesThroughHid(t *testing.T) {
	conn := newDefaultFakeConn()
	clientConn, serverConn := net.Pipe()
	srv := newFakeHidServer(serverConn)
	go srv.run()
	defer func() { _ = clientConn.Close() }()

	d, err := iossimdriver.New(conn, process.NewFake(), &fakeDialer{conn: clientConn}, iossimdriver.Options{
		Options: iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator},
		HidPort: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	streaming, ok := driver.AsStreamingTouchCapable(d)
	if !ok {
		t.Fatal("AsStreamingTouchCapable() = false, want IosSimDriver to satisfy StreamingTouchCapable")
	}
	ctx := context.Background()
	if err := streaming.StreamingTouchDown(ctx, wireBuildPoint(0, 0), 7); err != nil {
		t.Fatalf("StreamingTouchDown() error = %v", err)
	}
	if err := streaming.StreamingTouchMove(ctx, wireBuildPoint(39, 84.4), 7); err != nil {
		t.Fatalf("StreamingTouchMove() error = %v", err)
	}
	if err := streaming.StreamingTouchUp(ctx, wireBuildPoint(39, 84.4), 7); err != nil {
		t.Fatalf("StreamingTouchUp() error = %v", err)
	}
	received := srv.Received()
	wantTypes := []string{"touch-down", "touch-move", "touch-up"}
	if len(received) != 3 {
		t.Fatalf("HID server received = %+v, want 3 messages", received)
	}
	for i, want := range wantTypes {
		if received[i]["type"] != want {
			t.Fatalf("message[%d].type = %v, want %q", i, received[i]["type"], want)
		}
	}
}

func TestIosSimDriver_Hierarchy_DelegatesToXCUITest(t *testing.T) {
	conn := newDefaultFakeConn()
	conn.Respond("dumpRawTree", func(map[string]any) (map[string]any, error) {
		return map[string]any{"root": map[string]any{"elementType": "window"}}, nil
	})
	clientConn, serverConn := net.Pipe()
	srv := newFakeHidServer(serverConn)
	go srv.run()
	defer func() { _ = clientConn.Close() }()

	d, err := iossimdriver.New(conn, process.NewFake(), &fakeDialer{conn: clientConn}, iossimdriver.Options{
		Options: iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator},
		HidPort: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	tree, err := d.Hierarchy(context.Background())
	if err != nil {
		t.Fatalf("Hierarchy() error = %v", err)
	}
	if tree.Attributes["class"] != "window" {
		t.Fatalf("Hierarchy() = %+v, want the XCUITest-decoded root", tree)
	}
	if len(srv.Received()) != 0 {
		t.Fatalf("HID server received = %+v, want Hierarchy to never reach HID", srv.Received())
	}
}

func remarshal(data map[string]any, out any) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out)
}

func TestIosSimDriver_AgentInfo_ReportsWhatTheDelegateRunnerDeclared(t *testing.T) {
	d, conn := newContractSimDriver(t)
	conn.Respond("ping", func(map[string]any) (map[string]any, error) {
		return map[string]any{"protocolVersion": version.AgentProtocolVersion, "agentVersion": "3.1", "capabilities": map[string]any{}}, nil
	})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	want := driver.AgentInfo{ProtocolVersion: version.AgentProtocolVersion, AgentVersion: "3.1"}
	if got := d.AgentInfo(); got != want {
		t.Fatalf("AgentInfo() = %+v, want %+v", got, want)
	}
}

func TestIosSimDriver_Connect_RunnerProtocolMismatch_FailsWithAgentVersionMismatch(t *testing.T) {
	d, conn := newContractSimDriver(t)
	conn.Respond("ping", func(map[string]any) (map[string]any, error) {
		return map[string]any{"protocolVersion": version.AgentProtocolVersion + 1, "capabilities": map[string]any{}}, nil
	})

	err := d.Connect(context.Background())

	if code, _, _ := diag.Describe(err); code != diag.CodeAgentVersionMismatch {
		t.Fatalf("Connect() error = %v, want code %s", err, diag.CodeAgentVersionMismatch)
	}
}
