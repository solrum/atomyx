package androiddriver_test

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/drivertest"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
	"dev.atomyx/internal/version"
	"dev.atomyx/internal/wire"
)

// fakeTransport implements androiddriver.Transport with
// stateful, in-memory behavior: LaunchApp and ForceStopApp update a
// tracked foreground app so CurrentActivity reflects it, matching how
// a real device behaves across the same sequence of calls.
type fakeTransport struct {
	mu sync.Mutex

	healthErr error
	pingResp  transport.PingResponse
	pingErr   error
	treeResp  transport.AndroidTreeNode
	treeErr   error

	foreground *string

	screenshotResp transport.ScreenshotResponse
	appsResp       []transport.InstalledApp

	clearFocusedResp transport.ClearFocusedInputResult
	clearFocusedErr  error
	hideKeyboardResp transport.AckResult

	// forceStopResp overrides ForceStopApp's response. Zero value
	// (Ok: false, no Reason) is never sent to a test: newFakeTransport
	// seeds it to {Ok: true} so ForceStopApp's default behavior stays
	// the happy-path stop-and-clear-foreground it had before refusal
	// became reportable.
	forceStopResp transport.AckResult

	lastTap        transport.TapCoordsRequest
	lastLongPres   transport.LongPressRequest
	lastSwipe      transport.SwipeRequest
	lastDispatch   transport.DispatchGestureRequest
	lastTypeKb     transport.TypeKeyboardRequest
	lastPressKey   transport.KeyRequest
	launchAppCalls []string

	calls atomic.Int64
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{
		pingResp:         currentAgentPing(),
		clearFocusedResp: transport.ClearFocusedInputResult{Ok: true},
		hideKeyboardResp: transport.AckResult{Ok: true},
		forceStopResp:    transport.AckResult{Ok: true},
		treeResp: transport.AndroidTreeNode{
			ElementID: "root",
			Children: []transport.AndroidTreeNode{
				{
					ElementID: "window",
					ClassName: "android.widget.FrameLayout",
					Bounds:    &transport.AndroidBounds{Left: 0, Top: 0, Right: 1080, Bottom: 2400},
				},
			},
		},
	}
}

func (f *fakeTransport) Health(context.Context) (transport.HealthResponse, error) {
	f.calls.Add(1)
	if f.healthErr != nil {
		return transport.HealthResponse{}, f.healthErr
	}
	return transport.HealthResponse{Ok: true, AccessibilityConnected: true}, nil
}

func (f *fakeTransport) Ping(context.Context) (transport.PingResponse, error) {
	f.calls.Add(1)
	if f.pingErr != nil {
		return transport.PingResponse{}, f.pingErr
	}
	return f.pingResp, nil
}

func (f *fakeTransport) Tree(context.Context) (transport.AndroidTreeNode, error) {
	f.calls.Add(1)
	if f.treeErr != nil {
		return transport.AndroidTreeNode{}, f.treeErr
	}
	return f.treeResp, nil
}

func (f *fakeTransport) TapCoords(_ context.Context, req transport.TapCoordsRequest) (transport.GestureResult, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.lastTap = req
	f.mu.Unlock()
	return transport.GestureResult{Ok: true}, nil
}

func (f *fakeTransport) LongPress(_ context.Context, req transport.LongPressRequest) (transport.GestureResult, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.lastLongPres = req
	f.mu.Unlock()
	return transport.GestureResult{Ok: true}, nil
}

func (f *fakeTransport) Swipe(_ context.Context, req transport.SwipeRequest) (transport.GestureResult, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.lastSwipe = req
	f.mu.Unlock()
	return transport.GestureResult{Ok: true}, nil
}

func (f *fakeTransport) DispatchGesture(_ context.Context, req transport.DispatchGestureRequest) (transport.DispatchGestureResult, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.lastDispatch = req
	f.mu.Unlock()
	pointers := len(req.Pointers)
	result := transport.DispatchGestureResult{Ok: true, Pointers: &pointers}
	if requestHasCaptureWaypoint(req) {
		result.CapturedImages = []transport.CapturedGestureImage{
			{Base64: base64.StdEncoding.EncodeToString(fakeCapturedImageJPEG), MimeType: "image/jpeg"},
		}
	}
	return result, nil
}

// fakeCapturedImageJPEG stands in for the agent's real screenshot
// bytes in tests that only assert a capture waypoint round-trips into
// a non-empty GestureResult.CapturedImages entry.
var fakeCapturedImageJPEG = []byte{0xff, 0xd8, 0xff, 0xd9}

// requestHasCaptureWaypoint reports whether req carries at least one
// waypoint whose phase is "capture", mirroring the shape
// AndroidDriver.DispatchGesture builds.
func requestHasCaptureWaypoint(req transport.DispatchGestureRequest) bool {
	for _, p := range req.Pointers {
		for _, w := range p.Waypoints {
			if w.Phase == string(driver.GesturePhaseCapture) {
				return true
			}
		}
	}
	return false
}

func (f *fakeTransport) TypeKeyboard(_ context.Context, req transport.TypeKeyboardRequest) (transport.TypeKeyboardResult, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.lastTypeKb = req
	f.mu.Unlock()
	return transport.TypeKeyboardResult{Success: true, Typed: len(req.Text), Total: len(req.Text)}, nil
}

func (f *fakeTransport) ClearFocusedInput(context.Context) (transport.ClearFocusedInputResult, error) {
	f.calls.Add(1)
	if f.clearFocusedErr != nil {
		return transport.ClearFocusedInputResult{}, f.clearFocusedErr
	}
	return f.clearFocusedResp, nil
}

func (f *fakeTransport) HideKeyboard(context.Context) (transport.AckResult, error) {
	f.calls.Add(1)
	return f.hideKeyboardResp, nil
}

func (f *fakeTransport) PressKey(_ context.Context, req transport.KeyRequest) (transport.OkResponse, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.lastPressKey = req
	f.mu.Unlock()
	return transport.OkResponse{Ok: true}, nil
}

func (f *fakeTransport) LaunchApp(_ context.Context, req transport.LaunchRequest) (transport.OkResponse, error) {
	f.calls.Add(1)
	f.mu.Lock()
	id := req.PackageName
	f.foreground = &id
	f.launchAppCalls = append(f.launchAppCalls, req.PackageName)
	f.mu.Unlock()
	return transport.OkResponse{Ok: true}, nil
}

func (f *fakeTransport) ForceStopApp(_ context.Context, req transport.ForceStopRequest) (transport.AckResult, error) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.forceStopResp.Ok {
		return f.forceStopResp, nil
	}
	if f.foreground != nil && *f.foreground == req.PackageName {
		f.foreground = nil
	}
	return f.forceStopResp, nil
}

