package mcp

import (
	"strings"
	"sync"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
)

var debugPause = session.PauseLocation{Source: "inline", Line: 3, Command: "tap"}

// pauseHeldSession marks the run of the fixture's held session as paused.
func (f *lifecycleFixture) pauseHeldSession() *session.Session {
	f.t.Helper()
	f.devices.mu.Lock()
	sess := f.devices.sess
	f.devices.mu.Unlock()
	if sess == nil {
		f.t.Fatal("no session is bound")
	}
	sess.EnterPause(debugPause)
	return sess
}

func TestDeviceSessionPause_AgentDeviceToolsAreRefusedWithHolderBusyUntilTheRunResumes(t *testing.T) {
	f := newLifecycleFixture(t)
	f.selectedAndIdle()
	sess := f.pauseHeldSession()

	err := f.devices.BeginCall("tap")
	if codeOfErr(err) != diag.CodeHolderBusy {
		t.Fatalf("a device tool while paused: %v, want %s", err, diag.CodeHolderBusy)
	}
	for _, internal := range []string{"Android", "iOS", "Orchestra", "XCUITest"} {
		if strings.Contains(err.Error(), internal) {
			t.Fatalf("refusal %q names %q", err, internal)
		}
	}
	if !strings.Contains(err.Error(), "debugging") {
		t.Fatalf("refusal %q does not say a person is debugging", err)
	}
	if err := f.devices.BeginCall("list_runs"); err != nil {
		t.Fatalf("a tool that needs no device was refused: %v", err)
	}
	f.devices.EndCall()

	sess.LeavePause()
	if err := f.devices.BeginCall("tap"); err != nil {
		t.Fatalf("a device tool after the run resumed: %v", err)
	}
	f.devices.EndCall()
}

func TestDeviceSessionPause_SurvivesConcurrentCallsAndTheEndOfTheRunScriptCallThatPausedIt(t *testing.T) {
	f := newLifecycleFixture(t)
	f.selectedAndIdle()
	if err := f.devices.BeginCall("run_script"); err != nil {
		t.Fatal(err)
	}
	sess := f.pauseHeldSession()

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if err := f.devices.BeginCall("list_runs"); err == nil {
				f.devices.EndCall()
			}
			_ = f.devices.BeginCall("tap")
		})
	}
	wg.Wait()

	if _, paused := sess.Paused(); !paused {
		t.Fatal("a tool call cleared the pause")
	}
	f.devices.EndCall()
	if _, paused := sess.Paused(); !paused {
		t.Fatal("settling the holder cleared the pause")
	}
	if got := sess.Info().State; got != session.StatePaused {
		t.Fatalf("holder state = %q, want paused", got)
	}
}

func TestDeviceSessionPause_APausedHolderRefusesToYieldEvenWithNothingInFlight(t *testing.T) {
	f := newLifecycleFixture(t)
	f.selectedAndIdle()
	f.pauseHeldSession()

	if got := codeOfErr(f.requestYield()); got != diag.CodeHolderBusy {
		t.Fatalf("yield while paused = %q, want %s", got, diag.CodeHolderBusy)
	}
	f.requireStillHeld("a paused run")
}

func TestDeviceSessionPause_TheIdleTimerNeverReleasesAPausedHolder(t *testing.T) {
	f := newLifecycleFixture(t)
	f.selectedAndIdle()
	f.clk.AwaitSleepers(1)
	sess := f.pauseHeldSession()

	for range 2 {
		f.clk.Advance(testIdleMs)
		f.clk.AwaitSleepers(1)
		f.requireStillHeld("a paused run past the idle timeout")
	}

	sess.LeavePause()
	f.clk.Advance(testIdleMs)
	f.awaitRelease(f.devices)
	if got := f.state(f.devices); got != holderReleased {
		t.Fatalf("state after the pause ended and the timer fired = %q, want released", got)
	}
}

func TestDeviceSessionPause_TheOpenRunTimerNeverClosesAPausedRun(t *testing.T) {
	f := newLifecycleFixture(t)
	f.selectedAndIdle()
	f.clk.AwaitSleepers(1)
	if err := f.devices.BeginCall("start_run"); err != nil {
		t.Fatal(err)
	}
	f.startRun()
	f.devices.EndCall()
	f.clk.AwaitSleepers(1)
	f.pauseHeldSession()

	f.clk.Advance(testOpenRunMs)
	f.clk.AwaitSleepers(1)
	f.requireStillHeld("a paused run past the open-run timeout")
	if f.runs.OpenRunID() == "" {
		t.Fatal("the open run was closed under the pause")
	}
}
