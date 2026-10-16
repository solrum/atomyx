package iosdriver_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
	"dev.atomyx/internal/version"
	"dev.atomyx/internal/wire"
)

// responseTimeout bounds the fake TCPClient used throughout this
// file, standing in for tcpResponseTimeout so a hung runner's
// fallback bound fires in milliseconds instead of the production
// duration.
const responseTimeout = 50 * time.Millisecond

// faultInjectionBudget is the outer bound every assertion in this
// file holds a driver call to: generous enough that scheduling noise
// never trips it, but far short of a real hang, so a method that
// regresses to blocking indefinitely fails the test instead of
// hanging it.
const faultInjectionBudget = 2 * time.Second

// faultInjectionIosAgent is a real local TCP listener speaking the
// iOS runner's line-delimited JSON protocol. It answers every command
// type in its healthy set immediately with a minimal well-formed
// response, and accepts every other command without ever answering
// it. It stands in for a Swift XCUITest runner that accepted a
// command but then hung.
//
// A connection's read loop blocks on a dedicated release channel
// rather than on the client disconnecting, so a hung command always
// releases deterministically at test cleanup instead of depending on
// whether the client's own give-up tears down the socket promptly
// enough for this side to observe it.
type faultInjectionIosAgent struct {
	listener net.Listener
	healthy  map[string]bool
	release  chan struct{}

	// rejectAll, when set, makes every command answer immediately
	// with ok:false instead of consulting healthy, standing in for a
	// runner that reaches every command but reports the action itself
	// failed.
	rejectAll bool

	// rejectMessage is the error text a rejectAll agent reports for
	// every command. newFaultInjectionIosAgentWith seeds it with a
	// fixed, safe default; newRejectingIosAgentWithMessage overrides it
	// to stand in for a runner whose own rejection text takes an
	// arbitrary shape.
	rejectMessage string
}

func newFaultInjectionIosAgent(t *testing.T, healthyTypes ...string) *faultInjectionIosAgent {
	t.Helper()
	healthy := make(map[string]bool, len(healthyTypes))
	for _, ty := range healthyTypes {
		healthy[ty] = true
	}
	return newFaultInjectionIosAgentWith(t, healthy, false, "")
}

// newRejectingIosAgent starts a fault-injection agent that answers
// every command, including ping, with ok:false and a fixed, safe
// error message, standing in for a runner that reached every command
// but rejected it.
func newRejectingIosAgent(t *testing.T) *faultInjectionIosAgent {
	t.Helper()
	return newFaultInjectionIosAgentWith(t, map[string]bool{}, true, "the requested action failed")
}

// newRejectingIosAgentWithMessage starts a fault-injection agent that
// answers every command, including ping, with ok:false and message as
// the reported error text, standing in for a runner whose own
// rejection text names a route or a private symbol.
func newRejectingIosAgentWithMessage(t *testing.T, message string) *faultInjectionIosAgent {
	t.Helper()
	return newFaultInjectionIosAgentWith(t, map[string]bool{}, true, message)
}

func newFaultInjectionIosAgentWith(t *testing.T, healthy map[string]bool, rejectAll bool, rejectMessage string) *faultInjectionIosAgent {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	agent := &faultInjectionIosAgent{
		listener:      listener,
		healthy:       healthy,
		release:       make(chan struct{}),
		rejectAll:     rejectAll,
		rejectMessage: rejectMessage,
	}
	t.Cleanup(func() {
		close(agent.release)
		_ = listener.Close()
	})
	go agent.serve()
	return agent
}

func (a *faultInjectionIosAgent) hostPort(t *testing.T) (string, int) {
	t.Helper()
	tcpAddr, ok := a.listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address = %v, want *net.TCPAddr", a.listener.Addr())
	}
	return "127.0.0.1", tcpAddr.Port
}

func (a *faultInjectionIosAgent) serve() {
	for {
		conn, err := a.listener.Accept()
		if err != nil {
			return
		}
		go a.handleConn(conn)
	}
}

func (a *faultInjectionIosAgent) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			a.handleLine(conn, line)
		}
		if err != nil {
			return
		}
	}
}