func (f *fakeTransport) CurrentActivity(context.Context) (transport.CurrentActivityResponse, error) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.foreground == nil {
		return transport.CurrentActivityResponse{}, nil
	}
	return transport.CurrentActivityResponse{PackageName: *f.foreground}, nil
}

func (f *fakeTransport) Apps(context.Context) ([]transport.InstalledApp, error) {
	f.calls.Add(1)
	return f.appsResp, nil
}

func (f *fakeTransport) Screenshot(context.Context) (transport.ScreenshotResponse, error) {
	f.calls.Add(1)
	return f.screenshotResp, nil
}

var _ androiddriver.Transport = (*fakeTransport)(nil)

// contractRunner is a stateful process.Runner test double that tracks
// installed app ids across install / uninstall / list-packages calls,
// the way a real device does, so the driver contract suite's install
// and uninstall roundtrip observes consistent state. Every other
// command (adb forward, pm clear) succeeds unconditionally.
type contractRunner struct {
	mu        sync.Mutex
	installed map[string]bool
}

func newContractRunner() *contractRunner {
	return &contractRunner{installed: make(map[string]bool)}
}

func (r *contractRunner) Run(ctx context.Context, name string, args ...string) (process.Result, error) {
	if err := ctx.Err(); err != nil {
		return process.Result{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case len(args) == 5 && args[2] == "install":
		r.installed[args[4]] = true
	case len(args) == 4 && args[2] == "uninstall":
		delete(r.installed, args[3])
	case len(args) == 7 && args[2] == "shell" && args[3] == "pm" && args[4] == "list" && args[5] == "packages":
		appID := args[6]
		if r.installed[appID] {
			return process.Result{Stdout: "package:" + appID + "\n"}, nil
		}
		return process.Result{}, nil
	}
	return process.Result{}, nil
}

var _ process.Runner = (*contractRunner)(nil)

func TestAndroidDriver_SatisfiesContract(t *testing.T) {
	var tr *fakeTransport
	drivertest.Run(t, func() driver.Driver {
		tr = newFakeTransport()
		return androiddriver.New(tr, newContractRunner(), androiddriver.Options{Serial: "emulator-5554"})
	}, drivertest.Hooks{
		DeviceCalls: func() int { return int(tr.calls.Load()) },
		FailConnect: func() {
			tr.mu.Lock()
			tr.healthErr = errors.New("drivertest: simulated unreachable agent")
			tr.mu.Unlock()
		},
		FailPing: func() {
			tr.mu.Lock()
			tr.healthErr = errors.New("drivertest: simulated unreachable agent")
			tr.mu.Unlock()
		},
	})
}

func TestAndroidDriver_Connect_ForwardsAdbTunnelThenHealthAndPing(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "forward", "tcp:8765", "tcp:8765"), process.Result{}, nil)

	tr := newFakeTransport()
	tr.pingResp = transport.PingResponse{
		Ok:              true,
		ProtocolVersion: version.AgentProtocolVersion,
		Capabilities: transport.PingCapabilities{
			CanMultiPointer: true,
			CanPressure:     true,
			SecureFields:    true,
		},
	}
	d := androiddriver.New(tr, runner, androiddriver.Options{Serial: "S1"})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !d.IsConnected() {
		t.Fatal("IsConnected() = false after Connect")
	}
	if !d.Capabilities().CanMultiPointer || !d.Capabilities().CanPressure {
		t.Fatalf("Capabilities() = %+v, want ping capabilities applied", d.Capabilities())
	}

	invocations := runner.Invocations()
	if len(invocations) != 3 {
		t.Fatalf("adb invocations = %+v, want the foreign-automation probe, the forward --list check, then exactly one forward call", invocations)
	}
	want := process.Invocation{Name: "adb", Args: []string{"-s", "S1", "forward", "tcp:8765", "tcp:8765"}}
	if invocations[2].Name != want.Name || len(invocations[2].Args) != len(want.Args) {
		t.Fatalf("adb invocation = %+v, want %+v", invocations[2], want)
	}
}

func TestAndroidDriver_Connect_CustomPorts_ForwardsThoseExactPorts(t *testing.T) {
	runner := process.NewFake()
	d := androiddriver.New(newFakeTransport(), runner, androiddriver.Options{Serial: "S1", HostPort: 9001, DevicePort: 8765})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	invocations := runner.Invocations()
	if len(invocations) != 3 {
		t.Fatalf("adb invocations = %+v, want the foreign-automation probe, the forward --list check, then exactly one forward call", invocations)
	}
	wantArgs := []string{"-s", "S1", "forward", "tcp:9001", "tcp:8765"}
	if !equalArgs(invocations[2].Args, wantArgs) {
		t.Fatalf("adb forward args = %v, want %v", invocations[2].Args, wantArgs)
	}
}

// TestAndroidDriver_Connect_ExistingMatchingForward_ReusesItWithoutRecreating
// proves Connect reuses a forward already pointing at this driver's
// serial and device port instead of recreating it, and that
// Disconnect then leaves it in place since this driver did not create
// it.
func TestAndroidDriver_Connect_ExistingMatchingForward_ReusesItWithoutRecreating(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "forward", "--list"),
		process.Result{Stdout: "S1 tcp:9001 tcp:8765\n"}, nil)
	d := androiddriver.New(newFakeTransport(), runner, androiddriver.Options{Serial: "S1", HostPort: 9001, DevicePort: 8765})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	invocations := runner.Invocations()
	if len(invocations) != 2 {
		t.Fatalf("adb invocations = %+v, want only the foreign-automation probe and the forward --list check, no new forward call for an already-matching forward", invocations)
	}

	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	invocations = runner.Invocations()
	if len(invocations) != 2 {
		t.Fatalf("adb invocations after Disconnect = %+v, want no forward --remove call for a forward this driver did not create", invocations)
	}
}

// TestAndroidDriver_Connect_ConflictingForwardDifferentSerial_ReturnsHostPortInUse
// proves Connect refuses to steal a local port another device already
// holds a forward on, rather than silently replacing it.
func TestAndroidDriver_Connect_ConflictingForwardDifferentSerial_ReturnsHostPortInUse(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "forward", "--list"),
		process.Result{Stdout: "S2 tcp:9001 tcp:8765\n"}, nil)
	d := androiddriver.New(newFakeTransport(), runner, androiddriver.Options{Serial: "S1", HostPort: 9001, DevicePort: 8765})

	err := d.Connect(context.Background())
	if err == nil {
		t.Fatal("Connect() error = nil, want a conflict when the local port already forwards to a different device")
	}
	if d.IsConnected() {
		t.Fatal("IsConnected() = true after a conflicting forward, want false")
	}
	code, message, remedy := diag.Describe(err)
	if code != diag.CodeHostPortInUse {
		t.Fatalf("Connect() code = %q, want %q", code, diag.CodeHostPortInUse)
	}
	if remedy == "" {
		t.Fatal("Connect() remedy is empty, want an actionable hint")
	}
	if strings.Contains(message, "9001") || strings.Contains(message, "8765") {
		t.Fatalf("Connect() message = %q, must not name the conflicting port numbers", message)
	}
	invocations := runner.Invocations()
	if len(invocations) != 2 {
		t.Fatalf("adb invocations = %+v, want the foreign-automation probe and the forward --list check, no forward call issued into the conflict", invocations)
	}
}

