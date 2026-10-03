package mcp

import (
	"context"
	"testing"

	"dev.atomyx/internal/session"
)

func (f *lifecycleFixture) holderStateOf(ds *DeviceSession) session.HolderState {
	f.t.Helper()
	ds.mu.Lock()
	sess := ds.sess
	ds.mu.Unlock()
	if sess == nil {
		f.t.Fatal("no session is bound")
	}
	return sess.Info().State
}

func TestDeviceSession_ASelectedDeviceReportsIdleOnceOpen(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}

	if got := f.holderStateOf(f.devices); got != session.StateIdle {
		t.Fatalf("holder state = %q, want idle once select_device finished", got)
	}
}

func TestDeviceSession_ASelectInsideACallIsRunningUntilTheCallEnds(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.devices.BeginCall("select_device"); err != nil {
		t.Fatal(err)
	}
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	if got := f.holderStateOf(f.devices); got != session.StateRunning {
		t.Fatalf("holder state = %q, want running while the call that opened it is in flight", got)
	}

	f.devices.EndCall()

	if got := f.holderStateOf(f.devices); got != session.StateIdle {
		t.Fatalf("holder state = %q, want idle after the call ended", got)
	}
}

func TestDeviceSession_AReacquiredDeviceReportsIdleOnceTheCallEnds(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs)
	f.awaitRelease(f.devices)
	if err := f.devices.BeginCall("tap"); err != nil {
		t.Fatal(err)
	}

	if _, err := f.devices.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.holderStateOf(f.devices); got != session.StateRunning {
		t.Fatalf("holder state = %q, want running while the call that re-acquired it is in flight", got)
	}
	f.devices.EndCall()

	if got := f.holderStateOf(f.devices); got != session.StateIdle {
		t.Fatalf("holder state = %q, want idle after the call ended", got)
	}
}

func TestDeviceSession_AnOpenRunWithNoCallInFlightReportsRunning(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.clk.AwaitSleepers(1)
	if err := f.devices.BeginCall("start_run"); err != nil {
		t.Fatal(err)
	}
	f.startRun()
	f.devices.EndCall()

	if got := f.holderStateOf(f.devices); got != session.StateRunning {
		t.Fatalf("holder state = %q, want running while a run is open", got)
	}
}
