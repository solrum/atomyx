package device

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"dev.atomyx/internal/iossimdriver"
)

// fakeSimHidLauncher is a device-free simHidLauncher test double. Each
// call blocks on gate (when set) so a test can observe the
// "spawning" window deterministically, then returns the staged result
// or error for that UDID.
type fakeSimHidLauncher struct {
	mu     sync.Mutex
	result map[string]iossimdriver.SimHidLaunchResult
	err    map[string]error
	calls  []string
	gate   chan struct{}
}

func newFakeSimHidLauncher() *fakeSimHidLauncher {
	return &fakeSimHidLauncher{
		result: make(map[string]iossimdriver.SimHidLaunchResult),
		err:    make(map[string]error),
	}
}

func (f *fakeSimHidLauncher) setResult(udid string, result iossimdriver.SimHidLaunchResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.result[udid] = result
}

func (f *fakeSimHidLauncher) setErr(udid string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err[udid] = err
}

func (f *fakeSimHidLauncher) setGate(gate chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gate = gate
}

func (f *fakeSimHidLauncher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeSimHidLauncher) Launch(ctx context.Context, udid string) (iossimdriver.SimHidLaunchResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, udid)
	gate := f.gate
	result := f.result[udid]
	err := f.err[udid]
	f.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return iossimdriver.SimHidLaunchResult{}, ctx.Err()
		}
	}
	return result, err
}

func TestSimHidManager_Status_UnknownUDID_ReturnsIdle(t *testing.T) {
	m := newSimHidManagerWithLauncher(newFakeSimHidLauncher())
	status := m.Status("udid-1")
	want := SimHidStatus{UDID: "udid-1", State: SimHidStateIdle}
	if status != want {
		t.Fatalf("Status() = %+v, want %+v", status, want)
	}
}

func TestSimHidManager_StartSimHid_Success_ReportsReadyWithPort(t *testing.T) {
	launcher := newFakeSimHidLauncher()
	launcher.setResult("udid-1", iossimdriver.SimHidLaunchResult{Port: 19876, Kill: func() error { return nil }})
	m := newSimHidManagerWithLauncher(launcher)

	port, err := m.StartSimHid(context.Background(), "udid-1")
	if err != nil {
		t.Fatalf("StartSimHid() error = %v", err)
	}
	if port != 19876 {
		t.Fatalf("StartSimHid() port = %d, want 19876", port)
	}

	status := m.Status("udid-1")
	want := SimHidStatus{UDID: "udid-1", State: SimHidStateReady, Port: 19876, HasPort: true}
	if status != want {
		t.Fatalf("Status() = %+v, want %+v", status, want)
	}
}

func TestSimHidManager_StartSimHid_Failure_ReportsFailedWithMessage(t *testing.T) {
	launcher := newFakeSimHidLauncher()
	launcher.setErr("udid-1", errors.New("boom"))
	m := newSimHidManagerWithLauncher(launcher)

	_, err := m.StartSimHid(context.Background(), "udid-1")
	if err == nil || err.Error() != "boom" {
		t.Fatalf("StartSimHid() error = %v, want boom", err)
	}

	status := m.Status("udid-1")
	want := SimHidStatus{UDID: "udid-1", State: SimHidStateFailed, Message: "boom"}
	if status != want {
		t.Fatalf("Status() = %+v, want %+v", status, want)
	}
}