// TestAndroidDriver_Connect_ConflictingForwardDifferentDevicePort_ReturnsHostPortInUse
// proves the same conflict is caught when the local port belongs to
// this driver's own serial but points at a different device port,
// which is just as unsafe to silently replace.
func TestAndroidDriver_Connect_ConflictingForwardDifferentDevicePort_ReturnsHostPortInUse(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "forward", "--list"),
		process.Result{Stdout: "S1 tcp:9001 tcp:7000\n"}, nil)
	d := androiddriver.New(newFakeTransport(), runner, androiddriver.Options{Serial: "S1", HostPort: 9001, DevicePort: 8765})

	err := d.Connect(context.Background())
	if err == nil {
		t.Fatal("Connect() error = nil, want a conflict when the local port already forwards to a different device port")
	}
	code, _, _ := diag.Describe(err)
	if code != diag.CodeHostPortInUse {
		t.Fatalf("Connect() code = %q, want %q", code, diag.CodeHostPortInUse)
	}
}

func TestAndroidDriver_Connect_PingFailure_KeepsConservativeCapabilities(t *testing.T) {
	runner := process.NewFake()
	tr := newFakeTransport()
	tr.pingErr = errors.New("404 not found")
	d := androiddriver.New(tr, runner, androiddriver.Options{Serial: "S1"})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if d.Capabilities().CanMultiPointer || d.Capabilities().CanPressure {
		t.Fatalf("Capabilities() = %+v, want conservative defaults kept after a /ping failure", d.Capabilities())
	}
}

func TestAndroidDriver_Connect_HealthFailure_ReturnsError(t *testing.T) {
	runner := process.NewFake()
	tr := newFakeTransport()
	tr.healthErr = errors.New("connection refused")
	d := androiddriver.New(tr, runner, androiddriver.Options{Serial: "S1"})

	err := d.Connect(context.Background())
	if err == nil {
		t.Fatal("Connect() error = nil, want the health-check failure surfaced")
	}
	if d.IsConnected() {
		t.Fatal("IsConnected() = true after a failed Connect")
	}
	code, _, remedy := diag.Describe(err)
	if code != diag.CodeAgentUnreachable {
		t.Fatalf("Connect() code = %q, want %q", code, diag.CodeAgentUnreachable)
	}
	if remedy == "" {
		t.Fatal("Connect() remedy is empty, want an actionable hint")
	}
}

// TestAndroidDriver_Connect_ForeignInstrumentationActive_ReturnsDeviceBusyBeforeForward
// proves the foreign-automation probe runs first and, on a clear
// positive, stops Connect before it ever forwards the adb tunnel.
// TestAndroidDriver_Connect_HealthNotReady_ReturnsAgentNotReadyWithReason
// proves that a /health call answered with the agent's own not-ready
// status (a *transport.AgentError, matching a 503 response body) is
// reported apart from an unreachable agent: a distinct code, the
// agent's reason as the message, and no route, host, or port in
// either.
func TestAndroidDriver_Connect_HealthNotReady_ReturnsAgentNotReadyWithReason(t *testing.T) {
	runner := process.NewFake()
	tr := newFakeTransport()
	tr.healthErr = &transport.AgentError{
		Route:      "GET /health",
		StatusCode: 503,
		Message:    "The accessibility service is not running. Re-enable it on the device.",
	}
	d := androiddriver.New(tr, runner, androiddriver.Options{Serial: "S1"})

	err := d.Connect(context.Background())
	if err == nil {
		t.Fatal("Connect() error = nil, want the not-ready health outcome surfaced")
	}
	if d.IsConnected() {
		t.Fatal("IsConnected() = true after a not-ready Connect")
	}
	code, message, remedy := diag.Describe(err)
	if code != diag.CodeAgentNotReady {
		t.Fatalf("Connect() code = %q, want %q", code, diag.CodeAgentNotReady)
	}
	want := "The accessibility service is not running. Re-enable it on the device."
	if message != want {
		t.Fatalf("Connect() message = %q, want %q", message, want)
	}
	if remedy == "" {
		t.Fatal("Connect() remedy is empty, want an actionable hint")
	}
	for _, forbidden := range []string{"http", "127.0.0.1", "get ", "/health", ":8765"} {
		if strings.Contains(strings.ToLower(message), forbidden) || strings.Contains(strings.ToLower(remedy), forbidden) {
			t.Fatalf("Connect() message/remedy names the route, host, or port: message=%q remedy=%q", message, remedy)
		}
	}
}

func TestAndroidDriver_Connect_ForeignInstrumentationActive_ReturnsDeviceBusyBeforeForward(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "shell", "dumpsys", "activity"),
		process.Result{Stdout: "ACTIVITY MANAGER INSTRUMENTATION (dumpsys activity instrumentation)\n" +
			"  * InstrumentationRecord{9f3a2b0 com.example.app.test/androidx.test.runner.AndroidJUnitRunner}\n"},
		nil)
	d := androiddriver.New(newFakeTransport(), runner, androiddriver.Options{Serial: "S1"})

	err := d.Connect(context.Background())
	if err == nil {
		t.Fatal("Connect() error = nil, want a DEVICE_BUSY diagnostic when a foreign instrumentation is active")
	}
	if d.IsConnected() {
		t.Fatal("IsConnected() = true after Connect stopped on a foreign automation session")
	}
	code, _, remedy := diag.Describe(err)
	if code != diag.CodeDeviceBusy {
		t.Fatalf("Connect() code = %q, want %q", code, diag.CodeDeviceBusy)
	}
	if remedy == "" {
		t.Fatal("Connect() remedy is empty, want an actionable hint")
	}
	invocations := runner.Invocations()
	if len(invocations) != 1 {
		t.Fatalf("adb invocations = %+v, want only the foreign-automation probe, no forward", invocations)
	}
}

// TestAndroidDriver_Connect_ProbeCommandErrors_FailsOpenAndConnects
// proves a probe failure never blocks a legitimate run: Connect
// proceeds through forward, health, and ping exactly as it would with
// no probe at all.
func TestAndroidDriver_Connect_ProbeCommandErrors_FailsOpenAndConnects(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "shell", "dumpsys", "activity"),
		process.Result{}, errors.New("device offline"))
	d := androiddriver.New(newFakeTransport(), runner, androiddriver.Options{Serial: "S1"})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v, want nil: a failed probe must fail open", err)
	}
	if !d.IsConnected() {
		t.Fatal("IsConnected() = false, want true: a failed probe must not block Connect")
	}
}

