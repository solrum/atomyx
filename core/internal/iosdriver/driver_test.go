package iosdriver_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/drivertest"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
	"dev.atomyx/internal/version"
	"dev.atomyx/internal/wire"
)

// fakeConn implements iosdriver.Conn over an in-memory command table.
// Each CallInto is recorded in Calls, in dispatch order, and answered
// by the handler registered for that command type via Respond; a
// command with no registered handler is answered with an empty
// object, which decodes into a zero-valued out.
type fakeConn struct {
	mu sync.Mutex

	connected  bool
	connectErr error

	handlers map[string]func(args map[string]any) (map[string]any, error)
	calls    []fakeCall
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
	if f.connectErr != nil {
		return f.connectErr
	}
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
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out)
}

var _ iosdriver.Conn = (*fakeConn)(nil)

// newDefaultFakeConn wires the responses the driver contract suite
// needs from any driver: a positive screen size, and unconditionally
// successful app-launch and stop commands.
func newDefaultFakeConn() *fakeConn {
	c := newFakeConn()
	var landscape atomic.Bool
	c.Respond("getScreenSize", func(map[string]any) (map[string]any, error) {
		if landscape.Load() {
			return map[string]any{"width": 2340.0, "height": 1080.0}, nil
		}
		return map[string]any{"width": 1080.0, "height": 2340.0}, nil
	})
	c.Respond("setOrientation", func(args map[string]any) (map[string]any, error) {
		name, _ := args["orientation"].(string)
		landscape.Store(name != "portrait")
		return map[string]any{"orientation": name}, nil
	})
	c.Respond("setLocation", func(map[string]any) (map[string]any, error) {
		return map[string]any{"set": true}, nil
	})
	c.Respond("openLink", func(map[string]any) (map[string]any, error) {
		return map[string]any{"opened": true}, nil
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

// iosContractRunner is a stateful process.Runner test double that
// tracks installed app ids across install and uninstall calls, the
// way a real simulator does, so the driver contract suite's install
// and uninstall roundtrip observes consistent state.
type iosContractRunner struct {
	mu        sync.Mutex
	installed map[string]bool
}

func newIosContractRunner() *iosContractRunner {
	return &iosContractRunner{installed: map[string]bool{}}
}

func (r *iosContractRunner) Run(ctx context.Context, name string, args ...string) (process.Result, error) {
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

var _ process.Runner = (*iosContractRunner)(nil)

func TestIosDriver_SatisfiesContract(t *testing.T) {
	var conn *fakeConn
	drivertest.Run(t, func() driver.Driver {
		conn = newDefaultFakeConn()
		return iosdriver.New(conn, newIosContractRunner(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
	}, drivertest.WithSyntheticFixtures(drivertest.Hooks{
		DeviceCalls: func() int { return len(conn.Calls()) },
		FailConnect: func() {
			conn.Respond("ping", func(map[string]any) (map[string]any, error) {
				return nil, errors.New("drivertest: simulated unreachable agent")
			})
		},
		FailPing: func() {
			conn.Respond("ping", func(map[string]any) (map[string]any, error) {
				return nil, errors.New("drivertest: simulated unreachable agent")
			})
		},
	}))
}

func TestIosDriver_Connect_Simulator_ConnectsAndPingsWithoutIproxy(t *testing.T) {
	conn := newDefaultFakeConn()
	conn.Respond("ping", func(map[string]any) (map[string]any, error) {
		return map[string]any{"protocolVersion": version.AgentProtocolVersion, "capabilities": map[string]any{"canMultiPointer": true, "canPressure": true}}, nil
	})
	runner := process.NewFake()
	d := iosdriver.New(conn, runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !d.IsConnected() {
		t.Fatal("IsConnected() = false after Connect")
	}
	if !d.Capabilities().CanMultiPointer || !d.Capabilities().CanPressure {
		t.Fatalf("Capabilities() = %+v, want ping capabilities applied", d.Capabilities())
	}
	d.WaitForeignAutomationProbe()
	inv := runner.Invocations()
	if len(inv) != 1 || inv[0].Name != "xcrun" || inv[0].Args[len(inv[0].Args)-1] != "list" {
		t.Fatalf("runner invocations = %+v, want only the foreign automation probe for a simulator connect", inv)
	}
}

// tunnelBackground is a process.BackgroundRunner test double modeling
// iproxy's long-running USB tunnel process. Start parses the host
// port out of its "<port>:<port>" mapping argument and returns a live
// process whose body waits readyDelay (modeling the tunnel coming
// up), opens a real listener on that port so a canConnectTCP probe
// against it succeeds, then blocks until Kill and closes the listener
// before exiting. When exitErr is set, the process instead exits
// immediately with that error without ever opening a listener,
// modeling iproxy dying on launch. When startErr is set, Start itself
// fails, modeling a missing binary. stopped, when set, is closed once
// the process body returns, for any reason.
type tunnelBackground struct {
	readyDelay time.Duration
	exitErr    error
	startErr   error
	stopped    chan struct{}

	mu          sync.Mutex
	invocations []process.Invocation
}

func (r *tunnelBackground) Start(ctx context.Context, name string, args ...string) (process.BackgroundProcess, error) {
	r.mu.Lock()
	r.invocations = append(r.invocations, process.Invocation{Name: name, Args: append([]string(nil), args...)})
	r.mu.Unlock()
	if r.startErr != nil {
		return nil, r.startErr
	}

	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	pr, pw := io.Pipe()
	proc := &tunnelProcess{cancel: cancel, stdout: pr, done: make(chan struct{})}
	go func() {
		defer close(proc.done)
		defer func() { _ = pw.Close() }()
		if r.stopped != nil {
			defer close(r.stopped)
		}
		proc.err = r.serve(ctx, args)
	}()
	return proc, nil
}

func (r *tunnelBackground) serve(ctx context.Context, args []string) error {
	if r.exitErr != nil {
		return r.exitErr
	}
	port, err := strconv.Atoi(strings.SplitN(args[0], ":", 2)[0])
	if err != nil {
		return fmt.Errorf("tunnelBackground: parse port mapping %q: %w", args[0], err)
	}
	if r.readyDelay > 0 {
		select {
		case <-time.After(r.readyDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()
	<-ctx.Done()
	return ctx.Err()
}

func (r *tunnelBackground) Invocations() []process.Invocation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]process.Invocation(nil), r.invocations...)
}

var _ process.BackgroundRunner = (*tunnelBackground)(nil)

type tunnelProcess struct {
	cancel   context.CancelFunc
	stdout   *io.PipeReader
	done     chan struct{}
	err      error
	exitCode int
	stderr   string
	pid      int
}

func (p *tunnelProcess) Pid() int { return p.pid }

func (p *tunnelProcess) Stdout() io.Reader { return p.stdout }

func (p *tunnelProcess) Wait() (process.BackgroundResult, error) {
	<-p.done
	return process.BackgroundResult{ExitCode: p.exitCode, Stderr: p.stderr}, p.err
}

func (p *tunnelProcess) Kill() error {
	p.cancel()
	<-p.done
	return nil
}

func (p *tunnelProcess) Interrupt(time.Duration) error { return p.Kill() }

// freeTCPPort returns a host port with no listener bound at test
// start, for a test that wants to hand iosdriver.Options a port
// number without yet occupying it.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freeTCPPort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// startFakeTunnelListener binds a real listener that either answers
// the ping handshake launchIproxy's pre-spawn probe sends
// (answerPing true, modeling an existing Atomyx driver tunnel) or
// closes every connection without answering (modeling an unrelated
// process bound to the same port). It returns the port and a cleanup
// function that blocks until the accept loop has exited, so a test
// using it never leaks a background listener or goroutine.
func startFakeTunnelListener(t *testing.T, answerPing bool) (int, func()) {
	t.Helper()
	return startFakeRunnerListener(t, answerPing, defaultIdentityFields)
}

// startFakeRunnerListener is startFakeTunnelListener with the identity
// fields of its ping reply scripted by the caller, as the inner part of
// the reply's data object, for example `"simulatorUdid":"SIM-2"`.
func startFakeRunnerListener(t *testing.T, answerPing bool, identityFields string) (int, func()) {
	t.Helper()
	return startReplyingListener(t, answerPing, func(id int64) string { return pongReply(id, identityFields) })
}

// startRawRunnerListener starts a listener whose ping reply carries
// exactly fields, so a test controls whether protocolVersion appears.
func startRawRunnerListener(t *testing.T, fields string) (int, func()) {
	t.Helper()
	return startReplyingListener(t, true, func(id int64) string { return rawPongReply(id, fields) })
}

func startReplyingListener(t *testing.T, answerPing bool, reply func(id int64) string) (int, func()) {
	t.Helper()
	l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startFakeTunnelListener: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			if !answerPing {
				_ = c.Close()
				continue
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				line, err := bufio.NewReader(c).ReadString('\n')
				if err != nil {
					return
				}
				var req struct {
					ID int64 `json:"id"`
				}
				_ = json.Unmarshal([]byte(line), &req)
				_, _ = fmt.Fprint(c, reply(req.ID))
			}(c)
		}
	}()
	port := l.Addr().(*net.TCPAddr).Port
	return port, func() {
		_ = l.Close()
		<-done
	}
}

func TestIosDriver_Connect_Device_LaunchesIproxyWithExactArgsAndWaitsForReadiness(t *testing.T) {
	port := freeTCPPort(t)
	conn := newDefaultFakeConn()
	conn.Respond("ping", func(map[string]any) (map[string]any, error) {
		return map[string]any{"protocolVersion": version.AgentProtocolVersion, "capabilities": map[string]any{"canMultiPointer": true, "canPressure": true}}, nil
	})
	bg := &tunnelBackground{}
	runner := process.NewFake()
	d := iosdriver.New(conn, runner, iosdriver.Options{UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: port, Background: bg})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !d.IsConnected() {
		t.Fatal("IsConnected() = false after Connect")
	}

	d.WaitForeignAutomationProbe()
	if inv := runner.Invocations(); len(inv) != 1 || inv[0].Args[0] != "devicectl" {
		t.Fatalf("Runner.Run invocations = %+v, want only the foreign automation probe: iproxy is long-lived and must not go through Run", inv)
	}
	invocations := bg.Invocations()
	if len(invocations) != 1 {
		t.Fatalf("background starts = %+v, want exactly one iproxy launch", invocations)
	}
	want := process.Invocation{Name: "iproxy", Args: []string{fmt.Sprintf("%d:22087", port), "-u", "DEV-1"}}
	if invocations[0].Name != want.Name || !equalArgs(invocations[0].Args, want.Args) {
		t.Fatalf("iproxy invocation = %+v, want %+v", invocations[0], want)
	}

	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
}

func TestIosDriver_Connect_Device_WaitsForTunnelToBecomeReadyAfterDelay(t *testing.T) {
	port := freeTCPPort(t)
	conn := newDefaultFakeConn()
	conn.Respond("ping", func(map[string]any) (map[string]any, error) {
		return map[string]any{"protocolVersion": version.AgentProtocolVersion, "capabilities": map[string]any{}}, nil
	})
	const readyDelay = 300 * time.Millisecond
	bg := &tunnelBackground{readyDelay: readyDelay}
	runner := process.NewFake()
	d := iosdriver.New(conn, runner, iosdriver.Options{UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: port, Background: bg})

	start := time.Now()
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed < readyDelay {
		t.Fatalf("Connect() returned after %v, want it to wait at least %v for the tunnel to become reachable", elapsed, readyDelay)
	}
	if !d.IsConnected() {
		t.Fatal("IsConnected() = false after Connect")
	}

	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
}

func TestIosDriver_Disconnect_KillsRunningIproxy(t *testing.T) {
	port := freeTCPPort(t)
	stopped := make(chan struct{})
	bg := &tunnelBackground{stopped: stopped}
	runner := process.NewFake()
	d := iosdriver.New(newDefaultFakeConn(), runner, iosdriver.Options{UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: port, Background: bg})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	select {
	case <-stopped:
		t.Fatal("iproxy process exited before Disconnect killed it")
	default:
	}

	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("iproxy process did not exit after Disconnect")
	}
}

func TestIosDriver_Connect_Device_IproxyExitFailureSurfacesNamedSpawnExitError(t *testing.T) {
	port := freeTCPPort(t)
	bg := &tunnelBackground{exitErr: errors.New("iproxy: no device found with udid DEV-1")}
	runner := process.NewFake()
	d := iosdriver.New(newDefaultFakeConn(), runner, iosdriver.Options{UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: port, Background: bg})

	err := d.Connect(context.Background())
	var iproxyErr *iosdriver.IproxyError
	if !errors.As(err, &iproxyErr) {
		t.Fatalf("Connect() error = %v (%T), want a *iosdriver.IproxyError", err, err)
	}
	if iproxyErr.Code != iosdriver.IproxyErrSpawnExit {
		t.Fatalf("IproxyError.Code = %q, want %q", iproxyErr.Code, iosdriver.IproxyErrSpawnExit)
	}
	if d.IsConnected() {
		t.Fatal("IsConnected() = true, want the transport never dialed after a failed tunnel launch")
	}
}

func TestIosDriver_Connect_Device_IproxyNotInstalledSurfacesNamedError(t *testing.T) {
	port := freeTCPPort(t)
	bg := &tunnelBackground{startErr: &exec.Error{Name: "iproxy", Err: exec.ErrNotFound}}
	runner := process.NewFake()
	d := iosdriver.New(newDefaultFakeConn(), runner, iosdriver.Options{UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: port, Background: bg})

	err := d.Connect(context.Background())
	var iproxyErr *iosdriver.IproxyError
	if !errors.As(err, &iproxyErr) {
		t.Fatalf("Connect() error = %v (%T), want a *iosdriver.IproxyError", err, err)
	}
	if iproxyErr.Code != iosdriver.IproxyErrNotInstalled {
		t.Fatalf("IproxyError.Code = %q, want %q", iproxyErr.Code, iosdriver.IproxyErrNotInstalled)
	}
}

// TestIosDriver_Connect_Device_IproxyTunnelTimeout_DrivenByFakeClock
// guards IproxyErrTunnelTimeout deterministically: the tunnel runner
// never opens its listener within the test's real wall-clock budget,
// so the only thing that can end the wait is the injected fakeClock
// reaching the tunnel-up deadline.
func TestIosDriver_Connect_Device_IproxyTunnelTimeout_DrivenByFakeClock(t *testing.T) {
	port := freeTCPPort(t)
	bg := &tunnelBackground{readyDelay: time.Hour}
	runner := process.NewFake()
	d := iosdriver.New(newDefaultFakeConn(), runner, iosdriver.Options{
		UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: port, Clock: &fakeClock{}, Background: bg,
	})

	err := d.Connect(context.Background())
	var iproxyErr *iosdriver.IproxyError
	if !errors.As(err, &iproxyErr) {
		t.Fatalf("Connect() error = %v (%T), want a *iosdriver.IproxyError", err, err)
	}
	if iproxyErr.Code != iosdriver.IproxyErrTunnelTimeout {
		t.Fatalf("IproxyError.Code = %q, want %q", iproxyErr.Code, iosdriver.IproxyErrTunnelTimeout)
	}
	if d.IsConnected() {
		t.Fatal("IsConnected() = true, want the transport never dialed after a timed-out tunnel wait")
	}
}

func TestIosDriver_Connect_Device_ReusesExistingAtomyxTunnelWithoutSpawning(t *testing.T) {
	port, cleanup := startFakeTunnelListener(t, true)
	defer cleanup()

	runner := process.NewFake()
	bg := process.NewFakeBackground()
	d := iosdriver.New(newDefaultFakeConn(), runner, iosdriver.Options{UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: port, Background: bg})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	d.WaitForeignAutomationProbe()
	if inv := runner.Invocations(); len(inv) != 1 || inv[0].Args[0] != "devicectl" || len(bg.Invocations()) != 0 {
		t.Fatalf("invocations = %+v, want no iproxy spawn when an existing tunnel answers the ping handshake", runner.Invocations())
	}
	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
}

func TestIosDriver_Connect_Device_ForeignListenerRefusesWithBindConflict(t *testing.T) {
	port, cleanup := startFakeTunnelListener(t, false)
	defer cleanup()

	runner := process.NewFake()
	bg := process.NewFakeBackground()
	d := iosdriver.New(newDefaultFakeConn(), runner, iosdriver.Options{UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: port, Background: bg})

	err := d.Connect(context.Background())
	var iproxyErr *iosdriver.IproxyError
	if !errors.As(err, &iproxyErr) {
		t.Fatalf("Connect() error = %v (%T), want a *iosdriver.IproxyError", err, err)
	}
	if iproxyErr.Code != iosdriver.IproxyErrBindConflict {
		t.Fatalf("IproxyError.Code = %q, want %q", iproxyErr.Code, iosdriver.IproxyErrBindConflict)
	}
	if len(runner.Invocations()) != 0 {
		t.Fatalf("invocations = %+v, want no iproxy spawn when the port is held by a foreign listener", bg.Invocations())
	}
}

// TestIosDriver_Connect_TransportFailureSurfacesAgentUnreachableCode
// guards the boundary contract: a transport connect failure must be
// describable through diag.Describe as AGENT_UNREACHABLE, with a
// non-empty remedy.
func TestIosDriver_Connect_TransportFailureSurfacesAgentUnreachableCode(t *testing.T) {
	conn := newFakeConn()
	conn.connectErr = errors.New("dial tcp 127.0.0.1:22087: connect: connection refused")
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	err := d.Connect(context.Background())
	if err == nil {
		t.Fatal("Connect() error = nil, want the transport failure surfaced")
	}
	code, _, remedy := diag.Describe(err)
	if code != diag.CodeAgentUnreachable {
		t.Fatalf("Connect() code = %q, want %q", code, diag.CodeAgentUnreachable)
	}
	if remedy == "" {
		t.Fatal("Connect() remedy is empty, want an actionable hint")
	}
}

func TestIosDriver_Connect_PingFailure_ReturnsError(t *testing.T) {
	conn := newFakeConn()
	conn.Respond("ping", func(map[string]any) (map[string]any, error) {
		return nil, errors.New("driver-error: ping unsupported")
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.Connect(context.Background()); err == nil {
		t.Fatal("Connect() error = nil, want the ping failure surfaced")
	}
}

func TestIosDriver_Ping_Succeeds(t *testing.T) {
	conn := newDefaultFakeConn()
	conn.Respond("ping", func(map[string]any) (map[string]any, error) {
		return map[string]any{"protocolVersion": version.AgentProtocolVersion, "capabilities": map[string]any{"canMultiPointer": false, "canPressure": false}}, nil
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

// TestIosDriver_Ping_RoundTripFailure_ReturnsAgentUnreachable guards
// the boundary contract: a failed liveness probe must be describable
// through diag.Describe as AGENT_UNREACHABLE, with a non-empty
// remedy.
func TestIosDriver_Ping_RoundTripFailure_ReturnsAgentUnreachable(t *testing.T) {
	conn := newDefaultFakeConn()
	conn.Respond("ping", func(map[string]any) (map[string]any, error) {
		return nil, errors.New("broken pipe")
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	err := d.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping() error = nil, want the round trip failure surfaced")
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

func TestIosDriver_Tap_SendsCoordinates(t *testing.T) {
	conn := newFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.Tap(context.Background(), wire.Point{X: 10, Y: 20}); err != nil {
		t.Fatalf("Tap() error = %v", err)
	}
	calls := conn.Calls()
	if len(calls) != 1 || calls[0].cmdType != "tapAt" {
		t.Fatalf("calls = %+v, want one tapAt call", calls)
	}
	if calls[0].args["x"] != 10.0 || calls[0].args["y"] != 20.0 {
		t.Fatalf("tapAt args = %+v, want x=10 y=20", calls[0].args)
	}
}

func TestIosDriver_LongPress_SendsDuration(t *testing.T) {
	conn := newFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.LongPress(context.Background(), wire.Point{X: 1, Y: 2}, 900); err != nil {
		t.Fatalf("LongPress() error = %v", err)
	}
	calls := conn.Calls()
	if calls[0].args["durationMs"] != 900 {
		t.Fatalf("longPressAt args = %+v, want durationMs=900", calls[0].args)
	}
}

func TestIosDriver_Swipe_SendsEndpointsAndDuration(t *testing.T) {
	conn := newFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.Swipe(context.Background(), wire.Point{X: 1, Y: 2}, wire.Point{X: 3, Y: 4}, 300); err != nil {
		t.Fatalf("Swipe() error = %v", err)
	}
	got := conn.Calls()[0].args
	want := map[string]any{"fromX": 1.0, "fromY": 2.0, "toX": 3.0, "toY": 4.0, "durationMs": 300}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("swipe args[%s] = %v, want %v (full args %+v)", k, got[k], v, got)
		}
	}
}

func TestIosDriver_DispatchGesture_RejectsMultiPointerWithoutCapability(t *testing.T) {
	d := iosdriver.New(newFakeConn(), process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{{ID: "a"}, {ID: "b"}}}

	if _, err := d.DispatchGesture(context.Background(), gesture); err == nil {
		t.Fatal("DispatchGesture() error = nil, want a multi-pointer rejection")
	}
}

func TestIosDriver_DispatchGesture_RejectsPressureWithoutCapability(t *testing.T) {
	d := iosdriver.New(newFakeConn(), process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
	pressure := 0.5
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{
		{ID: "a", Waypoints: []driver.GestureWaypoint{{Phase: driver.GesturePhaseDown, Pressure: &pressure}}},
	}}

	if _, err := d.DispatchGesture(context.Background(), gesture); err == nil {
		t.Fatal("DispatchGesture() error = nil, want a pressure rejection")
	}
}

// TestIosDriver_DispatchGesture_ForwardsCaptureWaypointAndDecodesImage
// asserts a mid-gesture capture waypoint is forwarded to the runner
// unmodified and the runner's base64 image decodes into
// GestureResult.CapturedImages.
func TestIosDriver_DispatchGesture_ForwardsCaptureWaypointAndDecodesImage(t *testing.T) {
	imageBytes := []byte("fake-png-bytes")
	conn := newFakeConn()
	conn.Respond("dispatchPointer", func(map[string]any) (map[string]any, error) {
		return map[string]any{
			"capturedImages": []map[string]any{
				{"base64": base64.StdEncoding.EncodeToString(imageBytes), "mimeType": "image/png"},
			},
		}, nil
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
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
	if string(result.CapturedImages[0].Data) != string(imageBytes) {
		t.Fatalf("CapturedImages[0].Data = %q, want %q", result.CapturedImages[0].Data, imageBytes)
	}
	if result.CapturedImages[0].MimeType != "image/png" {
		t.Fatalf("CapturedImages[0].MimeType = %q, want image/png", result.CapturedImages[0].MimeType)
	}

	call := conn.Calls()[0]
	pointers, ok := call.args["pointers"].([]map[string]any)
	if !ok || len(pointers) != 1 {
		t.Fatalf("dispatchPointer args = %+v, want one pointer", call.args)
	}
	waypoints, ok := pointers[0]["waypoints"].([]map[string]any)
	if !ok || len(waypoints) != 3 {
		t.Fatalf("waypoints = %+v, want 3", pointers[0]["waypoints"])
	}
	if waypoints[1]["phase"] != "capture" {
		t.Fatalf("waypoints[1][\"phase\"] = %v, want capture", waypoints[1]["phase"])
	}
}

func TestIosDriver_DispatchGesture_ForwardsWaypointsToConn(t *testing.T) {
	conn := newFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
	gesture := driver.Gesture{Pointers: []driver.GesturePointer{
		{ID: "finger1", Waypoints: []driver.GestureWaypoint{
			{Phase: driver.GesturePhaseDown, Point: wire.Point{X: 1, Y: 2}, AtOffsetSeconds: 0},
			{Phase: driver.GesturePhaseUp, Point: wire.Point{X: 1, Y: 2}, AtOffsetSeconds: 0.1},
		}},
	}}

	if _, err := d.DispatchGesture(context.Background(), gesture); err != nil {
		t.Fatalf("DispatchGesture() error = %v", err)
	}
	call := conn.Calls()[0]
	pointers, ok := call.args["pointers"].([]map[string]any)
	if !ok || len(pointers) != 1 {
		t.Fatalf("dispatchPointer args = %+v, want one pointer", call.args)
	}
	if pointers[0]["id"] != "finger1" {
		t.Fatalf("pointer id = %v, want finger1", pointers[0]["id"])
	}
	waypoints, ok := pointers[0]["waypoints"].([]map[string]any)
	if !ok || len(waypoints) != 2 {
		t.Fatalf("waypoints = %+v, want 2", pointers[0]["waypoints"])
	}
}

func TestIosDriver_InputText_SendsText(t *testing.T) {
	conn := newFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.InputText(context.Background(), "hello"); err != nil {
		t.Fatalf("InputText() error = %v", err)
	}
	if conn.Calls()[0].args["text"] != "hello" {
		t.Fatalf("typeText args = %+v, want text=hello", conn.Calls()[0].args)
	}
}

func TestIosDriver_EraseText_SendsMaxDeletes(t *testing.T) {
	conn := newFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.EraseText(context.Background(), 5); err != nil {
		t.Fatalf("EraseText() error = %v", err)
	}
	if conn.Calls()[0].args["maxDeletes"] != 5 {
		t.Fatalf("clearFocusedInput args = %+v, want maxDeletes=5", conn.Calls()[0].args)
	}
}

func TestIosDriver_PressKey_ReportsAffordanceFoundAsOk(t *testing.T) {
	conn := newFakeConn()
	conn.Respond("pressKey", func(map[string]any) (map[string]any, error) {
		return map[string]any{"affordanceFound": true, "strategy": "nav_bar_back"}, nil
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	res, err := d.PressKey(context.Background(), driver.KeyBack)
	if err != nil {
		t.Fatalf("PressKey() error = %v", err)
	}
	if !res.Ok || res.Reason != "nav_bar_back" {
		t.Fatalf("PressKey() = %+v, want ok=true reason=nav_bar_back", res)
	}
}

func TestIosDriver_HideKeyboard_ReportsOkAndStrategy(t *testing.T) {
	conn := newFakeConn()
	conn.Respond("hideKeyboard", func(map[string]any) (map[string]any, error) {
		return map[string]any{"ok": true, "strategy": "swipe_down"}, nil
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	res, err := d.HideKeyboard(context.Background())
	if err != nil {
		t.Fatalf("HideKeyboard() error = %v", err)
	}
	if !res.Ok || res.Reason != "swipe_down" {
		t.Fatalf("HideKeyboard() = %+v, want ok=true reason=swipe_down", res)
	}
}

func TestIosDriver_LaunchApp_PlainLaunch_AlwaysDispatches(t *testing.T) {
	conn := newFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.LaunchApp(context.Background(), "dev.atomyx.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp() error = %v", err)
	}
	calls := conn.Calls()
	if len(calls) != 1 || calls[0].cmdType != "launchApp" {
		t.Fatalf("calls = %+v, want one launchApp call", calls)
	}
	if calls[0].args["noReset"] != false {
		t.Fatalf("launchApp args = %+v, want noReset=false for a plain launch", calls[0].args)
	}
}

func TestIosDriver_LaunchApp_FreshReset_StopsThenLaunches(t *testing.T) {
	conn := newFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
	if err := d.LaunchApp(context.Background(), "dev.atomyx.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("initial LaunchApp() error = %v", err)
	}

	noReset := false
	if err := d.LaunchApp(context.Background(), "dev.atomyx.app", driver.LaunchArgs{NoReset: &noReset}); err != nil {
		t.Fatalf("fresh LaunchApp() error = %v", err)
	}
	calls := conn.Calls()
	cmdTypes := make([]string, len(calls))
	for i, c := range calls {
		cmdTypes[i] = c.cmdType
	}
	want := []string{"launchApp", "forceStopApp", "launchApp"}
	if !equalArgs(cmdTypes, want) {
		t.Fatalf("call sequence = %v, want %v", cmdTypes, want)
	}
}

func TestIosDriver_LaunchApp_Attach_SkipsWhenAlreadyForeground(t *testing.T) {
	conn := newFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
	if err := d.LaunchApp(context.Background(), "dev.atomyx.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("initial LaunchApp() error = %v", err)
	}

	attach := true
	if err := d.LaunchApp(context.Background(), "dev.atomyx.app", driver.LaunchArgs{NoReset: &attach}); err != nil {
		t.Fatalf("attach LaunchApp() error = %v", err)
	}
	if len(conn.Calls()) != 1 {
		t.Fatalf("calls = %+v, want the attach launch to skip dispatch entirely", conn.Calls())
	}
}

func TestIosDriver_LaunchApp_Attach_FallsBackToPlainLaunchWhenNotTrackedYet(t *testing.T) {
	conn := newFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	attach := true
	if err := d.LaunchApp(context.Background(), "dev.atomyx.app", driver.LaunchArgs{NoReset: &attach}); err != nil {
		t.Fatalf("attach LaunchApp() error = %v", err)
	}
	if len(conn.Calls()) != 1 || conn.Calls()[0].cmdType != "launchApp" {
		t.Fatalf("calls = %+v, want one launchApp call", conn.Calls())
	}
}

// TestIosDriver_Connect_ResetsStaleTrackedBundleIDFromPriorConnection
// guards against a silent reconnect trusting launch state from a
// previous connection: without the reset, LaunchApp's NoReset attach
// fast path would wrongly skip dispatch instead of falling back to a
// plain launch of an app the new connection has never launched.
func TestIosDriver_Connect_ResetsStaleTrackedBundleIDFromPriorConnection(t *testing.T) {
	conn := newFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.LaunchApp(context.Background(), "dev.atomyx.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp() error = %v", err)
	}

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	attach := true
	if err := d.LaunchApp(context.Background(), "dev.atomyx.app", driver.LaunchArgs{NoReset: &attach}); err != nil {
		t.Fatalf("attach LaunchApp() error = %v", err)
	}
	launchCount := 0
	for _, c := range conn.Calls() {
		if c.cmdType == "launchApp" {
			launchCount++
		}
	}
	if launchCount != 2 {
		t.Fatalf("launchApp call count = %d, want 2: a reconnect must not let the stale bundle id short-circuit the NoReset attach", launchCount)
	}
}

// TestIosDriver_StopApp_ClearsTrackedForeground verifies StopApp
// clears the tracked bundle id by observing its effect on LaunchApp's
// NoReset attach fast path rather than through CurrentForeground: an
// attach launch right after StopApp must dispatch again instead of
// wrongly skipping, which only happens once the tracked bundle id is
// cleared.
func TestIosDriver_StopApp_ClearsTrackedForeground(t *testing.T) {
	conn := newFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
	if err := d.LaunchApp(context.Background(), "dev.atomyx.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp() error = %v", err)
	}
	if err := d.StopApp(context.Background(), "dev.atomyx.app"); err != nil {
		t.Fatalf("StopApp() error = %v", err)
	}
	attach := true
	if err := d.LaunchApp(context.Background(), "dev.atomyx.app", driver.LaunchArgs{NoReset: &attach}); err != nil {
		t.Fatalf("attach LaunchApp() error = %v", err)
	}
	launchCount := 0
	for _, c := range conn.Calls() {
		if c.cmdType == "launchApp" {
			launchCount++
		}
	}
	if launchCount != 2 {
		t.Fatalf("launchApp call count = %d, want 2: StopApp must clear the tracked bundle id so the attach fast path does not skip dispatch", launchCount)
	}
}

func TestIosDriver_KillApp_DelegatesToStopApp(t *testing.T) {
	conn := newFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
	if err := d.LaunchApp(context.Background(), "dev.atomyx.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp() error = %v", err)
	}
	if err := d.KillApp(context.Background(), "dev.atomyx.app"); err != nil {
		t.Fatalf("KillApp() error = %v", err)
	}
	calls := conn.Calls()
	if calls[len(calls)-1].cmdType != "forceStopApp" {
		t.Fatalf("last call = %+v, want forceStopApp", calls[len(calls)-1])
	}
}

func TestIosDriver_CurrentForeground_ReportsUnsupported(t *testing.T) {
	d := iosdriver.New(newFakeConn(), process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
	if d.Capabilities().CanQueryForeground {
		t.Fatal("Capabilities().CanQueryForeground = true, want false")
	}
	if err := d.LaunchApp(context.Background(), "dev.atomyx.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp() error = %v", err)
	}

	foreground, err := d.CurrentForeground(context.Background())
	if err == nil {
		t.Fatalf("CurrentForeground() = %+v, want the unsupported error even after a launch", foreground)
	}
	if foreground.BundleID != nil {
		t.Fatalf("CurrentForeground() returned bundleId %q alongside an error, want none", *foreground.BundleID)
	}
	code, _, _ := diag.Describe(err)
	if code != diag.CodeForegroundUnsupported {
		t.Fatalf("CurrentForeground() error code = %q, want %q", code, diag.CodeForegroundUnsupported)
	}
}

func TestIosDriver_ListApps_Simulator_RunsListappsThroughShell(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(
		process.ExactMatch("sh", "-c", "xcrun simctl listapps 'SIM-1' | plutil -convert json -o - -- -"),
		process.Result{Stdout: `{"dev.atomyx.app":{"CFBundleDisplayName":"Atomyx"}}`},
		nil,
	)
	d := iosdriver.New(newFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	apps, err := d.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}
	if len(apps) != 1 || apps[0].BundleID != "dev.atomyx.app" || apps[0].DisplayName != "Atomyx" {
		t.Fatalf("ListApps() = %+v, want one Atomyx app", apps)
	}
}

func TestIosDriver_ListApps_Device_RunsDevicectlWithExactArgs(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(
		process.ExactMatch("xcrun", "devicectl", "device", "info", "apps", "--device", "DEV-1", "--include-default-apps", "--json-output", "-"),
		process.Result{Stdout: `{"result":{"apps":[{"bundleIdentifier":"dev.atomyx.app","name":"Atomyx"}]}}`},
		nil,
	)
	d := iosdriver.New(newFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "DEV-1", Kind: iosdriver.KindDevice})

	apps, err := d.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}
	if len(apps) != 1 || apps[0].BundleID != "dev.atomyx.app" || apps[0].DisplayName != "Atomyx" {
		t.Fatalf("ListApps() = %+v, want one Atomyx app", apps)
	}
}

func TestIosDriver_ListApps_Device_GenuineFailurePropagates(t *testing.T) {
	runner := process.NewFake()
	runner.DefaultErr = errors.New("devicectl daemon unreachable")
	d := iosdriver.New(newFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "DEV-1", Kind: iosdriver.KindDevice})

	apps, err := d.ListApps(context.Background())
	if err == nil {
		t.Fatalf("ListApps() = %+v, err = nil, want the genuine devicectl failure surfaced", apps)
	}
}

func TestIosDriver_InstallApp_Simulator_ExactArgs(t *testing.T) {
	runner := process.NewFake()
	d := iosdriver.New(newFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.InstallApp(context.Background(), "/tmp/App.app"); err != nil {
		t.Fatalf("InstallApp() error = %v", err)
	}
	want := []string{"simctl", "install", "SIM-1", "/tmp/App.app"}
	if got := runner.Invocations(); len(got) != 1 || got[0].Name != "xcrun" || !equalArgs(got[0].Args, want) {
		t.Fatalf("invocations = %+v, want xcrun %v", got, want)
	}
}

func TestIosDriver_InstallApp_Device_ExactArgs(t *testing.T) {
	runner := process.NewFake()
	d := iosdriver.New(newFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "DEV-1", Kind: iosdriver.KindDevice})

	if err := d.InstallApp(context.Background(), "/tmp/App.ipa"); err != nil {
		t.Fatalf("InstallApp() error = %v", err)
	}
	want := []string{"devicectl", "device", "install", "app", "--device", "DEV-1", "/tmp/App.ipa"}
	if got := runner.Invocations(); len(got) != 1 || got[0].Name != "xcrun" || !equalArgs(got[0].Args, want) {
		t.Fatalf("invocations = %+v, want xcrun %v", got, want)
	}
}

func TestIosDriver_UninstallApp_Simulator_ExactArgs(t *testing.T) {
	runner := process.NewFake()
	d := iosdriver.New(newFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.UninstallApp(context.Background(), "dev.atomyx.app"); err != nil {
		t.Fatalf("UninstallApp() error = %v", err)
	}
	want := []string{"simctl", "uninstall", "SIM-1", "dev.atomyx.app"}
	if got := runner.Invocations(); len(got) != 1 || !equalArgs(got[0].Args, want) {
		t.Fatalf("invocations = %+v, want xcrun %v", got, want)
	}
}

func TestIosDriver_UninstallApp_Device_ExactArgs(t *testing.T) {
	runner := process.NewFake()
	d := iosdriver.New(newFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "DEV-1", Kind: iosdriver.KindDevice})

	if err := d.UninstallApp(context.Background(), "dev.atomyx.app"); err != nil {
		t.Fatalf("UninstallApp() error = %v", err)
	}
	want := []string{"devicectl", "device", "uninstall", "app", "--device", "DEV-1", "dev.atomyx.app"}
	if got := runner.Invocations(); len(got) != 1 || !equalArgs(got[0].Args, want) {
		t.Fatalf("invocations = %+v, want xcrun %v", got, want)
	}
}

func TestIosDriver_IsAppInstalled_Simulator_ExactArgsAndNotInstalledDetection(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(
		process.ExactMatch("xcrun", "simctl", "get_app_container", "SIM-1", "dev.atomyx.app", "app"),
		process.Result{Stdout: "/path/App.app"},
		nil,
	)
	runner.Stub(
		process.ExactMatch("xcrun", "simctl", "get_app_container", "SIM-1", "dev.atomyx.missing", "app"),
		process.Result{Stderr: "get_app_container: is not installed"},
		errors.New("exit status 2"),
	)
	d := iosdriver.New(newFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	installed, err := d.IsAppInstalled(context.Background(), "dev.atomyx.app")
	if err != nil {
		t.Fatalf("IsAppInstalled() error = %v", err)
	}
	if !installed {
		t.Fatal("IsAppInstalled() = false, want true")
	}

	installed, err = d.IsAppInstalled(context.Background(), "dev.atomyx.missing")
	if err != nil {
		t.Fatalf("IsAppInstalled() error = %v, want a swallowed not-installed error", err)
	}
	if installed {
		t.Fatal("IsAppInstalled() = true, want false")
	}
}

func TestIosDriver_IsAppInstalled_Simulator_GenuineFailurePropagates(t *testing.T) {
	runner := process.NewFake()
	runner.DefaultErr = errors.New("CoreSimulatorService is unavailable")
	d := iosdriver.New(newFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if _, err := d.IsAppInstalled(context.Background(), "dev.atomyx.app"); err == nil {
		t.Fatal("IsAppInstalled() error = nil, want the genuine simctl failure surfaced")
	}
}

func TestIosDriver_IsAppInstalled_Device_UsesAppList(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(
		process.ExactMatch("xcrun", "devicectl", "device", "info", "apps", "--device", "DEV-1", "--include-default-apps", "--json-output", "-"),
		process.Result{Stdout: `{"result":{"apps":[{"bundleIdentifier":"dev.atomyx.app","name":"Atomyx"}]}}`},
		nil,
	)
	d := iosdriver.New(newFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "DEV-1", Kind: iosdriver.KindDevice})

	installed, err := d.IsAppInstalled(context.Background(), "dev.atomyx.app")
	if err != nil {
		t.Fatalf("IsAppInstalled() error = %v", err)
	}
	if !installed {
		t.Fatal("IsAppInstalled() = false, want true")
	}

	installed, err = d.IsAppInstalled(context.Background(), "dev.atomyx.other")
	if err != nil {
		t.Fatalf("IsAppInstalled() error = %v", err)
	}
	if installed {
		t.Fatal("IsAppInstalled() = true, want false")
	}
}

func TestIosDriver_IsAppInstalled_Device_SeesSystemApps(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(
		process.ExactMatch("xcrun", "devicectl", "device", "info", "apps", "--device", "DEV-1", "--include-default-apps", "--json-output", "-"),
		process.Result{Stdout: `{"result":{"apps":[{"bundleIdentifier":"dev.atomyx.driver.host.xctrunner","name":"Runner"},{"bundleIdentifier":"com.apple.Preferences","name":"Settings"}]}}`},
		nil,
	)
	d := iosdriver.New(newFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "DEV-1", Kind: iosdriver.KindDevice})

	installed, err := d.IsAppInstalled(context.Background(), "com.apple.Preferences")
	if err != nil {
		t.Fatalf("IsAppInstalled() error = %v", err)
	}
	if !installed {
		t.Fatal("IsAppInstalled(com.apple.Preferences) = false, want true for a system app")
	}
}

func TestIosDriver_ClearAppData_RequiresSource(t *testing.T) {
	runner := process.NewFake()
	d := iosdriver.New(newFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	err := d.ClearAppData(context.Background(), "dev.atomyx.app", driver.ClearAppDataOptions{})
	if err == nil {
		t.Fatal("ClearAppData() error = nil, want an error when Source is empty")
	}
	code, message, remedy := diag.Describe(err)
	if code != diag.CodeInvalidArgument {
		t.Fatalf("code = %q, want %q", code, diag.CodeInvalidArgument)
	}
	if !strings.Contains(message, "dev.atomyx.app") || !strings.Contains(remedy, `"artifact"`) {
		t.Fatalf("message %q and remedy %q should name the app and the artifact field", message, remedy)
	}
	for _, banned := range []string{"iOS", "opts.", "Source"} {
		if strings.Contains(message+remedy, banned) {
			t.Fatalf("message %q and remedy %q must not mention %q", message, remedy, banned)
		}
	}
	if got := len(runner.Invocations()); got != 0 {
		t.Fatalf("invocations = %d, want none before the artifact is known", got)
	}
}

func TestIosDriver_ClearAppData_Simulator_UninstallThenReinstallInOrder(t *testing.T) {
	runner := process.NewFake()
	d := iosdriver.New(newFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.ClearAppData(context.Background(), "dev.atomyx.app", driver.ClearAppDataOptions{Source: "/tmp/App.app"}); err != nil {
		t.Fatalf("ClearAppData() error = %v", err)
	}
	invocations := runner.Invocations()
	if len(invocations) != 2 {
		t.Fatalf("invocations = %+v, want uninstall then install", invocations)
	}
	wantUninstall := []string{"simctl", "uninstall", "SIM-1", "dev.atomyx.app"}
	wantInstall := []string{"simctl", "install", "SIM-1", "/tmp/App.app"}
	if !equalArgs(invocations[0].Args, wantUninstall) {
		t.Fatalf("first invocation args = %v, want %v", invocations[0].Args, wantUninstall)
	}
	if !equalArgs(invocations[1].Args, wantInstall) {
		t.Fatalf("second invocation args = %v, want %v", invocations[1].Args, wantInstall)
	}
}

func TestIosDriver_ClearAppData_Simulator_ToleratesNotInstalledThenInstalls(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(
		process.ExactMatch("xcrun", "simctl", "uninstall", "SIM-1", "dev.atomyx.app"),
		process.Result{Stderr: "Failed to find app"},
		errors.New("exit status 3"),
	)
	d := iosdriver.New(newFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.ClearAppData(context.Background(), "dev.atomyx.app", driver.ClearAppDataOptions{Source: "/tmp/App.app"}); err != nil {
		t.Fatalf("ClearAppData() error = %v, want the not-installed uninstall failure tolerated", err)
	}
	invocations := runner.Invocations()
	if len(invocations) != 2 || invocations[1].Name != "xcrun" {
		t.Fatalf("invocations = %+v, want uninstall then install despite the uninstall failure", invocations)
	}
}

func TestIosDriver_ClearAppData_GenuineUninstallFailurePropagates(t *testing.T) {
	runner := process.NewFake()
	runner.DefaultErr = errors.New("simctl daemon unreachable")
	d := iosdriver.New(newFakeConn(), runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	err := d.ClearAppData(context.Background(), "dev.atomyx.app", driver.ClearAppDataOptions{Source: "/tmp/App.app"})
	if err == nil {
		t.Fatal("ClearAppData() error = nil, want the genuine uninstall failure surfaced")
	}
	if len(runner.Invocations()) != 1 {
		t.Fatalf("invocations = %+v, want install skipped after a genuine uninstall failure", runner.Invocations())
	}
}

func TestIosDriver_Hierarchy_Simulator_QueriesRunningBundleIdsWhenNoneTracked(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(
		process.ExactMatch("xcrun", "simctl", "spawn", "SIM-1", "launchctl", "list"),
		process.Result{Stdout: "PID\tStatus\tLabel\n123\t0\tUIKitApplication:dev.atomyx.app[abcd][100]\n"},
		nil,
	)
	conn := newFakeConn()
	dumpRawTreeCalls := 0
	conn.Respond("dumpRawTree", func(args map[string]any) (map[string]any, error) {
		dumpRawTreeCalls++
		if dumpRawTreeCalls == 1 {
			candidates, _ := args["bundleIdCandidates"].([]string)
			if len(candidates) != 1 || candidates[0] != "dev.atomyx.app" {
				t.Fatalf("dumpRawTree args = %+v, want bundleIdCandidates=[dev.atomyx.app]", args)
			}
		} else if _, present := args["bundleIdCandidates"]; present {
			t.Fatalf("dumpRawTree args = %+v, want no bundleIdCandidates once an app is tracked", args)
		}
		return map[string]any{"bundleId": "dev.atomyx.app", "root": map[string]any{"elementType": "window"}}, nil
	})
	d := iosdriver.New(conn, runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	tree, err := d.Hierarchy(context.Background())
	if err != nil {
		t.Fatalf("Hierarchy() error = %v", err)
	}
	if tree.Attributes["class"] != "window" {
		t.Fatalf("Hierarchy() = %+v, want the decoded root", tree)
	}

	// A second Hierarchy call must not repeat the launchctl query: the
	// bundle id decoded from the first dumpRawTree response is now
	// tracked internally, which is the same tracking
	// TestIosDriver_Hierarchy_SkipsBundleQueryOnceAppIsTracked verifies
	// when it is seeded through LaunchApp instead.
	if _, err := d.Hierarchy(context.Background()); err != nil {
		t.Fatalf("second Hierarchy() error = %v", err)
	}
	if len(runner.Invocations()) != 1 {
		t.Fatalf("invocations = %+v, want the launchctl query to run only once", runner.Invocations())
	}
}

func TestIosDriver_Hierarchy_SkipsBundleQueryOnceAppIsTracked(t *testing.T) {
	runner := process.NewFake()
	conn := newFakeConn()
	conn.Respond("dumpRawTree", func(args map[string]any) (map[string]any, error) {
		if _, present := args["bundleIdCandidates"]; present {
			t.Fatalf("dumpRawTree args = %+v, want no bundleIdCandidates once an app is tracked", args)
		}
		return map[string]any{"root": map[string]any{"elementType": "window"}}, nil
	})
	d := iosdriver.New(conn, runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
	if err := d.LaunchApp(context.Background(), "dev.atomyx.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp() error = %v", err)
	}

	if _, err := d.Hierarchy(context.Background()); err != nil {
		t.Fatalf("Hierarchy() error = %v", err)
	}
	if len(runner.Invocations()) != 0 {
		t.Fatalf("invocations = %+v, want no launchctl query once an app is tracked", runner.Invocations())
	}
}

func TestIosDriver_Hierarchy_Device_PassesInstalledBundleIdsWhenNoneTracked(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(
		process.ExactMatch("xcrun", "devicectl", "device", "info", "apps", "--device", "DEV-1", "--include-default-apps", "--json-output", "-"),
		process.Result{Stdout: `{"result":{"apps":[{"bundleIdentifier":"dev.atomyx.demo","name":"Demo"},{"bundleIdentifier":"com.apple.Preferences","name":"Settings"}]}}`},
		nil,
	)
	conn := newFakeConn()
	dumpRawTreeCalls := 0
	conn.Respond("dumpRawTree", func(args map[string]any) (map[string]any, error) {
		dumpRawTreeCalls++
		if dumpRawTreeCalls == 1 {
			candidates, _ := args["bundleIdCandidates"].([]string)
			if len(candidates) != 2 || candidates[0] != "com.apple.Preferences" || candidates[1] != "dev.atomyx.demo" {
				t.Fatalf("dumpRawTree args = %+v, want the installed bundle ids as candidates", args)
			}
		} else if _, present := args["bundleIdCandidates"]; present {
			t.Fatalf("dumpRawTree args = %+v, want no bundleIdCandidates once an app is tracked", args)
		}
		return map[string]any{"bundleId": "dev.atomyx.demo", "root": map[string]any{"elementType": "window"}}, nil
	})
	d := iosdriver.New(conn, runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "DEV-1", Kind: iosdriver.KindDevice})

	for i := 0; i < 2; i++ {
		if _, err := d.Hierarchy(context.Background()); err != nil {
			t.Fatalf("Hierarchy() call %d error = %v", i+1, err)
		}
	}
	if len(runner.Invocations()) != 1 {
		t.Fatalf("invocations = %+v, want the installed app query to run only once", runner.Invocations())
	}
}

func TestIosDriver_Hierarchy_Device_ProceedsWithoutCandidatesWhenAppQueryFails(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(
		process.ExactMatch("xcrun", "devicectl", "device", "info", "apps", "--device", "DEV-1", "--include-default-apps", "--json-output", "-"),
		process.Result{},
		errors.New("device unreachable"),
	)
	conn := newFakeConn()
	conn.Respond("dumpRawTree", func(args map[string]any) (map[string]any, error) {
		if _, present := args["bundleIdCandidates"]; present {
			t.Fatalf("dumpRawTree args = %+v, want no bundleIdCandidates after a failed app query", args)
		}
		return map[string]any{"root": map[string]any{"elementType": "window"}}, nil
	})
	d := iosdriver.New(conn, runner, iosdriver.Options{Background: process.NewFakeBackground(), UDID: "DEV-1", Kind: iosdriver.KindDevice})

	if _, err := d.Hierarchy(context.Background()); err != nil {
		t.Fatalf("Hierarchy() error = %v", err)
	}
}

func TestIosDriver_Screenshot_DecodesBase64Payload(t *testing.T) {
	conn := newFakeConn()
	conn.Respond("screenshot", func(map[string]any) (map[string]any, error) {
		return map[string]any{"base64": base64.StdEncoding.EncodeToString([]byte("hello"))}, nil
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	got, err := d.Screenshot(context.Background())
	if err != nil {
		t.Fatalf("Screenshot() error = %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("Screenshot() = %q, want %q", got, "hello")
	}
}

func TestIosDriver_ScreenSize_DecodesWidthAndHeight(t *testing.T) {
	conn := newFakeConn()
	conn.Respond("getScreenSize", func(map[string]any) (map[string]any, error) {
		return map[string]any{"width": 390.0, "height": 844.0}, nil
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	size, err := d.ScreenSize(context.Background())
	if err != nil {
		t.Fatalf("ScreenSize() error = %v", err)
	}
	if want := (wire.Size{Width: 390, Height: 844}); size != want {
		t.Fatalf("ScreenSize() = %+v, want %+v", size, want)
	}
}

func TestIosDriver_DeviceInfo_ReportsKindFromOptions(t *testing.T) {
	sim := iosdriver.New(newFakeConn(), process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
	info, err := sim.DeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("DeviceInfo() error = %v", err)
	}
	if info.Kind != driver.DeviceKindSimulator || info.UDID != "SIM-1" {
		t.Fatalf("DeviceInfo() = %+v, want kind simulator and udid SIM-1", info)
	}

	dev := iosdriver.New(newFakeConn(), process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "DEV-1", Kind: iosdriver.KindDevice})
	infoDev, err := dev.DeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("DeviceInfo() error = %v", err)
	}
	if infoDev.Kind != driver.DeviceKindDevice {
		t.Fatalf("DeviceInfo() Kind = %v, want device", infoDev.Kind)
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

func connectedDriverWithRunnerID(t *testing.T, runnerID string) *iosdriver.IosDriver {
	t.Helper()
	d, _ := connectedDriverAndConn(t, runnerID)
	return d
}

func connectedDriverAndConn(t *testing.T, runnerID string) (*iosdriver.IosDriver, *fakeConn) {
	t.Helper()
	conn := newDefaultFakeConn()
	conn.Respond("ping", func(map[string]any) (map[string]any, error) {
		return map[string]any{"protocolVersion": version.AgentProtocolVersion, "capabilities": map[string]any{}, "runnerId": runnerID}, nil
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	return d, conn
}

func launchCalls(conn *fakeConn) int {
	n := 0
	for _, c := range conn.Calls() {
		if c.cmdType == "launchApp" {
			n++
		}
	}
	return n
}

func TestIosDriver_LaunchedApp_ReportsTheTrackedAppAndTheRunnerItConnectedTo(t *testing.T) {
	d := connectedDriverWithRunnerID(t, "runner-A")
	if err := d.LaunchApp(context.Background(), "com.example.app", driver.LaunchArgs{}); err != nil {
		t.Fatal(err)
	}

	bundle, runnerID := d.LaunchedApp()

	if bundle != "com.example.app" || runnerID != "runner-A" {
		t.Fatalf("LaunchedApp() = %q, %q, want com.example.app and runner-A", bundle, runnerID)
	}
}

func TestIosDriver_RestoreLaunchedApp_SeedsTheSameRunnerAndRelaunchesThroughAnyOther(t *testing.T) {
	ctx := context.Background()

	same, sameConn := connectedDriverAndConn(t, "runner-A")
	if err := same.RestoreLaunchedApp(ctx, "com.example.app", "runner-A"); err != nil {
		t.Fatal(err)
	}
	if bundle, _ := same.LaunchedApp(); bundle != "com.example.app" {
		t.Fatalf("tracked bundle after restoring on the same runner = %q, want com.example.app", bundle)
	}
	if got := launchCalls(sameConn); got != 0 {
		t.Fatalf("launchApp calls on the same runner = %d, want none because the runner already targets the app", got)
	}

	other, otherConn := connectedDriverAndConn(t, "runner-B")
	if err := other.RestoreLaunchedApp(ctx, "com.example.app", "runner-A"); err != nil {
		t.Fatal(err)
	}
	if got := launchCalls(otherConn); got != 1 {
		t.Fatalf("launchApp calls on a different runner = %d, want 1 so the new runner targets the app", got)
	}
	if bundle, _ := other.LaunchedApp(); bundle != "com.example.app" {
		t.Fatalf("tracked bundle after relaunching = %q, want com.example.app", bundle)
	}

	none, noneConn := connectedDriverAndConn(t, "")
	if err := none.RestoreLaunchedApp(ctx, "", "runner-A"); err != nil || launchCalls(noneConn) != 0 {
		t.Fatalf("restoring an empty bundle: err = %v launches = %d, want no error and no launch", err, launchCalls(noneConn))
	}
}

func TestIosDriver_InputText_UnfocusedFieldIsRejectedWithReason(t *testing.T) {
	conn := newFakeConn()
	conn.Respond("typeText", func(map[string]any) (map[string]any, error) {
		return map[string]any{"success": false, "typed": 0, "total": 5, "reason": "no_focused_field"}, nil
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	err := d.InputText(context.Background(), "hello")
	if err == nil {
		t.Fatal("InputText() error = nil, want a rejection")
	}
	code, message, remedy := diag.Describe(err)
	if code != diag.CodeInputNotFocused {
		t.Fatalf("code = %q, want %q", code, diag.CodeInputNotFocused)
	}
	if message != "nothing was typed because no input field has keyboard focus" {
		t.Fatalf("message = %q, want the focus message without the wire token", message)
	}
	if remedy != "Tap the input field so it has focus, then type again." {
		t.Fatalf("remedy = %q, want the tool-neutral remedy", remedy)
	}
}

func TestIosDriver_KeyboardFocusLostRunnerErrorIsClassifiedAsInputNotFocused(t *testing.T) {
	focusLost := &transport.AgentError{Message: "Failed to synthesize event: Neither element nor any descendant has keyboard focus"}
	for _, command := range []string{"typeText", "clearFocusedInput"} {
		conn := newFakeConn()
		conn.Respond(command, func(map[string]any) (map[string]any, error) { return nil, focusLost })
		d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

		var err error
		if command == "typeText" {
			err = d.InputText(context.Background(), "hello")
		} else {
			err = d.EraseText(context.Background(), 5)
		}
		if code, _, _ := diag.Describe(err); code != diag.CodeInputNotFocused {
			t.Errorf("%s: code = %q, want %q", command, code, diag.CodeInputNotFocused)
		}
	}
}

func TestIosDriver_UnrelatedRunnerErrorStaysAgentRejected(t *testing.T) {
	conn := newFakeConn()
	conn.Respond("typeText", func(map[string]any) (map[string]any, error) {
		return nil, &transport.AgentError{Message: "element is not hittable"}
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if code, _, _ := diag.Describe(d.InputText(context.Background(), "hello")); code != diag.CodeAgentRejected {
		t.Fatalf("code = %q, want %q", code, diag.CodeAgentRejected)
	}
}

func TestIosDriver_LaunchApp_DeliversArgumentsAsEnvironmentAndDefaults(t *testing.T) {
	conn := newFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	args := driver.LaunchArgs{
		Arguments:   map[string]string{"route": "/focus", "mode": "demo"},
		Args:        []string{"-verbose"},
		Environment: map[string]string{"mode": "override", "TRACE": "1"},
	}
	if err := d.LaunchApp(context.Background(), "dev.atomyx.app", args); err != nil {
		t.Fatalf("LaunchApp() error = %v", err)
	}
	got := conn.Calls()[0].args
	wantArguments := []string{"-mode", "demo", "-route", "/focus", "-verbose"}
	if !reflect.DeepEqual(got["launchArguments"], wantArguments) {
		t.Fatalf("launchArguments = %#v, want %#v", got["launchArguments"], wantArguments)
	}
	wantEnvironment := map[string]string{"mode": "override", "route": "/focus", "TRACE": "1"}
	if !reflect.DeepEqual(got["launchEnvironment"], wantEnvironment) {
		t.Fatalf("launchEnvironment = %#v, want %#v", got["launchEnvironment"], wantEnvironment)
	}
}

func TestIosDriver_LaunchApp_PlainLaunchOmitsLaunchInputs(t *testing.T) {
	conn := newFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.LaunchApp(context.Background(), "dev.atomyx.app", driver.LaunchArgs{}); err != nil {
		t.Fatalf("LaunchApp() error = %v", err)
	}
	got := conn.Calls()[0].args
	if _, ok := got["launchArguments"]; ok {
		t.Fatalf("launchArguments present in %+v, want it omitted", got)
	}
	if _, ok := got["launchEnvironment"]; ok {
		t.Fatalf("launchEnvironment present in %+v, want it omitted", got)
	}
}

func TestIosDriver_LaunchApp_RejectsInvalidArgumentsBeforeTheDevice(t *testing.T) {
	conn := newFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	err := d.LaunchApp(context.Background(), "dev.atomyx.app", driver.LaunchArgs{Arguments: map[string]string{"a=b": "x"}})
	if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
		t.Fatalf("LaunchApp() error code = %q (err %v), want %q", code, err, diag.CodeInvalidArgument)
	}
	if len(conn.Calls()) != 0 {
		t.Fatalf("calls = %+v, want none", conn.Calls())
	}
}

func TestIosDriver_OpenLink_SendsTheLinkAndTracksTheNamedApp(t *testing.T) {
	conn := newDefaultFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	link := "atomyxdemo://open/focus?a=1&b=two words"
	if err := d.OpenLink(context.Background(), link, driver.OpenLinkOptions{AppID: "dev.atomyx.demo"}); err != nil {
		t.Fatalf("OpenLink() error = %v", err)
	}
	calls := conn.Calls()
	if len(calls) != 1 || calls[0].cmdType != "openLink" {
		t.Fatalf("calls = %+v, want one openLink", calls)
	}
	want := map[string]any{"url": link, "bundleId": "dev.atomyx.demo"}
	if !reflect.DeepEqual(calls[0].args, want) {
		t.Fatalf("openLink args = %#v, want %#v", calls[0].args, want)
	}
}

func TestIosDriver_OpenLink_WithoutAnAppOmitsTheBundleId(t *testing.T) {
	conn := newDefaultFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.OpenLink(context.Background(), "https://example.com", driver.OpenLinkOptions{}); err != nil {
		t.Fatalf("OpenLink() error = %v", err)
	}
	if _, ok := conn.Calls()[0].args["bundleId"]; ok {
		t.Fatalf("args = %+v, want no bundleId", conn.Calls()[0].args)
	}
}

func TestIosDriver_OpenLink_OldOsIsReportedAsUnsupported(t *testing.T) {
	conn := newDefaultFakeConn()
	conn.Respond("openLink", func(map[string]any) (map[string]any, error) {
		return map[string]any{"opened": false, "reason": "unsupported-os"}, nil
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	err := d.OpenLink(context.Background(), "https://example.com", driver.OpenLinkOptions{})
	if code, _, _ := diag.Describe(err); code != diag.CodeLinkUnsupported {
		t.Fatalf("OpenLink() code = %q (err %v), want %q", code, err, diag.CodeLinkUnsupported)
	}
}

func TestIosDriver_OpenLink_RefusalWithoutAReasonIsLinkNotOpened(t *testing.T) {
	conn := newDefaultFakeConn()
	conn.Respond("openLink", func(map[string]any) (map[string]any, error) {
		return map[string]any{"opened": false}, nil
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	err := d.OpenLink(context.Background(), "https://example.com", driver.OpenLinkOptions{})
	if code, _, _ := diag.Describe(err); code != diag.CodeLinkNotOpened {
		t.Fatalf("OpenLink() code = %q (err %v), want %q", code, err, diag.CodeLinkNotOpened)
	}
}

func TestIosDriver_OpenLink_RejectsAnInvalidLinkBeforeTheDevice(t *testing.T) {
	conn := newDefaultFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	err := d.OpenLink(context.Background(), "no scheme here", driver.OpenLinkOptions{})
	if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
		t.Fatalf("OpenLink() code = %q (err %v), want %q", code, err, diag.CodeInvalidArgument)
	}
	if len(conn.Calls()) != 0 {
		t.Fatalf("calls = %+v, want none", conn.Calls())
	}
}

func TestIosDriver_SetOrientation_SendsTheNameThenWaitsForTheRotatedSize(t *testing.T) {
	conn := newDefaultFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	if err := d.SetOrientation(context.Background(), driver.OrientationLandscapeRight); err != nil {
		t.Fatalf("SetOrientation() error = %v", err)
	}
	calls := conn.Calls()
	if len(calls) < 2 || calls[0].cmdType != "setOrientation" || calls[1].cmdType != "getScreenSize" {
		t.Fatalf("calls = %+v, want setOrientation then getScreenSize", calls)
	}
	if want := map[string]any{"orientation": "landscapeRight"}; !reflect.DeepEqual(calls[0].args, want) {
		t.Fatalf("setOrientation args = %#v, want %#v", calls[0].args, want)
	}
}

func TestIosDriver_SetOrientation_RejectsAnUnknownNameBeforeTheRunner(t *testing.T) {
	conn := newDefaultFakeConn()
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})

	err := d.SetOrientation(context.Background(), "sideways")
	if code, _, _ := diag.Describe(err); code != diag.CodeInvalidArgument {
		t.Fatalf("SetOrientation() code = %q (err %v), want %q", code, err, diag.CodeInvalidArgument)
	}
	if calls := conn.Calls(); len(calls) != 0 {
		t.Fatalf("calls = %+v, want none", calls)
	}
}

func TestIosDriver_SetOrientation_AnAppThatKeepsItsLayoutIsOrientationNotSet(t *testing.T) {
	conn := newDefaultFakeConn()
	conn.Respond("setOrientation", func(map[string]any) (map[string]any, error) {
		return map[string]any{}, nil
	})
	fakeClock := clock.NewFakeClock(0)
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator, Clock: fakeClock})

	err := d.SetOrientation(context.Background(), driver.OrientationLandscapeLeft)
	if code, _, _ := diag.Describe(err); code != diag.CodeOrientationNotSet {
		t.Fatalf("SetOrientation() code = %q (err %v), want %q", code, err, diag.CodeOrientationNotSet)
	}
}