func TestSimHidManager_StartSimHid_ConcurrentCallsShareOneAttempt(t *testing.T) {
	launcher := newFakeSimHidLauncher()
	launcher.setResult("udid-1", iossimdriver.SimHidLaunchResult{Port: 4242, Kill: func() error { return nil }})
	gate := make(chan struct{})
	launcher.setGate(gate)
	m := newSimHidManagerWithLauncher(launcher)

	type outcome struct {
		port int
		err  error
	}
	results := make(chan outcome, 3)
	for i := 0; i < 3; i++ {
		go func() {
			port, err := m.StartSimHid(context.Background(), "udid-1")
			results <- outcome{port: port, err: err}
		}()
	}

	deadline := time.After(2 * time.Second)
	for launcher.callCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("Launch was never called within timeout")
		case <-time.After(time.Millisecond):
		}
	}
	if status := m.Status("udid-1"); status.State != SimHidStateSpawning {
		t.Fatalf("Status() = %+v, want spawning while the shared attempt is gated", status)
	}
	close(gate)

	for i := 0; i < 3; i++ {
		select {
		case out := <-results:
			if out.err != nil || out.port != 4242 {
				t.Fatalf("StartSimHid() = (%d, %v), want (4242, nil)", out.port, out.err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("did not observe all three StartSimHid results within timeout")
		}
	}
	if got := launcher.callCount(); got != 1 {
		t.Fatalf("Launch call count = %d, want exactly 1 for three concurrent callers", got)
	}
}

func TestSimHidManager_StartSimHid_AfterFailure_Retries(t *testing.T) {
	launcher := newFakeSimHidLauncher()
	launcher.setErr("udid-1", errors.New("boom"))
	m := newSimHidManagerWithLauncher(launcher)

	if _, err := m.StartSimHid(context.Background(), "udid-1"); err == nil {
		t.Fatal("StartSimHid() error = nil, want boom")
	}

	launcher.setErr("udid-1", nil)
	launcher.setResult("udid-1", iossimdriver.SimHidLaunchResult{Port: 111, Kill: func() error { return nil }})
	port, err := m.StartSimHid(context.Background(), "udid-1")
	if err != nil {
		t.Fatalf("retry StartSimHid() error = %v", err)
	}
	if port != 111 {
		t.Fatalf("retry StartSimHid() port = %d, want 111", port)
	}
	if got := launcher.callCount(); got != 2 {
		t.Fatalf("Launch call count = %d, want 2 (initial failure + retry)", got)
	}
}

func TestSimHidManager_StartSimHid_CallerCtxCanceled_ReturnsCtxErrWithoutStoppingAttempt(t *testing.T) {
	launcher := newFakeSimHidLauncher()
	gate := make(chan struct{})
	launcher.setGate(gate)
	launcher.setResult("udid-1", iossimdriver.SimHidLaunchResult{Port: 555, Kill: func() error { return nil }})
	m := newSimHidManagerWithLauncher(launcher)

	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := m.StartSimHid(ctx, "udid-1")
		firstDone <- err
	}()

	deadline := time.After(2 * time.Second)
	for launcher.callCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("Launch was never called within timeout")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-firstDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("StartSimHid() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled caller did not return within timeout")
	}

	// The shared attempt is still in flight for a second caller even
	// though the first caller's own ctx was canceled.
	close(gate)
	port, err := m.StartSimHid(context.Background(), "udid-1")
	if err != nil {
		t.Fatalf("second StartSimHid() error = %v", err)
	}
	if port != 555 {
		t.Fatalf("second StartSimHid() port = %d, want 555", port)
	}
	if got := launcher.callCount(); got != 1 {
		t.Fatalf("Launch call count = %d, want exactly 1: the attempt keeps running after one caller's ctx is canceled", got)
	}
}

func TestSimHidManager_Dispose_CancelsSpawningAndKillsReady(t *testing.T) {
	launcher := newFakeSimHidLauncher()
	gate := make(chan struct{})
	launcher.setGate(gate)
	m := newSimHidManagerWithLauncher(launcher)

	spawningDone := make(chan error, 1)
	go func() {
		_, err := m.StartSimHid(context.Background(), "udid-spawning")
		spawningDone <- err
	}()
	deadline := time.After(2 * time.Second)
	for launcher.callCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("Launch was never called within timeout")
		case <-time.After(time.Millisecond):
		}
	}

	killed := false
	var killedMu sync.Mutex
	launcherReady := newFakeSimHidLauncher()
	launcherReady.setResult("udid-ready", iossimdriver.SimHidLaunchResult{
		Port: 1,
		Kill: func() error {
			killedMu.Lock()
			killed = true
			killedMu.Unlock()
			return nil
		},
	})
	mReady := newSimHidManagerWithLauncher(launcherReady)
	if _, err := mReady.StartSimHid(context.Background(), "udid-ready"); err != nil {
		t.Fatalf("StartSimHid() error = %v", err)
	}
	mReady.Dispose()
	killedMu.Lock()
	gotKilled := killed
	killedMu.Unlock()
	if !gotKilled {
		t.Fatal("Dispose() did not kill a ready helper")
	}
	if status := mReady.Status("udid-ready"); status.State != SimHidStateIdle {
		t.Fatalf("Status() after Dispose() = %+v, want idle (entry cleared)", status)
	}

	m.Dispose()
	select {
	case err := <-spawningDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("spawning StartSimHid() after Dispose() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("spawning caller did not return after Dispose() within timeout")
	}
}
