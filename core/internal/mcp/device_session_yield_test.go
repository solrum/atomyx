package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/session/sessiontest"
)

const (
	yieldBorrower = "atomyx run login.yml"
	yieldWait     = 5 * time.Second
	// releasedWithin bounds how long the source takes to notice that its
	// borrower ended, which must stay well under the follow budget.
	releasedWithin = time.Second
)

func (f *lifecycleFixture) requestYield() error {
	f.t.Helper()
	return sessiontest.RequestYield(context.Background(), f.holderSocket(), "atomyx run", yieldBorrower)
}

// eventually polls cond until it holds or yieldWait passes.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(yieldWait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func (f *lifecycleFixture) awaitLeaseFree() {
	f.t.Helper()
	eventually(f.t, "the lease to be released", func() bool { return !f.leaseHeld() })
}

// selectedAndIdle selects the device and returns the fixture ready to yield.
func (f *lifecycleFixture) selectedAndIdle() {
	f.t.Helper()
	if err := f.selectDevice(f.devices); err != nil {
		f.t.Fatal(err)
	}
}

func (f *lifecycleFixture) requireStillHeld(what string) {
	f.t.Helper()
	if got := f.state(f.devices); got != holderIdle && got != holderRunning {
		f.t.Fatalf("state after a refused yield during %s = %q, want the device still held", what, got)
	}
	if len(f.factory.disconnected) != 0 || !f.leaseHeld() {
		f.t.Fatalf("a refused yield during %s closed the session", what)
	}
}