func TestAndroidDriver_Ping_Succeeds(t *testing.T) {
	d := androiddriver.New(newFakeTransport(), process.NewFake(), androiddriver.Options{Serial: "S1"})
	if err := d.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

// TestAndroidDriver_Ping_HealthFailure_ReturnsAgentUnreachable guards
// the boundary contract: a failed liveness probe must be describable
// through diag.Describe as AGENT_UNREACHABLE, with a non-empty
// remedy.
func TestAndroidDriver_Ping_HealthFailure_ReturnsAgentUnreachable(t *testing.T) {
	tr := newFakeTransport()
	tr.healthErr = errors.New("connection refused")
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	err := d.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping() error = nil, want the health-check failure surfaced")
	}
	code, message, remedy := diag.Describe(err)
	if code != diag.CodeAgentUnreachable {
		t.Fatalf("Ping() code = %q, want %q", code, diag.CodeAgentUnreachable)
	}
	if remedy == "" {
		t.Fatal("Ping() remedy is empty, want an actionable hint")
	}
	if strings.Contains(message, "driver") || strings.Contains(remedy, "driver") {
		t.Fatalf("Ping() message/remedy names the internal driver abstraction: message=%q remedy=%q", message, remedy)
	}
}

// TestAndroidDriver_Ping_HealthNotReady_ReturnsAgentNotReadyWithReason
// mirrors the Connect case above for Ping, which reuses the same
// /health call and must classify it the same way.
func TestAndroidDriver_Ping_HealthNotReady_ReturnsAgentNotReadyWithReason(t *testing.T) {
	tr := newFakeTransport()
	tr.healthErr = &transport.AgentError{
		Route:      "GET /health",
		StatusCode: 503,
		Message:    "The accessibility service is connected but cannot read the screen.",
	}
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	err := d.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping() error = nil, want the not-ready health outcome surfaced")
	}
	code, message, remedy := diag.Describe(err)
	if code != diag.CodeAgentNotReady {
		t.Fatalf("Ping() code = %q, want %q", code, diag.CodeAgentNotReady)
	}
	want := "The accessibility service is connected but cannot read the screen."
	if message != want {
		t.Fatalf("Ping() message = %q, want %q", message, want)
	}
	if remedy == "" {
		t.Fatal("Ping() remedy is empty, want an actionable hint")
	}
	for _, forbidden := range []string{"http", "127.0.0.1", "get ", "/health", ":8765"} {
		if strings.Contains(strings.ToLower(message), forbidden) || strings.Contains(strings.ToLower(remedy), forbidden) {
			t.Fatalf("Ping() message/remedy names the route, host, or port: message=%q remedy=%q", message, remedy)
		}
	}
}

func TestAndroidDriver_Disconnect_RemovesForwardOnConnectedPort(t *testing.T) {
	runner := process.NewFake()
	d := androiddriver.New(newFakeTransport(), runner, androiddriver.Options{Serial: "S1", HostPort: 9001})
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	invocations := runner.Invocations()
	if len(invocations) != 4 {
		t.Fatalf("adb invocations = %+v, want the foreign-automation probe, the forward --list check, forward, then forward --remove", invocations)
	}
	wantArgs := []string{"-s", "S1", "forward", "--remove", "tcp:9001"}
	if !equalArgs(invocations[3].Args, wantArgs) {
		t.Fatalf("disconnect adb args = %v, want %v", invocations[3].Args, wantArgs)
	}
}

func TestAndroidDriver_InstallApp_IssuesExactAdbArgs(t *testing.T) {
	runner := process.NewFake()
	d := androiddriver.New(newFakeTransport(), runner, androiddriver.Options{Serial: "S1"})

	if err := d.InstallApp(context.Background(), "/tmp/app.apk"); err != nil {
		t.Fatalf("InstallApp() error = %v", err)
	}
	wantArgs := []string{"-s", "S1", "install", "-r", "/tmp/app.apk"}
	if got := runner.Invocations(); len(got) != 1 || !equalArgs(got[0].Args, wantArgs) {
		t.Fatalf("adb invocations = %+v, want %v", got, wantArgs)
	}
}

// TestAndroidDriver_InstallApp_FailureSurfacesAppInstallFailedCode
// guards the boundary contract: an install failure must be
// describable through diag.Describe as APP_INSTALL_FAILED, naming the
// source artifact in its message, with a non-empty remedy.
func TestAndroidDriver_InstallApp_FailureSurfacesAppInstallFailedCode(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "install", "-r", "/tmp/app.apk"),
		process.Result{Stderr: "Failure [INSTALL_FAILED_INVALID_APK]"}, errors.New("exit status 1"))
	d := androiddriver.New(newFakeTransport(), runner, androiddriver.Options{Serial: "S1"})

	err := d.InstallApp(context.Background(), "/tmp/app.apk")
	if err == nil {
		t.Fatal("InstallApp() error = nil, want the adb failure surfaced")
	}
	code, message, remedy := diag.Describe(err)
	if code != diag.CodeAppInstallFailed {
		t.Fatalf("InstallApp() code = %q, want %q", code, diag.CodeAppInstallFailed)
	}
	if !strings.Contains(message, "/tmp/app.apk") {
		t.Fatalf("InstallApp() message = %q, want it to name the source artifact", message)
	}
	if remedy == "" {
		t.Fatal("InstallApp() remedy is empty, want an actionable hint")
	}
}

func TestAndroidDriver_UninstallApp_IssuesExactAdbArgs(t *testing.T) {
	runner := process.NewFake()
	d := androiddriver.New(newFakeTransport(), runner, androiddriver.Options{Serial: "S1"})

	if err := d.UninstallApp(context.Background(), "dev.atomyx.app"); err != nil {
		t.Fatalf("UninstallApp() error = %v", err)
	}
	wantArgs := []string{"-s", "S1", "uninstall", "dev.atomyx.app"}
	if got := runner.Invocations(); len(got) != 1 || !equalArgs(got[0].Args, wantArgs) {
		t.Fatalf("adb invocations = %+v, want %v", got, wantArgs)
	}
}

func TestAndroidDriver_ClearAppData_IssuesExactAdbArgs(t *testing.T) {
	runner := process.NewFake()
	d := androiddriver.New(newFakeTransport(), runner, androiddriver.Options{Serial: "S1"})

	if err := d.ClearAppData(context.Background(), "dev.atomyx.app", driver.ClearAppDataOptions{Source: "/ignored.apk"}); err != nil {
		t.Fatalf("ClearAppData() error = %v", err)
	}
	wantArgs := []string{"-s", "S1", "shell", "pm", "clear", "dev.atomyx.app"}
	if got := runner.Invocations(); len(got) != 1 || !equalArgs(got[0].Args, wantArgs) {
		t.Fatalf("adb invocations = %+v, want %v", got, wantArgs)
	}
}

