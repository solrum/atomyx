package mcp

import (
	"strings"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/session/sessiontest"
)

// newDevicesWithSeam is newDevices with the yield request routed through seam.
func (f *lifecycleFixture) newDevicesWithSeam(command string, seam *sessiontest.YieldSeam) *DeviceSession {
	return NewDeviceSession(DeviceSessionDeps{
		Factory: f.factory, Probe: device.NewStaticProbe(), Clock: f.clk, TimerClock: f.clk, Logger: logging.NoopLogger{},
		Session: session.Deps{
			Leases: f.leases, Sockets: f.leases, Clock: clock.SystemClock{}, Command: command,
			RequestYield: seam.Requester(),
		},
		Timeouts: session.Timeouts{McpIdleMs: testIdleMs, OpenRunIdleMs: testOpenRunMs, DeviceAcquireMs: 120_000},
		Runs:     f.runs,
	})
}

func TestDeviceSessionBorrow_SelectTakesTheDeviceFromAnIdleSourceAndHoldsIt(t *testing.T) {
	f := newLifecycleFixture(t)
	f.selectedAndIdle()
	seam := &sessiontest.YieldSeam{}
	borrower := f.newDevicesWithSeam("atomyx-mcp", seam)
	t.Cleanup(borrower.Close)

	if err := f.selectDevice(borrower); err != nil {
		t.Fatalf("select_device on a device held by an idle source: %v", err)
	}

	if borrower.Current() == nil || !f.leaseHeld() {
		t.Fatal("the borrower does not hold the device after select_device")
	}
	if got := seam.Borrower(); got != selectBorrower {
		t.Fatalf("the source was told the borrower is %v, want %q", got, selectBorrower)
	}
	eventually(t, "the source to report borrowed", func() bool { return f.state(f.devices) == holderBorrowed })
	requireIntervention(t, f.devices.Interventions(), "controlBorrowed:"+selectBorrower)
}

func TestDeviceSessionBorrow_SelectOnABusySourceFailsWithDeviceHeldNamingIt(t *testing.T) {
	f := newLifecycleFixture(t)
	f.selectedAndIdle()
	if err := f.devices.BeginCall("tap"); err != nil {
		t.Fatal(err)
	}
	seam := &sessiontest.YieldSeam{}
	other := f.newDevicesWithSeam("atomyx-mcp", seam)
	t.Cleanup(other.Close)

	err := f.selectDevice(other)

	code, message, _ := diag.Describe(err)
	if code != diag.CodeDeviceHeld || !strings.Contains(message, "atomyx-mcp") {
		t.Fatalf("select_device on a busy source = %s %q, want DEVICE_HELD naming the holder", code, message)
	}
	if seam.Calls() != 1 {
		t.Fatalf("yield requests = %d, want the one the busy source refused", seam.Calls())
	}
	f.requireStillHeld("a busy source")
}

func TestDeviceSessionBorrow_AReacquireAfterBeingBorrowedFromNeverAsksToYield(t *testing.T) {
	f := newLifecycleFixture(t)
	seam := &sessiontest.YieldSeam{}
	source := f.newDevicesWithSeam("atomyx-mcp", seam)
	t.Cleanup(source.Close)
	if err := f.selectDevice(source); err != nil {
		t.Fatal(err)
	}
	other := f.newDevices("atomyx-mcp", f.factory)
	t.Cleanup(other.Close)
	if err := f.selectDevice(other); err != nil {
		t.Fatalf("borrow: %v", err)
	}
	eventually(t, "the source to report borrowed", func() bool { return f.state(source) == holderBorrowed })
	asked := seam.Calls()

	err := ensureErr(source)

	if got := codeOfErr(err); got != diag.CodeDeviceReleasedHeld {
		t.Fatalf("Ensure after being borrowed from = %q, want %s", got, diag.CodeDeviceReleasedHeld)
	}
	if seam.Calls() != asked {
		t.Fatal("a re-acquire sent a yield request, so two idle sessions could trade the device")
	}
	if other.Current() == nil {
		t.Fatal("the re-acquire took the device back from its borrower")
	}
}

func TestDeviceSessionBorrow_AReacquireAfterAnIdleReleaseNeverAsksTheHolderToYield(t *testing.T) {
	f := newLifecycleFixture(t)
	seam := &sessiontest.YieldSeam{}
	source := f.newDevicesWithSeam("atomyx-mcp", seam)
	t.Cleanup(source.Close)
	if err := f.selectDevice(source); err != nil {
		t.Fatal(err)
	}
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs)
	f.awaitRelease(source)
	other := f.newDevices("atomyx-mcp", f.factory)
	t.Cleanup(other.Close)
	if err := f.selectDevice(other); err != nil {
		t.Fatal(err)
	}

	err := ensureErr(source)

	if got := codeOfErr(err); got != diag.CodeDeviceReleasedHeld {
		t.Fatalf("Ensure after an idle release = %q, want %s", got, diag.CodeDeviceReleasedHeld)
	}
	if got := seam.Calls(); got != 0 {
		t.Fatalf("a re-acquire sent %d yield requests, want none", got)
	}
	if other.Current() == nil {
		t.Fatal("the re-acquire took the device from the process that holds it")
	}
}