func TestDeviceSessionYield_AToolCallInFlightRefusesWithHolderBusy(t *testing.T) {
	f := newLifecycleFixture(t)
	f.selectedAndIdle()
	if err := f.devices.BeginCall("tap"); err != nil {
		t.Fatal(err)
	}

	if got := codeOfErr(f.requestYield()); got != diag.CodeHolderBusy {
		t.Fatalf("yield during a call = %q, want %s", got, diag.CodeHolderBusy)
	}
	f.requireStillHeld("a tool call")

	leave, err := f.devices.EnterCall(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.devices.EndCall()
	if got := codeOfErr(f.requestYield()); got != diag.CodeHolderBusy {
		t.Fatalf("yield inside the gate = %q, want %s", got, diag.CodeHolderBusy)
	}
	leave()
}

func TestDeviceSessionYield_ACaptureInFlightRefusesWithHolderBusy(t *testing.T) {
	f := newLifecycleFixture(t)
	f.selectedAndIdle()
	endCapture, ok := f.devices.gate.TryExclusive()
	if !ok {
		t.Fatal("the gate of an idle session is not free")
	}

	got := codeOfErr(f.requestYield())
	endCapture()

	if got != diag.CodeHolderBusy {
		t.Fatalf("yield during a capture = %q, want %s", got, diag.CodeHolderBusy)
	}
	f.requireStillHeld("a capture")
}

func TestDeviceSessionYield_AnOpenRunRefusesWithHolderBusy(t *testing.T) {
	f := newLifecycleFixture(t)
	f.selectedAndIdle()
	f.startRun()

	if got := codeOfErr(f.requestYield()); got != diag.CodeHolderBusy {
		t.Fatalf("yield with a run open = %q, want %s", got, diag.CodeHolderBusy)
	}
	f.requireStillHeld("an open run")
}

func TestDeviceSessionYield_ARaceWithTheIdleTimerClosesTheSessionOnce(t *testing.T) {
	f := newLifecycleFixture(t)
	log := &closeCounter{}
	f.devices.logger = log
	f.selectedAndIdle()
	f.clk.AwaitSleepers(1)

	var wg sync.WaitGroup
	var afterReply func()
	wg.Add(2)
	go func() {
		defer wg.Done()
		f.clk.Advance(testIdleMs)
	}()
	go func() {
		defer wg.Done()
		afterReply, _ = f.devices.Yield(context.Background(), yieldBorrower)
	}()
	wg.Wait()
	if afterReply != nil {
		go afterReply()
	}
	f.devices.acquireMu.Lock()
	f.devices.acquireMu.Unlock() //nolint:staticcheck // the empty section is the barrier
	f.devices.Close()

	if got := log.closes.Load(); got != 1 {
		t.Fatalf("session closes = %d, want exactly one", got)
	}
}

func TestDeviceSessionYield_AcceptingFreesTheLeaseAndLeavesTheRunnerRunning(t *testing.T) {
	f := newLifecycleFixture(t)
	f.selectedAndIdle()

	if err := f.requestYield(); err != nil {
		t.Fatalf("yield of an idle session: %v", err)
	}
	f.awaitLeaseFree()

	if got := f.factory.runnerStops.Load(); got != 0 {
		t.Fatalf("runner stops = %d after a yield, want the runner left running", got)
	}
	if got := len(f.factory.disconnected); got != 1 {
		t.Fatalf("driver disconnects = %d, want one", got)
	}
}

func TestDeviceSessionYield_TheAgentSeesTheBorrowerAndCheckHealthReportsBorrowed(t *testing.T) {
	f := newLifecycleFixture(t)
	f.selectedAndIdle()
	borrower := f.newDevices("atomyx run", f.factory)
	t.Cleanup(borrower.Close)

	if err := f.requestYield(); err != nil {
		t.Fatal(err)
	}
	f.awaitLeaseFree()
	if err := f.selectDevice(borrower); err != nil {
		t.Fatalf("borrower select: %v", err)
	}

	requireIntervention(t, f.devices.Interventions(), "controlBorrowed:"+yieldBorrower)
	if report := f.devices.CheckHealth(context.Background()); report.State != "borrowed" || !report.Selected {
		t.Fatalf("CheckHealth() = %+v, want selected and borrowed while the borrower runs", report)
	}
	if got := codeOfErr(ensureErr(f.devices)); got != diag.CodeDeviceReleasedHeld {
		t.Fatalf("Ensure while borrowed = %q, want %s and no second borrow", got, diag.CodeDeviceReleasedHeld)
	}
}

func TestDeviceSessionYield_TheSourceReportsReleasedOnceTheBorrowerEnds(t *testing.T) {
	f := newLifecycleFixture(t)
	f.selectedAndIdle()
	borrower := f.newDevices("atomyx run", f.factory)
	t.Cleanup(borrower.Close)
	if err := f.requestYield(); err != nil {
		t.Fatal(err)
	}
	f.awaitLeaseFree()
	if err := f.selectDevice(borrower); err != nil {
		t.Fatal(err)
	}
	f.awaitBorrowerWatched(borrower)
	if got := f.state(f.devices); got != holderBorrowed {
		t.Fatalf("state while the borrower runs = %q, want borrowed", got)
	}

	started := time.Now()
	if err := borrower.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}

	eventually(t, "the source to report released", func() bool { return f.state(f.devices) == holderReleased })
	if took := time.Since(started); took > releasedWithin {
		t.Fatalf("the source reported released after %v, want under %v", took, releasedWithin)
	}
	if report := f.devices.CheckHealth(context.Background()); report.State != "released" {
		t.Fatalf("CheckHealth() = %+v, want released after the borrower ended", report)
	}
}

func TestDeviceSessionYield_ASourceThatNobodyTakesTheDeviceFromReleasesAfterTheFollowBudget(t *testing.T) {
	f := newLifecycleFixture(t)
	source := f.newDevicesPreparing("atomyx-mcp", f.factory, nil)
	t.Cleanup(source.Close)
	if err := f.selectDevice(source); err != nil {
		t.Fatal(err)
	}
	if err := f.requestYield(); err != nil {
		t.Fatal(err)
	}
	f.awaitLeaseFree()
	f.clk.AwaitSleepers(2)

	f.clk.Advance(session.FollowBudgetMs)

	eventually(t, "the source to give up waiting", func() bool { return f.state(source) == holderReleased })
}

