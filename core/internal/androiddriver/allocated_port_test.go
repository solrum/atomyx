package androiddriver_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

// transportRecorder is an Options.NewTransport stand-in that records
// every host port a driver asked a transport for, and hands out a
// per-port fake so a test can fail the agent on one port only.
type transportRecorder struct {
	mu     sync.Mutex
	ports  []int
	byPort map[int]*fakeTransport
}

func newTransportRecorder() *transportRecorder {
	return &transportRecorder{byPort: make(map[int]*fakeTransport)}
}

func (r *transportRecorder) build(port int) androiddriver.Transport {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ports = append(r.ports, port)
	if tr, ok := r.byPort[port]; ok {
		return tr
	}
	tr := newFakeTransport()
	r.byPort[port] = tr
	return tr
}

func (r *transportRecorder) requested() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.ports...)
}

func allocatingDriver(serial string, runner process.Runner, rec *transportRecorder) *androiddriver.AndroidDriver {
	return androiddriver.New(nil, runner, androiddriver.Options{
		Serial:           serial,
		AllocateHostPort: true,
		DevicePort:       8765,
		NewTransport:     rec.build,
	})
}

func countInvocations(runner *process.Fake, args ...string) int {
	n := 0
	for _, inv := range runner.Invocations() {
		if inv.Name == "adb" && reflect.DeepEqual(inv.Args, args) {
			n++
		}
	}
	return n
}

func TestAndroidDriver_AllocateHostPort_TwoSerialsGetDistinctForwards(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "forward", "tcp:0", "tcp:8765"), process.Result{Stdout: "41001\n"}, nil)
	runner.Stub(process.ExactMatch("adb", "-s", "S2", "forward", "tcp:0", "tcp:8765"), process.Result{Stdout: "41002\n"}, nil)
	rec := newTransportRecorder()

	d1 := allocatingDriver("S1", runner, rec)
	d2 := allocatingDriver("S2", runner, rec)
	if err := d1.Connect(context.Background()); err != nil {
		t.Fatalf("S1 Connect() error = %v", err)
	}
	if err := d2.Connect(context.Background()); err != nil {
		t.Fatalf("S2 Connect() error = %v", err)
	}

	if got, want := rec.requested(), []int{41001, 41002}; !reflect.DeepEqual(got, want) {
		t.Fatalf("transport host ports = %v, want %v", got, want)
	}

	if err := d1.Disconnect(); err != nil {
		t.Fatalf("S1 Disconnect() error = %v", err)
	}
	if countInvocations(runner, "-s", "S1", "forward", "--remove", "tcp:41001") != 1 {
		t.Fatalf("adb invocations = %+v, want S1's own allocated forward removed", runner.Invocations())
	}
	if countInvocations(runner, "-s", "S2", "forward", "--remove", "tcp:41002") != 0 {
		t.Fatalf("adb invocations = %+v, want S2's forward untouched by S1's Disconnect", runner.Invocations())
	}
}

func TestAndroidDriver_AllocateHostPort_IgnoresFixedHostPort(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "forward", "tcp:0", "tcp:8765"), process.Result{Stdout: "41001\n"}, nil)
	rec := newTransportRecorder()
	d := androiddriver.New(nil, runner, androiddriver.Options{
		Serial: "S1", HostPort: 9001, AllocateHostPort: true, DevicePort: 8765, NewTransport: rec.build,
	})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if countInvocations(runner, "-s", "S1", "forward", "tcp:9001", "tcp:8765") != 0 {
		t.Fatalf("adb invocations = %+v, want HostPort ignored when AllocateHostPort is set", runner.Invocations())
	}
	if got := rec.requested(); !reflect.DeepEqual(got, []int{41001}) {
		t.Fatalf("transport host ports = %v, want [41001]", got)
	}
}