func (a *faultInjectionIosAgent) handleLine(conn net.Conn, line string) {
	var req struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(line), &req) != nil {
		return
	}
	if a.rejectAll {
		a.writeReject(conn, req.ID)
		return
	}
	if !a.healthy[req.Type] {
		<-a.release
		return
	}
	frame, err := json.Marshal(struct {
		ID   int64           `json:"id"`
		Ok   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}{ID: req.ID, Ok: true, Data: json.RawMessage(faultInjectionIosOkData(req.Type))})
	if err != nil {
		return
	}
	frame = append(frame, '\n')
	_, _ = conn.Write(frame)
}

// writeReject answers id with ok:false and a.rejectMessage as the
// reported error text, standing in for the runner's own rejection
// text.
func (a *faultInjectionIosAgent) writeReject(conn net.Conn, id int64) {
	frame, err := json.Marshal(struct {
		ID    int64  `json:"id"`
		Ok    bool   `json:"ok"`
		Error string `json:"error"`
	}{ID: id, Ok: false, Error: a.rejectMessage})
	if err != nil {
		return
	}
	frame = append(frame, '\n')
	_, _ = conn.Write(frame)
}

// faultInjectionIosOkData returns the minimal well-formed data payload
// cmdType's real command requires, so the ping handshake Connect
// depends on does not fail decoding before the fault-injection
// assertion ever runs.
func faultInjectionIosOkData(cmdType string) string {
	if cmdType == "ping" {
		return `{"protocolVersion":` + strconv.Itoa(version.AgentProtocolVersion) + `,"capabilities":{"canMultiPointer":false,"canPressure":false}}`
	}
	return `{}`
}

// connectedFaultInjectionDriver builds an IosDriver against agent's
// listener, with the TCP client's response-timeout fallback shortened
// to responseTimeout, and connects it. process.NewFake answers every
// process invocation with an empty success, so no path in Connect
// depends on a real xcrun or iproxy binary; targeting KindSimulator
// keeps Connect from launching iproxy at all.
func connectedFaultInjectionDriver(t *testing.T, agent *faultInjectionIosAgent) *iosdriver.IosDriver {
	t.Helper()
	host, port := agent.hostPort(t)
	conn := transport.NewTCPClient(host, port, transport.WithTCPResponseTimeout(responseTimeout))
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "fault-injection", Kind: iosdriver.KindSimulator})
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() against a healthy runner: %v", err)
	}
	return d
}

// minimalGesture is the smallest gesture DispatchGesture accepts:
// unlike AndroidDriver, IosDriver rejects nothing locally before the
// wire call, so a single pointer with one waypoint is enough to
// exercise it.
var minimalGesture = driver.Gesture{
	Pointers: []driver.GesturePointer{{
		ID: "0",
		Waypoints: []driver.GestureWaypoint{
			{Phase: driver.GesturePhaseDown, Point: wire.Point{X: 1, Y: 1}},
			{Phase: driver.GesturePhaseUp, Point: wire.Point{X: 1, Y: 1}, AtOffsetSeconds: 0.05},
		},
	}},
}