func TestAndroidDriver_IsAppInstalled_ExactMatchOnly(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(
		process.ExactMatch("adb", "-s", "S1", "shell", "pm", "list", "packages", "dev.atomyx.app"),
		process.Result{Stdout: "package:dev.atomyx.app.debug\npackage:dev.atomyx.app\n"},
		nil,
	)
	d := androiddriver.New(newFakeTransport(), runner, androiddriver.Options{Serial: "S1"})

	installed, err := d.IsAppInstalled(context.Background(), "dev.atomyx.app")
	if err != nil {
		t.Fatalf("IsAppInstalled() error = %v", err)
	}
	if !installed {
		t.Fatal("IsAppInstalled() = false, want true: an exact-match line is present")
	}

	installed, err = d.IsAppInstalled(context.Background(), "dev.atomyx.app.other")
	if err != nil {
		t.Fatalf("IsAppInstalled() error = %v", err)
	}
	if installed {
		t.Fatal("IsAppInstalled() = true, want false: no exact-match line for this id")
	}
}

func TestAndroidDriver_Tap_DelegatesCoordinatesToTransport(t *testing.T) {
	tr := newFakeTransport()
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	if err := d.Tap(context.Background(), wire.Point{X: 10, Y: 20}); err != nil {
		t.Fatalf("Tap() error = %v", err)
	}
	if want := (transport.TapCoordsRequest{X: 10, Y: 20}); tr.lastTap != want {
		t.Fatalf("TapCoords request = %+v, want %+v", tr.lastTap, want)
	}
}

func TestAndroidDriver_LongPress_DelegatesDurationToTransport(t *testing.T) {
	tr := newFakeTransport()
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	if err := d.LongPress(context.Background(), wire.Point{X: 5, Y: 6}, 800); err != nil {
		t.Fatalf("LongPress() error = %v", err)
	}
	if want := (transport.LongPressRequest{X: 5, Y: 6, DurationMs: 800}); tr.lastLongPres != want {
		t.Fatalf("LongPress request = %+v, want %+v", tr.lastLongPres, want)
	}
}

func TestAndroidDriver_Swipe_DelegatesEndpointsAndDurationToTransport(t *testing.T) {
	tr := newFakeTransport()
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	if err := d.Swipe(context.Background(), wire.Point{X: 1, Y: 2}, wire.Point{X: 3, Y: 4}, 300); err != nil {
		t.Fatalf("Swipe() error = %v", err)
	}
	want := transport.SwipeRequest{FromX: 1, FromY: 2, ToX: 3, ToY: 4, DurationMs: 300}
	if tr.lastSwipe != want {
		t.Fatalf("Swipe request = %+v, want %+v", tr.lastSwipe, want)
	}
}

func TestAndroidDriver_InputText_AlwaysSendsClearFirstFalse(t *testing.T) {
	tr := newFakeTransport()
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	if err := d.InputText(context.Background(), "hello"); err != nil {
		t.Fatalf("InputText() error = %v", err)
	}
	if tr.lastTypeKb.Text != "hello" {
		t.Fatalf("TypeKeyboard.Text = %q, want %q", tr.lastTypeKb.Text, "hello")
	}
	if tr.lastTypeKb.ClearFirst == nil || *tr.lastTypeKb.ClearFirst {
		t.Fatalf("TypeKeyboard.ClearFirst = %v, want an explicit false", tr.lastTypeKb.ClearFirst)
	}
}

func TestAndroidDriver_EraseText_FailureReturnsErrorWithReason(t *testing.T) {
	tr := newFakeTransport()
	tr.clearFocusedResp = transport.ClearFocusedInputResult{Ok: false, Reason: "no focused field"}
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	err := d.EraseText(context.Background(), 3)
	if err == nil {
		t.Fatal("EraseText() error = nil, want a failure surfaced")
	}
}

func TestAndroidDriver_EraseText_FailurePopulatesStructuredDiagnostic(t *testing.T) {
	tr := newFakeTransport()
	tr.clearFocusedResp = transport.ClearFocusedInputResult{
		Ok:              false,
		Reason:          "all strategies failed",
		StrategiesTried: []string{"select_all_delete", "long_press_clear", "backspace_loop", "set_text_empty"},
		LastValue:       "partial",
		FocusedNodeDesc: "EditText(resourceId=com.app:id/search)",
		ScreenWidth:     1080,
		ScreenHeight:    2400,
	}
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	err := d.EraseText(context.Background(), 3)
	var clearErr *androiddriver.ClearTextError
	if !errors.As(err, &clearErr) {
		t.Fatalf("EraseText() error = %v (%T), want a *androiddriver.ClearTextError", err, err)
	}
	want := androiddriver.ClearTextDiagnostic{
		StrategiesTried: []string{"select_all_delete", "long_press_clear", "backspace_loop", "set_text_empty"},
		LastValue:       "partial",
		FocusedNodeDesc: "EditText(resourceId=com.app:id/search)",
		ScreenWidth:     1080,
		ScreenHeight:    2400,
	}
	if !reflect.DeepEqual(clearErr.Diagnostic, want) {
		t.Fatalf("ClearTextError.Diagnostic = %+v, want %+v", clearErr.Diagnostic, want)
	}
}

func TestAndroidDriver_EraseText_FailureFillsDefaultsWhenAgentOmitsDiagnostic(t *testing.T) {
	tr := newFakeTransport()
	tr.clearFocusedResp = transport.ClearFocusedInputResult{Ok: false}
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	err := d.EraseText(context.Background(), 3)
	var clearErr *androiddriver.ClearTextError
	if !errors.As(err, &clearErr) {
		t.Fatalf("EraseText() error = %v (%T), want a *androiddriver.ClearTextError", err, err)
	}
	want := androiddriver.ClearTextDiagnostic{
		StrategiesTried: []string{},
		LastValue:       "",
		FocusedNodeDesc: "unknown",
		ScreenWidth:     0,
		ScreenHeight:    0,
	}
	if !reflect.DeepEqual(clearErr.Diagnostic, want) {
		t.Fatalf("ClearTextError.Diagnostic = %+v, want %+v", clearErr.Diagnostic, want)
	}
}

func TestAndroidDriver_StopApp_AgentRefusesStop_ReturnsForceStopError(t *testing.T) {
	tr := newFakeTransport()
	tr.forceStopResp = transport.AckResult{Ok: false, Reason: "not permitted to force-stop com.example.app"}
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	err := d.StopApp(context.Background(), "com.example.app")
	var stopErr *androiddriver.ForceStopError
	if !errors.As(err, &stopErr) {
		t.Fatalf("StopApp() error = %v (%T), want a *androiddriver.ForceStopError", err, err)
	}
	if stopErr.PackageName != "com.example.app" {
		t.Fatalf("ForceStopError.PackageName = %q, want %q", stopErr.PackageName, "com.example.app")
	}
	if stopErr.Reason != "not permitted to force-stop com.example.app" {
		t.Fatalf("ForceStopError.Reason = %q, want the agent's refusal reason", stopErr.Reason)
	}
}