func TestAndroidDriver_AllocateHostPort_ExistingForwardForSameSerial_IsReused(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "forward", "--list"),
		process.Result{Stdout: "S2 tcp:41002 tcp:8765\nS1 tcp:41001 tcp:8765\n"}, nil)
	rec := newTransportRecorder()
	d := allocatingDriver("S1", runner, rec)

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if countInvocations(runner, "-s", "S1", "forward", "tcp:0", "tcp:8765") != 0 {
		t.Fatalf("adb invocations = %+v, want no new forward for a serial that already has one", runner.Invocations())
	}
	if got := rec.requested(); !reflect.DeepEqual(got, []int{41001}) {
		t.Fatalf("transport host ports = %v, want the reused port [41001]", got)
	}
	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if countInvocations(runner, "-s", "S1", "forward", "--remove", "tcp:41001") != 0 {
		t.Fatalf("adb invocations = %+v, want a reused forward left in place", runner.Invocations())
	}
}

func TestAndroidDriver_AllocateHostPort_OtherSerialsForward_DoesNotBlockAllocation(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "forward", "--list"),
		process.Result{Stdout: "S2 tcp:41001 tcp:8765\n"}, nil)
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "forward", "tcp:0", "tcp:8765"), process.Result{Stdout: "41009\n"}, nil)
	rec := newTransportRecorder()
	d := allocatingDriver("S1", runner, rec)

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if got := rec.requested(); !reflect.DeepEqual(got, []int{41009}) {
		t.Fatalf("transport host ports = %v, want [41009]", got)
	}
}

func TestAndroidDriver_AllocateHostPort_MalformedAdbOutput_IsTypedError(t *testing.T) {
	for _, stdout := range []string{"", "\n", "not-a-port", "0", "-5", "70000", "41001 extra"} {
		runner := process.NewFake()
		runner.Stub(process.ExactMatch("adb", "-s", "S1", "forward", "tcp:0", "tcp:8765"), process.Result{Stdout: stdout}, nil)
		rec := newTransportRecorder()
		d := allocatingDriver("S1", runner, rec)

		err := d.Connect(context.Background())
		if err == nil {
			t.Fatalf("stdout %q: Connect() error = nil, want a typed error", stdout)
		}
		if code, _, _ := diag.Describe(err); code != diag.CodeAgentUnreachable {
			t.Fatalf("stdout %q: Connect() code = %q, want %q", stdout, code, diag.CodeAgentUnreachable)
		}
		if d.IsConnected() {
			t.Fatalf("stdout %q: IsConnected() = true after a malformed allocation", stdout)
		}
		if len(rec.requested()) != 0 {
			t.Fatalf("stdout %q: a transport was built for a port that was never parsed", stdout)
		}
	}
}

func TestAndroidDriver_AllocateHostPort_StaleForward_IsRemovedAndReallocated(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "forward", "--list"),
		process.Result{Stdout: "S1 tcp:41001 tcp:8765\n"}, nil)
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "forward", "tcp:0", "tcp:8765"), process.Result{Stdout: "41005\n"}, nil)
	rec := newTransportRecorder()
	rec.byPort[41001] = newFakeTransport()
	rec.byPort[41001].healthErr = errors.New("connection refused")
	d := allocatingDriver("S1", runner, rec)

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v, want recovery by reallocating the dead forward", err)
	}
	if got, want := rec.requested(), []int{41001, 41005}; !reflect.DeepEqual(got, want) {
		t.Fatalf("transport host ports = %v, want %v", got, want)
	}
	if countInvocations(runner, "-s", "S1", "forward", "--remove", "tcp:41001") != 1 {
		t.Fatalf("adb invocations = %+v, want the dead forward removed", runner.Invocations())
	}
	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if countInvocations(runner, "-s", "S1", "forward", "--remove", "tcp:41005") != 1 {
		t.Fatalf("adb invocations = %+v, want the reallocated forward removed on Disconnect", runner.Invocations())
	}
}

func TestAndroidDriver_AllocateHostPort_DroppedForwardAfterKillServer_AllocatesOnNextConnect(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "-s", "S1", "forward", "tcp:0", "tcp:8765"), process.Result{Stdout: "41001\n"}, nil)
	rec := newTransportRecorder()
	d := allocatingDriver("S1", runner, rec)
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("first Connect() error = %v", err)
	}

	// adb kill-server dropped every forward: the list is empty again.
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("second Connect() error = %v", err)
	}
	if n := countInvocations(runner, "-s", "S1", "forward", "tcp:0", "tcp:8765"); n != 2 {
		t.Fatalf("tcp:0 forwards = %d, want 2 (one per Connect)", n)
	}
}