// wireCallMethodCases enumerates every IosDriver method that issues a
// TCP wire call, driven with minimal valid arguments over a plain
// context.Context so a caller can force the underlying transport call
// to fail without needing a real XCUITest runner. TestIosDriver_WireCallTimeout
// and TestIosDriver_BoundaryTranslation_EveryWireCallMethod both drive
// this same table against a differently-faulty runner, so a method
// added to the port only has to be added here once.
//
// Connect and Ping are deliberately absent from this table: Ping
// already applies its own typed classification (diag.CodeAgentUnreachable)
// to any handshake failure, including a hang, and Connect's ping
// handshake now routes through the same fail boundary this table
// checks; both are checked directly by
// TestIosDriver_Connect_HungPing_SurfacesTypedTimeout and
// TestIosDriver_Ping_Hung_SurfacesTypedUnreachable. WaitForIdle never
// issues a wire call. CurrentForeground always returns a local
// unsupported diagnostic without touching the conn. ListApps,
// InstallApp, UninstallApp, ClearAppData, and IsAppInstalled issue a
// host subprocess call through the injected process.Runner (xcrun)
// rather than the bounded TCP transport; a hung subprocess is a
// distinct, unaddressed gap outside the classification this table
// checks. DeviceInfo returns a static local descriptor.
var wireCallMethodCases = []struct {
	name string
	call func(ctx context.Context, d driver.Driver) error
}{
	{"Hierarchy", func(ctx context.Context, d driver.Driver) error { _, err := d.Hierarchy(ctx); return err }},
	{"Tap", func(ctx context.Context, d driver.Driver) error { return d.Tap(ctx, wire.Point{X: 1, Y: 1}) }},
	{"LongPress", func(ctx context.Context, d driver.Driver) error {
		return d.LongPress(ctx, wire.Point{X: 1, Y: 1}, 100)
	}},
	{"Swipe", func(ctx context.Context, d driver.Driver) error {
		return d.Swipe(ctx, wire.Point{X: 0, Y: 0}, wire.Point{X: 1, Y: 1}, 100)
	}},
	{"DispatchGesture", func(ctx context.Context, d driver.Driver) error {
		_, err := d.DispatchGesture(ctx, minimalGesture)
		return err
	}},
	{"InputText", func(ctx context.Context, d driver.Driver) error { return d.InputText(ctx, "x") }},
	{"EraseText", func(ctx context.Context, d driver.Driver) error { return d.EraseText(ctx, 1) }},
	{"PressKey", func(ctx context.Context, d driver.Driver) error {
		_, err := d.PressKey(ctx, driver.KeyBack)
		return err
	}},
	{"HideKeyboard", func(ctx context.Context, d driver.Driver) error { _, err := d.HideKeyboard(ctx); return err }},
	{"LaunchApp", func(ctx context.Context, d driver.Driver) error {
		return d.LaunchApp(ctx, "dev.atomyx.faultinjection", driver.LaunchArgs{})
	}},
	{"OpenLink", func(ctx context.Context, d driver.Driver) error {
		return d.OpenLink(ctx, "faultinjection://home", driver.OpenLinkOptions{})
	}},
	{"StopApp", func(ctx context.Context, d driver.Driver) error {
		return d.StopApp(ctx, "dev.atomyx.faultinjection")
	}},
	{"KillApp", func(ctx context.Context, d driver.Driver) error {
		return d.KillApp(ctx, "dev.atomyx.faultinjection")
	}},
	{"Screenshot", func(ctx context.Context, d driver.Driver) error { _, err := d.Screenshot(ctx); return err }},
	{"ScreenSize", func(ctx context.Context, d driver.Driver) error { _, err := d.ScreenSize(ctx); return err }},
}

// TestIosDriver_WireCallTimeout drives wireCallMethodCases against a
// runner that accepts the command but never answers it: each method
// must return within faultInjectionBudget with a diag.CodeToolTimeout
// diagnostic rather than hang.
func TestIosDriver_WireCallTimeout(t *testing.T) {
	for _, tc := range wireCallMethodCases {
		t.Run(tc.name, func(t *testing.T) {
			agent := newFaultInjectionIosAgent(t, "ping")
			d := connectedFaultInjectionDriver(t, agent)

			start := time.Now()
			err := tc.call(context.Background(), d)
			elapsed := time.Since(start)

			if err == nil {
				t.Fatalf("%s against a hung runner returned no error", tc.name)
			}
			if elapsed > faultInjectionBudget {
				t.Fatalf("%s took %v to fail against a %v response timeout, want it bounded by %v rather than hanging",
					tc.name, elapsed, responseTimeout, faultInjectionBudget)
			}
			if code, _, _ := diag.Describe(err); code != diag.CodeToolTimeout {
				t.Fatalf("%s against a hung runner: diag.Describe code = %q, want %q (error: %v)",
					tc.name, code, diag.CodeToolTimeout, err)
			}
		})
	}
}