func TestAndroidDriver_StopApp_AgentStopsSuccessfully_ReturnsNil(t *testing.T) {
	tr := newFakeTransport()
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	if err := d.StopApp(context.Background(), "com.example.app"); err != nil {
		t.Fatalf("StopApp() error = %v, want nil", err)
	}
}

func TestAndroidDriver_LaunchApp_ResetRefusedByAgent_ReturnsForceStopErrorAndSkipsRelaunch(t *testing.T) {
	tr := newFakeTransport()
	tr.forceStopResp = transport.AckResult{Ok: false, Reason: "not permitted to force-stop com.example.app"}
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})
	fresh := false

	err := d.LaunchApp(context.Background(), "com.example.app", driver.LaunchArgs{NoReset: &fresh})

	var stopErr *androiddriver.ForceStopError
	if !errors.As(err, &stopErr) {
		t.Fatalf("LaunchApp() error = %v (%T), want a *androiddriver.ForceStopError", err, err)
	}
	if len(tr.launchAppCalls) != 0 {
		t.Fatalf("LaunchApp() called the agent's launch route %v times after a refused reset, want 0 (a refused reset must not relaunch on top of the still-running process)", tr.launchAppCalls)
	}
}

func TestAndroidDriver_LaunchApp_ResetSucceeds_RelaunchesApp(t *testing.T) {
	tr := newFakeTransport()
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})
	fresh := false

	if err := d.LaunchApp(context.Background(), "com.example.app", driver.LaunchArgs{NoReset: &fresh}); err != nil {
		t.Fatalf("LaunchApp() error = %v, want nil", err)
	}
	if want := []string{"com.example.app"}; !reflect.DeepEqual(tr.launchAppCalls, want) {
		t.Fatalf("launchAppCalls = %v, want %v", tr.launchAppCalls, want)
	}
}

func TestAndroidDriver_DispatchGesture_RejectsMultiPointerWithoutCapability(t *testing.T) {
	d := androiddriver.New(newFakeTransport(), process.NewFake(), androiddriver.Options{Serial: "S1"})
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{{ID: "a"}, {ID: "b"}}}

	if _, err := d.DispatchGesture(context.Background(), gesture); err == nil {
		t.Fatal("DispatchGesture() error = nil, want a multi-pointer rejection")
	}
}

func TestAndroidDriver_DispatchGesture_RejectsPressureWithoutCapability(t *testing.T) {
	d := androiddriver.New(newFakeTransport(), process.NewFake(), androiddriver.Options{Serial: "S1"})
	pressure := 0.5
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{
		{ID: "a", Waypoints: []driver.GestureWaypoint{{Phase: driver.GesturePhaseDown, Pressure: &pressure}}},
	}}

	if _, err := d.DispatchGesture(context.Background(), gesture); err == nil {
		t.Fatal("DispatchGesture() error = nil, want a pressure rejection")
	}
}

func TestAndroidDriver_DispatchGesture_RejectsEmptyPointers(t *testing.T) {
	d := androiddriver.New(newFakeTransport(), process.NewFake(), androiddriver.Options{Serial: "S1"})

	if _, err := d.DispatchGesture(context.Background(), driver.Gesture{}); err == nil {
		t.Fatal("DispatchGesture() error = nil, want an empty-pointers rejection")
	}
}

// TestAndroidDriver_DispatchGesture_ForwardsCaptureWaypointAndDecodesImage
// asserts a mid-gesture capture waypoint is forwarded to the agent
// unmodified and the agent's base64 image decodes into
// GestureResult.CapturedImages.
func TestAndroidDriver_DispatchGesture_ForwardsCaptureWaypointAndDecodesImage(t *testing.T) {
	tr := newFakeTransport()
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})
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
	if len(result.CapturedImages) != 1 {
		t.Fatalf("CapturedImages = %+v, want 1 entry", result.CapturedImages)
	}
	if string(result.CapturedImages[0].Data) != string(fakeCapturedImageJPEG) {
		t.Fatalf("CapturedImages[0].Data = %q, want %q", result.CapturedImages[0].Data, fakeCapturedImageJPEG)
	}
	if result.CapturedImages[0].MimeType != "image/jpeg" {
		t.Fatalf("CapturedImages[0].MimeType = %q, want image/jpeg", result.CapturedImages[0].MimeType)
	}
	if len(tr.lastDispatch.Pointers) != 1 || len(tr.lastDispatch.Pointers[0].Waypoints) != 3 {
		t.Fatalf("DispatchGesture request = %+v, want the capture waypoint forwarded unmodified", tr.lastDispatch)
	}
	if tr.lastDispatch.Pointers[0].Waypoints[1].Phase != string(driver.GesturePhaseCapture) {
		t.Fatalf("forwarded waypoint[1].Phase = %q, want %q", tr.lastDispatch.Pointers[0].Waypoints[1].Phase, driver.GesturePhaseCapture)
	}
}

func TestAndroidDriver_DispatchGesture_ForwardsWaypointsToTransport(t *testing.T) {
	tr := newFakeTransport()
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{
		{ID: "finger1", Waypoints: []driver.GestureWaypoint{
			{Phase: driver.GesturePhaseDown, Point: wire.Point{X: 1, Y: 2}, AtOffsetSeconds: 0},
			{Phase: driver.GesturePhaseUp, Point: wire.Point{X: 1, Y: 2}, AtOffsetSeconds: 0.1},
		}},
	}}

	if _, err := d.DispatchGesture(context.Background(), gesture); err != nil {
		t.Fatalf("DispatchGesture() error = %v", err)
	}
	if len(tr.lastDispatch.Pointers) != 1 || tr.lastDispatch.Pointers[0].ID != "finger1" {
		t.Fatalf("DispatchGesture request = %+v, want one pointer with ID finger1", tr.lastDispatch)
	}
	if len(tr.lastDispatch.Pointers[0].Waypoints) != 2 {
		t.Fatalf("DispatchGesture waypoints = %+v, want 2", tr.lastDispatch.Pointers[0].Waypoints)
	}
}

func TestAndroidDriver_ScreenSize_UsesLargestAreaChildBounds(t *testing.T) {
	tr := newFakeTransport()
	tr.treeResp = transport.AndroidTreeNode{
		ElementID: "root",
		Children: []transport.AndroidTreeNode{
			{ElementID: "ime", ClassName: "android.inputmethodservice.KeyboardView", Bounds: &transport.AndroidBounds{Left: 0, Top: 1900, Right: 1080, Bottom: 2400}},
			{ElementID: "window", ClassName: "android.widget.FrameLayout", Bounds: &transport.AndroidBounds{Left: 0, Top: 0, Right: 1080, Bottom: 2400}},
		},
	}
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	size, err := d.ScreenSize(context.Background())
	if err != nil {
		t.Fatalf("ScreenSize() error = %v", err)
	}
	if want := (wire.Size{Width: 1080, Height: 2400}); size != want {
		t.Fatalf("ScreenSize() = %+v, want %+v", size, want)
	}
}