func TestDeviceSessionYield_ACallDuringTheHandOverDoesNotTakeTheDeviceBack(t *testing.T) {
	f := newLifecycleFixture(t)
	f.selectedAndIdle()
	borrower := f.newDevices("atomyx run", f.factory)
	t.Cleanup(borrower.Close)
	if err := f.requestYield(); err != nil {
		t.Fatal(err)
	}
	f.awaitLeaseFree()

	if got := codeOfErr(ensureErr(f.devices)); got != diag.CodeDeviceReleasedHeld {
		t.Fatalf("Ensure while the hand-over is open = %q, want %s", got, diag.CodeDeviceReleasedHeld)
	}
	if f.leaseHeld() {
		t.Fatal("a call during the hand-over took the lease back")
	}
	if err := f.selectDevice(borrower); err != nil {
		t.Fatalf("borrower select after the source's call: %v", err)
	}
}

func ensureErr(ds *DeviceSession) error {
	_, err := ds.Ensure(context.Background())
	return err
}

func requireIntervention(t *testing.T, got []session.Intervention, want string) {
	t.Helper()
	for _, line := range summarize(got) {
		if line == want {
			return
		}
	}
	t.Fatalf("interventions = %v, want one to be %q", summarize(got), want)
}

// closeCounter counts the closes a DeviceSession performs, whichever path
// ends the session.
type closeCounter struct {
	logging.NoopLogger
	closes atomic.Int32
}

func (c *closeCounter) Info(msg string, _ logging.LogContext) {
	if msg == "session.released" || msg == "session.borrowed" {
		c.closes.Add(1)
	}
}

func (c *closeCounter) Child(logging.LogContext) logging.Logger { return c }

func (f *lifecycleFixture) awaitBorrowerWatched(borrower *DeviceSession) {
	f.t.Helper()
	eventually(f.t, "the source to watch the borrower", func() bool {
		for _, entry := range borrower.Interventions() {
			if entry.Kind == session.InterventionViewerAttached {
				return true
			}
		}
		return false
	})
}

func TestDeviceSessionYield_CheckHealthSaysTheDeviceWasHandedOverNotIdledOnceTheBorrowerEnds(t *testing.T) {
	f := newLifecycleFixture(t)
	f.selectedAndIdle()
	borrower := f.newDevices("atomyx run", f.factory)
	t.Cleanup(borrower.Close)
	if err := f.requestYield(); err != nil {
		t.Fatal(err)
	}
	f.awaitLeaseFree()
	if err := f.selectDevice(borrower); err != nil {
		t.Fatal(err)
	}
	f.awaitBorrowerWatched(borrower)
	if err := borrower.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the source to report released", func() bool { return f.state(f.devices) == holderReleased })

	report := f.devices.CheckHealth(context.Background())
	const want = "the session handed its device to another process that no longer holds it and has not probed it"
	if report.State != "released" || report.Message != want {
		t.Fatalf("CheckHealth() = %+v, want released with message %q", report, want)
	}
}

func TestDeviceSessionYield_CheckHealthKeepsTheIdleReasonForAnIdleRelease(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	f.clk.AwaitSleepers(1)
	f.clk.Advance(testIdleMs)
	f.awaitRelease(f.devices)

	report := f.devices.CheckHealth(context.Background())
	const want = "the session released its device after an idle timeout and has not probed it"
	if report.State != "released" || report.Message != want {
		t.Fatalf("CheckHealth() = %+v, want released with message %q", report, want)
	}
}

func TestDeviceSessionYield_EnsureWhileBorrowedNamesTheBorrowerLikeDeviceHeld(t *testing.T) {
	f := newLifecycleFixture(t)
	f.selectedAndIdle()
	borrower := f.newDevices("atomyx run", f.factory)
	t.Cleanup(borrower.Close)
	if err := f.requestYield(); err != nil {
		t.Fatal(err)
	}
	f.awaitLeaseFree()
	if err := f.selectDevice(borrower); err != nil {
		t.Fatal(err)
	}
	holder := fmt.Sprintf("pid %d (atomyx run), ", os.Getpid())
	var err error
	eventually(t, "Ensure to name the borrower", func() bool {
		err = ensureErr(f.devices)
		return err != nil && strings.Contains(err.Error(), holder)
	})
	if code := codeOfErr(err); code != diag.CodeDeviceReleasedHeld {
		t.Fatalf("Ensure while borrowed = %v, want %s", err, diag.CodeDeviceReleasedHeld)
	}
}