// TestIosDriver_Connect_HungPing_SurfacesTypedTimeout proves that
// Connect classifies a hung ping handshake as diag.CodeToolTimeout,
// the same code any other hung wire call surfaces, rather than
// hanging or surfacing an unclassified error. Unlike AndroidDriver,
// IosDriver's Connect does not re-wrap the handshake failure into a
// different code, so the underlying classification passes through
// unchanged.
func TestIosDriver_Connect_HungPing_SurfacesTypedTimeout(t *testing.T) {
	agent := newFaultInjectionIosAgent(t) // no command type is healthy, including "ping".
	host, port := agent.hostPort(t)
	conn := transport.NewTCPClient(host, port, transport.WithTCPResponseTimeout(responseTimeout))
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "fault-injection", Kind: iosdriver.KindSimulator})

	start := time.Now()
	err := d.Connect(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Connect() against a hung runner returned no error")
	}
	if elapsed > faultInjectionBudget {
		t.Fatalf("Connect() took %v against a hung runner, want it bounded by %v rather than hanging", elapsed, faultInjectionBudget)
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeToolTimeout {
		t.Fatalf("Connect() against a hung runner: diag.Describe code = %q, want %q (error: %v)", code, diag.CodeToolTimeout, err)
	}
}

// pingProbeBudget bounds TestIosDriver_Ping_Hung_SurfacesTypedUnreachable.
// Ping applies its own internal sub-context to the probe (currently a
// few seconds) regardless of responseTimeout, so this budget is kept
// separate from and looser than faultInjectionBudget rather than
// duplicating that unexported constant here.
const pingProbeBudget = 10 * time.Second

// TestIosDriver_Ping_Hung_SurfacesTypedUnreachable proves that Ping
// classifies a hung ping handshake as diag.CodeAgentUnreachable within
// its own probe sub-budget, rather than hanging or surfacing an
// unclassified error.
func TestIosDriver_Ping_Hung_SurfacesTypedUnreachable(t *testing.T) {
	agent := newFaultInjectionIosAgent(t) // no command type is healthy, including "ping".
	host, port := agent.hostPort(t)
	conn := transport.NewTCPClient(host, port, transport.WithTCPResponseTimeout(responseTimeout))
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "fault-injection", Kind: iosdriver.KindSimulator})
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("conn.Connect() against a live listener: %v", err)
	}

	start := time.Now()
	err := d.Ping(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Ping() against a hung runner returned no error")
	}
	if elapsed > pingProbeBudget {
		t.Fatalf("Ping() took %v against a hung runner, want it bounded by %v rather than hanging", elapsed, pingProbeBudget)
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeAgentUnreachable {
		t.Fatalf("Ping() against a hung runner: diag.Describe code = %q, want %q (error: %v)", code, diag.CodeAgentUnreachable, err)
	}
}

// forbiddenTransportSubstrings are the shapes a leaked wire-call
// detail (a TCP address, port, or dial diagnostic) takes. A
// tool-facing IosDriver error must never contain one of these,
// regardless of which method produced it or why the underlying
// request failed.
var forbiddenTransportSubstrings = []string{
	"tcp", "127.0.0.1", "localhost", ":22087", "dial", "iproxy",
	"http", "GET ", "POST ", ":8765", "/tree", "/actions",
}

// boundaryTranslationSkipped names every driver.Driver method absent
// from wireCallMethodCases, with the reason
// TestIosDriver_BoundaryTranslation_EveryWireCallMethod does not
// exercise it. TestIosDriver_BoundaryTranslation_CoversEveryDriverMethod
// fails when a method sits in neither this map nor
// wireCallMethodCases, so a port method added later cannot silently
// skip this classification.
var boundaryTranslationSkipped = map[string]string{
	"Platform":     "returns a static string, never an error",
	"Capabilities": "returns a struct, never an error",
	"IsConnected":  "returns a bool, never an error",
	"Disconnect":   "best-effort by contract, never returns a non-nil error",
	"WaitForIdle":  "returns a defensive true unconditionally, never issues a wire call",
	"DeviceInfo":   "derives its result from opts alone, never issues a wire call",
	"AgentInfo":    "returns a stored value, never an error and never issues a wire call",
	"CurrentForeground": "always returns a local unsupported diagnostic without touching the conn, " +
		"because this driver has no way to query the true foreground app",
	"Connect": "already checked directly by TestIosDriver_Connect_HungPing_SurfacesTypedTimeout " +
		"and its sibling failure tests, which assert its own typed classification",
	"Ping": "already checked directly by TestIosDriver_Ping_Hung_SurfacesTypedUnreachable, " +
		"which asserts its own typed classification",
	"ListApps":       "issues a host subprocess call through process.Runner (xcrun), not the TCP transport this boundary classifies",
	"InstallApp":     "issues a host subprocess call through process.Runner (xcrun), not the TCP transport this boundary classifies",
	"UninstallApp":   "issues a host subprocess call through process.Runner (xcrun), not the TCP transport this boundary classifies",
	"ClearAppData":   "issues a host subprocess call through process.Runner (xcrun), not the TCP transport this boundary classifies",
	"IsAppInstalled": "issues a host subprocess call through process.Runner (xcrun), not the TCP transport this boundary classifies",
}