func TestAndroidDriver_ScreenSize_NoChildBounds_ReturnsError(t *testing.T) {
	tr := newFakeTransport()
	tr.treeResp = transport.AndroidTreeNode{ElementID: "root"}
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	if _, err := d.ScreenSize(context.Background()); err == nil {
		t.Fatal("ScreenSize() error = nil, want an error when no child has bounds")
	}
}

// A tree whose only bounds-bearing child reports "0,0,0,0" must be
// treated the same as a tree with no bounds-bearing child at all: a
// zero-area rectangle is not a screen, so it must not silently win a
// tie-break against no other candidate and be reported as the
// device's screen size.
func TestAndroidDriver_ScreenSize_OnlyZeroAreaChildBounds_ReturnsError(t *testing.T) {
	tr := newFakeTransport()
	tr.treeResp = transport.AndroidTreeNode{
		ElementID: "root",
		Children: []transport.AndroidTreeNode{
			{ElementID: "overlay", ClassName: "android.view.View", Bounds: &transport.AndroidBounds{Left: 0, Top: 0, Right: 0, Bottom: 0}},
		},
	}
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	if _, err := d.ScreenSize(context.Background()); err == nil {
		t.Fatal("ScreenSize() error = nil, want an error when the only bounds-bearing child has zero area")
	}
}

func TestAndroidDriver_DeviceInfo_DerivesEmulatorKindFromSerialPrefix(t *testing.T) {
	d := androiddriver.New(newFakeTransport(), process.NewFake(), androiddriver.Options{Serial: "emulator-5554"})
	info, err := d.DeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("DeviceInfo() error = %v", err)
	}
	if info.Kind != driver.DeviceKindEmulator || info.UDID != "emulator-5554" {
		t.Fatalf("DeviceInfo() = %+v, want kind emulator and udid emulator-5554", info)
	}

	d2 := androiddriver.New(newFakeTransport(), process.NewFake(), androiddriver.Options{Serial: "R3CT909XYZ"})
	info2, err := d2.DeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("DeviceInfo() error = %v", err)
	}
	if info2.Kind != driver.DeviceKindDevice {
		t.Fatalf("DeviceInfo() Kind = %v, want device", info2.Kind)
	}
}

func TestAndroidDriver_ListApps_FallsBackToPackageNameWhenLabelEmpty(t *testing.T) {
	tr := newFakeTransport()
	tr.appsResp = []transport.InstalledApp{
		{PackageName: "com.android.chrome", Label: "Chrome"},
		{PackageName: "com.example.nolabel", Label: ""},
	}
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	apps, err := d.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}
	if len(apps) != 2 || apps[1].DisplayName != "com.example.nolabel" {
		t.Fatalf("ListApps() = %+v, want fallback display name for the unlabeled app", apps)
	}
}

func TestAndroidDriver_Screenshot_DecodesBase64Payload(t *testing.T) {
	tr := newFakeTransport()
	tr.screenshotResp = transport.ScreenshotResponse{Base64: "aGVsbG8=", Format: "jpeg"}
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	got, err := d.Screenshot(context.Background())
	if err != nil {
		t.Fatalf("Screenshot() error = %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("Screenshot() = %q, want %q", got, "hello")
	}
}

func equalArgs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func currentAgentPing() transport.PingResponse {
	return transport.PingResponse{
		Ok:              true,
		ProtocolVersion: version.AgentProtocolVersion,
		AgentVersion:    "0.1.0",
		Capabilities:    transport.PingCapabilities{SecureFields: true},
	}
}

func staleAgentPing() transport.PingResponse {
	return transport.PingResponse{
		Ok:              true,
		ProtocolVersion: version.AgentProtocolVersion,
		Capabilities:    transport.PingCapabilities{CanMultiPointer: true},
	}
}

func agentPingWithProtocol(protocolVersion int) transport.PingResponse {
	pong := currentAgentPing()
	pong.ProtocolVersion = protocolVersion
	return pong
}

func TestAndroidDriver_Connect_MatchingVersion_ExposesBothValuesThroughAgentInfo(t *testing.T) {
	tr := newFakeTransport()
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{
		Serial:         "S1",
		ReinstallAgent: func(context.Context) error { t.Fatal("reinstalled a matching agent"); return nil },
	})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	want := driver.AgentInfo{ProtocolVersion: version.AgentProtocolVersion, AgentVersion: "0.1.0"}
	if got := d.AgentInfo(); got != want {
		t.Fatalf("AgentInfo() = %+v, want %+v", got, want)
	}
}

func TestAndroidDriver_Connect_AgentErrorOnPing_CountsAsVersionZeroAndReinstallsOnce(t *testing.T) {
	tr := newFakeTransport()
	tr.pingErr = &transport.AgentError{Route: "GET /ping", StatusCode: 404, Message: "Unsupported command."}
	reinstalls := 0
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{
		Serial: "S1",
		ReinstallAgent: func(context.Context) error {
			reinstalls++
			tr.pingErr = nil
			tr.pingResp = currentAgentPing()
			return nil
		},
	})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if reinstalls != 1 || !d.IsConnected() {
		t.Fatalf("reinstalls = %d, connected = %v, want one reinstall and a connected driver", reinstalls, d.IsConnected())
	}
	if got := d.AgentInfo().ProtocolVersion; got != version.AgentProtocolVersion {
		t.Fatalf("AgentInfo().ProtocolVersion = %d, want %d from the replacement agent", got, version.AgentProtocolVersion)
	}
}

func TestAndroidDriver_Connect_PersistentVersionMismatch_FailsAfterExactlyOneReinstall(t *testing.T) {
	tr := newFakeTransport()
	tr.pingResp = agentPingWithProtocol(version.AgentProtocolVersion + 1)
	reinstalls := 0
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{
		Serial:         "S1",
		ReinstallAgent: func(context.Context) error { reinstalls++; return nil },
	})

	err := d.Connect(context.Background())

	code, message, remedy := diag.Describe(err)
	if code != diag.CodeAgentVersionMismatch || d.IsConnected() {
		t.Fatalf("Connect() code = %q, connected = %v, want %q and a failed connect", code, d.IsConnected(), diag.CodeAgentVersionMismatch)
	}
	if reinstalls != 1 {
		t.Fatalf("reinstalls = %d, want exactly one", reinstalls)
	}
	if !strings.Contains(message, "protocol version") || !strings.Contains(remedy, "assembleDebug") {
		t.Fatalf("message = %q, remedy = %q, want the versions named with a build remedy", message, remedy)
	}
}