func TestAndroidDriver_AllocateHostPort_ReusedForwardWithNotReadyAgent_IsNotReallocated(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "forward", "--list"),
		process.Result{Stdout: "S1 tcp:41001 tcp:8765\n"}, nil)
	rec := newTransportRecorder()
	rec.byPort[41001] = newFakeTransport()
	rec.byPort[41001].healthErr = &transport.AgentError{Route: "/health", StatusCode: 503, Message: "accessibility service not connected"}
	d := allocatingDriver("S1", runner, rec)

	err := d.Connect(context.Background())
	if code, _, _ := diag.Describe(err); code != diag.CodeAgentNotReady {
		t.Fatalf("Connect() code = %q, want %q", code, diag.CodeAgentNotReady)
	}
	if countInvocations(runner, "-s", "S1", "forward", "tcp:0", "tcp:8765") != 0 {
		t.Fatalf("adb invocations = %+v, want a live forward with a not-ready agent left alone", runner.Invocations())
	}
}

func TestAndroidDriver_AllocateHostPort_WithoutTransportConstructor_IsTypedError(t *testing.T) {
	d := androiddriver.New(nil, process.NewFake(), androiddriver.Options{Serial: "S1", AllocateHostPort: true})

	err := d.Connect(context.Background())
	if code, _, _ := diag.Describe(err); code != diag.CodeAgentUnreachable {
		t.Fatalf("Connect() code = %q, want %q", code, diag.CodeAgentUnreachable)
	}
}

func ownedForwardDriver(runner process.Runner, rec *transportRecorder, own bool) *androiddriver.AndroidDriver {
	return androiddriver.New(nil, runner, androiddriver.Options{
		Serial: "S1", AllocateHostPort: true, DevicePort: 8765, NewTransport: rec.build,
		OwnAnsweringForward: own,
	})
}

func TestAndroidDriver_OwnAnsweringForward_ReusedForwardWithAnsweringAgent_IsRemovedOnDisconnect(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "forward", "--list"),
		process.Result{Stdout: "S1 tcp:41001 tcp:8765\n"}, nil)
	d := ownedForwardDriver(runner, newTransportRecorder(), true)

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if countInvocations(runner, "-s", "S1", "forward", "--remove", "tcp:41001") != 1 {
		t.Fatalf("adb invocations = %+v, want the reused forward removed once", runner.Invocations())
	}
}

func TestAndroidDriver_OwnAnsweringForward_ReusedForwardWithSilentAgent_IsNeverRemoved(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "forward", "--list"),
		process.Result{Stdout: "S1 tcp:41001 tcp:8765\n"}, nil)
	rec := newTransportRecorder()
	rec.byPort[41001] = newFakeTransport()
	rec.byPort[41001].healthErr = &transport.AgentError{Route: "/health", StatusCode: 503, Message: "accessibility service not connected"}
	d := ownedForwardDriver(runner, rec, true)

	if err := d.Connect(context.Background()); err == nil {
		t.Fatal("Connect() error = nil, want the not-ready agent reported")
	}
	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if countInvocations(runner, "-s", "S1", "forward", "--remove", "tcp:41001") != 0 {
		t.Fatalf("adb invocations = %+v, want a forward the agent never answered through left in place", runner.Invocations())
	}
}

func TestAndroidDriver_OwnAnsweringForward_Off_ReusedForwardWithAnsweringAgent_IsLeftInPlace(t *testing.T) {
	runner := process.NewFake()
	runner.Stub(process.ExactMatch("adb", "forward", "--list"),
		process.Result{Stdout: "S1 tcp:41001 tcp:8765\n"}, nil)
	d := ownedForwardDriver(runner, newTransportRecorder(), false)

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if countInvocations(runner, "-s", "S1", "forward", "--remove", "tcp:41001") != 0 {
		t.Fatalf("adb invocations = %+v, want the reused forward left in place", runner.Invocations())
	}
}