// TestIosDriver_BoundaryTranslation_CoversEveryDriverMethod proves
// that wireCallMethodCases plus boundaryTranslationSkipped account for
// every method on driver.Driver, so a method added to the port
// without being triaged into one of the two fails this test instead
// of silently escaping the boundary-translation classification
// TestIosDriver_BoundaryTranslation_EveryWireCallMethod checks.
func TestIosDriver_BoundaryTranslation_CoversEveryDriverMethod(t *testing.T) {
	covered := map[string]bool{}
	for _, tc := range wireCallMethodCases {
		covered[tc.name] = true
	}
	for name := range boundaryTranslationSkipped {
		covered[name] = true
	}

	driverType := reflect.TypeOf((*driver.Driver)(nil)).Elem()
	for i := 0; i < driverType.NumMethod(); i++ {
		name := driverType.Method(i).Name
		if !covered[name] {
			t.Errorf("driver.Driver method %s is covered by neither wireCallMethodCases nor boundaryTranslationSkipped; add it to one", name)
		}
	}
}

// deadEndpointDriver builds an IosDriver bound to a TCPClient that was
// never connected, so every wire call it issues fails immediately with
// TCPErrNotConnected, the same classification a dial failure against a
// dead listener produces.
func deadEndpointDriver(t *testing.T) *iosdriver.IosDriver {
	t.Helper()
	conn := transport.NewTCPClient("127.0.0.1", 1, transport.WithTCPResponseTimeout(responseTimeout))
	return iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "fault-injection", Kind: iosdriver.KindSimulator})
}

// agentRejectionDriver builds an IosDriver whose conn is connected to
// a real runner that answers every command with ok:false, so every
// wire call it issues completes but is reported as an agent rejection
// rather than a connectivity failure.
func agentRejectionDriver(t *testing.T) *iosdriver.IosDriver {
	t.Helper()
	agent := newRejectingIosAgent(t)
	host, port := agent.hostPort(t)
	conn := transport.NewTCPClient(host, port, transport.WithTCPResponseTimeout(responseTimeout))
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("conn.Connect() against a rejecting runner: %v", err)
	}
	return iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "fault-injection", Kind: iosdriver.KindSimulator})
}

// TestIosDriver_BoundaryTranslation_SanitizesLeakingAgentRejectionMessage
// proves that when the runner's own rejection text names a private
// XCTest symbol, fail neutralizes it before it reaches the caller
// instead of passing the runner's message through unchanged, even
// though the runner is the party that should never have produced that
// shape in the first place.
func TestIosDriver_BoundaryTranslation_SanitizesLeakingAgentRejectionMessage(t *testing.T) {
	const leaking = `privateSymbolMissing(symbol: "XCSynthesizedEventRecord")`
	agent := newRejectingIosAgentWithMessage(t, leaking)
	host, port := agent.hostPort(t)
	conn := transport.NewTCPClient(host, port, transport.WithTCPResponseTimeout(responseTimeout))
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("conn.Connect() against a rejecting runner: %v", err)
	}
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "fault-injection", Kind: iosdriver.KindSimulator})

	err := d.Tap(context.Background(), wire.Point{X: 1, Y: 1})
	if err == nil {
		t.Fatal("Tap() against a runner reporting a leaking rejection message returned no error")
	}
	code, message, _ := diag.Describe(err)
	if code != diag.CodeAgentRejected {
		t.Fatalf("Tap() against a runner reporting a leaking rejection message: diag.Describe code = %q, want %q", code, diag.CodeAgentRejected)
	}
	if strings.Contains(message, "XCSynthesizedEventRecord") {
		t.Fatalf("Tap() against a runner reporting a leaking rejection message: message = %q, want the private symbol stripped", message)
	}
}