func TestAndroidDriver_Connect_VersionMismatchWithoutAReinstallHook_ReportsVersionMismatch(t *testing.T) {
	tr := newFakeTransport()
	tr.pingResp = agentPingWithProtocol(0)
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	err := d.Connect(context.Background())

	if code, _, _ := diag.Describe(err); code != diag.CodeAgentVersionMismatch {
		t.Fatalf("Connect() code = %q, want %q", code, diag.CodeAgentVersionMismatch)
	}
}

func TestAndroidDriver_Connect_UnreachablePing_KeepsConservativeDefaultsWithoutReinstall(t *testing.T) {
	tr := newFakeTransport()
	tr.pingErr = errors.New("connection reset")
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{
		Serial:         "S1",
		ReinstallAgent: func(context.Context) error { t.Fatal("reinstalled over a transport error"); return nil },
	})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if got := d.AgentInfo(); got != (driver.AgentInfo{}) {
		t.Fatalf("AgentInfo() = %+v, want the zero value when ping was unreachable", got)
	}
}

func TestAndroidDriver_Connect_MissingSecureFieldsWithMatchingVersion_StillReinstalls(t *testing.T) {
	tr := newFakeTransport()
	tr.pingResp = staleAgentPing()
	reinstalls := 0
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{
		Serial: "S1",
		ReinstallAgent: func(context.Context) error {
			reinstalls++
			tr.pingResp = currentAgentPing()
			return nil
		},
	})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if reinstalls != 1 {
		t.Fatalf("reinstalls = %d, want one for an agent that does not mark secret fields", reinstalls)
	}
}

func TestAndroidDriver_Connect_AgentWithoutSecureFields_ReinstallsOnceThenConnects(t *testing.T) {
	tr := newFakeTransport()
	tr.pingResp = staleAgentPing()
	reinstalls := 0
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{
		Serial: "S1",
		ReinstallAgent: func(context.Context) error {
			reinstalls++
			tr.pingResp = currentAgentPing()
			tr.pingResp.Capabilities.CanMultiPointer = true
			return nil
		},
	})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if reinstalls != 1 || !d.IsConnected() {
		t.Fatalf("reinstalls = %d, connected = %v, want one reinstall and a connected driver", reinstalls, d.IsConnected())
	}
	if !d.Capabilities().CanMultiPointer {
		t.Fatal("capabilities from the replacement agent were not applied")
	}
}

func TestAndroidDriver_Connect_AgentWithoutSecureFields_FailsWhenItCannotBeReplaced(t *testing.T) {
	noAPK := &androiddriver.AgentLauncherError{Code: androiddriver.ErrCodeNoAPK, Message: "APK not found"}
	cases := map[string]func(*fakeTransport) androiddriver.Options{
		"no reinstall hook": func(*fakeTransport) androiddriver.Options { return androiddriver.Options{Serial: "S1"} },
		"no apk": func(*fakeTransport) androiddriver.Options {
			return androiddriver.Options{Serial: "S1", ReinstallAgent: func(context.Context) error { return noAPK }}
		},
		"agent still stale after the reinstall": func(*fakeTransport) androiddriver.Options {
			return androiddriver.Options{Serial: "S1", ReinstallAgent: func(context.Context) error { return nil }}
		},
	}
	for name, options := range cases {
		t.Run(name, func(t *testing.T) {
			tr := newFakeTransport()
			tr.pingResp = staleAgentPing()
			d := androiddriver.New(tr, process.NewFake(), options(tr))

			err := d.Connect(context.Background())

			code, message, remedy := diag.Describe(err)
			if code != diag.CodeAgentNotReady || d.IsConnected() {
				t.Fatalf("Connect() code = %q, connected = %v, want %q and a failed connect", code, d.IsConnected(), diag.CodeAgentNotReady)
			}
			if !strings.Contains(message, "out of date") || !strings.Contains(remedy, "assembleDebug") {
				t.Fatalf("message = %q, remedy = %q, want the stale agent named with a rebuild remedy", message, remedy)
			}
		})
	}
}

func TestAndroidDriver_Connect_PingRouteMissing_CountsAsAVersionZeroAgent(t *testing.T) {
	tr := newFakeTransport()
	tr.pingErr = &transport.AgentError{Route: "GET /ping", StatusCode: 404, Message: "Unsupported command."}
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})

	err := d.Connect(context.Background())

	if code, _, _ := diag.Describe(err); code != diag.CodeAgentVersionMismatch {
		t.Fatalf("Connect() code = %q, want %q for an agent that predates /ping", code, diag.CodeAgentVersionMismatch)
	}
}

func TestAndroidDriver_Connect_AgentWithSecureFields_NeverReinstalls(t *testing.T) {
	tr := newFakeTransport()
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{
		Serial:         "S1",
		ReinstallAgent: func(context.Context) error { t.Fatal("reinstalled a current agent"); return nil },
	})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
}

func TestAndroidDriver_Connect_PingUnreachableAfterReinstall_FailsAsStaleAgent(t *testing.T) {
	tr := newFakeTransport()
	tr.pingResp = agentPingWithProtocol(version.AgentProtocolVersion + 1)
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{
		Serial: "S1",
		ReinstallAgent: func(context.Context) error {
			tr.pingErr = errors.New("connection reset")
			return nil
		},
	})

	err := d.Connect(context.Background())

	if err == nil || d.IsConnected() {
		t.Fatalf("Connect() error = %v, connected = %v, want a failed connect: the replacement could not be verified", err, d.IsConnected())
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeAgentNotReady {
		t.Fatalf("Connect() code = %q, want %q", code, diag.CodeAgentNotReady)
	}
	if got := d.AgentInfo(); got != (driver.AgentInfo{}) {
		t.Fatalf("AgentInfo() = %+v after a failed Connect, want the zero value", got)
	}
}

func TestAndroidDriver_Connect_SecondConnectFailing_ClearsTheEarlierAgentInfo(t *testing.T) {
	tr := newFakeTransport()
	d := androiddriver.New(tr, process.NewFake(), androiddriver.Options{Serial: "S1"})
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("first Connect() error = %v", err)
	}
	if d.AgentInfo() == (driver.AgentInfo{}) {
		t.Fatal("AgentInfo() is zero after a successful Connect, want the reported versions")
	}
	_ = d.Disconnect()
	tr.healthErr = errors.New("agent gone")

	if err := d.Connect(context.Background()); err == nil {
		t.Fatal("second Connect() succeeded, want a failure")
	}
	if got := d.AgentInfo(); got != (driver.AgentInfo{}) {
		t.Fatalf("AgentInfo() = %+v after a failed second Connect, want the zero value", got)
	}
}