// TestIosDriver_FailedCommandReply_SurfacesRunnerMessageAndKeepsConnection
// proves that an ok:false reply a runner sends for a command whose UI
// action failed reaches the caller as a rejected request carrying the
// runner's own short message, not as an unreachable or communication
// failure, and that the same connection still answers the next call.
func TestIosDriver_FailedCommandReply_SurfacesRunnerMessageAndKeepsConnection(t *testing.T) {
	const runnerMessage = "Unable to find a focused element"
	agent := newRejectingIosAgentWithMessage(t, runnerMessage)
	host, port := agent.hostPort(t)
	conn := transport.NewTCPClient(host, port, transport.WithTCPResponseTimeout(responseTimeout))
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("conn.Connect() against a rejecting runner: %v", err)
	}
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "fault-injection", Kind: iosdriver.KindSimulator})

	for attempt := 1; attempt <= 2; attempt++ {
		err := d.Tap(context.Background(), wire.Point{X: 1, Y: 1})
		if err == nil {
			t.Fatalf("Tap() attempt %d against a runner reporting a failed action returned no error", attempt)
		}
		code, message, _ := diag.Describe(err)
		if code != diag.CodeAgentRejected {
			t.Fatalf("Tap() attempt %d: diag.Describe code = %q, want %q", attempt, code, diag.CodeAgentRejected)
		}
		if message != runnerMessage {
			t.Fatalf("Tap() attempt %d: message = %q, want %q", attempt, message, runnerMessage)
		}
	}
}

// assertNoForbiddenTransportSubstring fails t when message contains
// any shape of a leaked TCP address, port, or dial diagnostic,
// case-insensitively.
func assertNoForbiddenTransportSubstring(t *testing.T, methodName, message string) {
	t.Helper()
	lower := strings.ToLower(message)
	for _, forbidden := range forbiddenTransportSubstrings {
		if strings.Contains(lower, strings.ToLower(forbidden)) {
			t.Fatalf("%s error %q contains forbidden substring %q", methodName, message, forbidden)
		}
	}
}

// TestIosDriver_BoundaryTranslation_EveryWireCallMethod is the
// enforcement gate for the boundary IosDriver.fail implements: driven
// with minimal valid arguments, every method in wireCallMethodCases
// must return an error whose string names no TCP address, port, or
// dial diagnostic, whether the runner could not be reached at all (a
// conn that was never connected) or was reached and reported the
// request itself failed (a runner that rejects every command).
func TestIosDriver_BoundaryTranslation_EveryWireCallMethod(t *testing.T) {
	t.Run("dead endpoint", func(t *testing.T) {
		for _, tc := range wireCallMethodCases {
			t.Run(tc.name, func(t *testing.T) {
				d := deadEndpointDriver(t)
				err := tc.call(context.Background(), d)
				if err == nil {
					t.Fatalf("%s against a dead endpoint returned no error", tc.name)
				}
				assertNoForbiddenTransportSubstring(t, tc.name, err.Error())
				if code, _, _ := diag.Describe(err); code != diag.CodeAgentUnreachable {
					t.Fatalf("%s against a dead endpoint: diag.Describe code = %q, want %q (error: %v)",
						tc.name, code, diag.CodeAgentUnreachable, err)
				}
			})
		}
	})

	t.Run("agent rejection", func(t *testing.T) {
		for _, tc := range wireCallMethodCases {
			t.Run(tc.name, func(t *testing.T) {
				d := agentRejectionDriver(t)
				err := tc.call(context.Background(), d)
				if err == nil {
					t.Fatalf("%s against a runner that rejects every command returned no error", tc.name)
				}
				assertNoForbiddenTransportSubstring(t, tc.name, err.Error())
				if code, _, _ := diag.Describe(err); code != diag.CodeAgentRejected {
					t.Fatalf("%s against a runner that rejects every command: diag.Describe code = %q, want %q (error: %v)",
						tc.name, code, diag.CodeAgentRejected, err)
				}
			})
		}
	})
}
