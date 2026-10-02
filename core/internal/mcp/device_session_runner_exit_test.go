package mcp

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/session"
)

func newRunnerExitFixture(t *testing.T) *lifecycleFixture {
	t.Helper()
	f := newLifecycleFixture(t)
	f.factory.lost = make(chan error, 1)
	if err := f.selectDevice(f.devices); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *lifecycleFixture) exitRunner() {
	f.factory.lost <- &device.ProcessExitError{Component: device.LostRunner}
}

func TestDeviceSession_ARunnerExitMovesTheSessionToLostAndClosesTheOpenRunAsDeviceLost(t *testing.T) {
	f := newRunnerExitFixture(t)
	f.startRun()

	f.exitRunner()
	f.awaitRelease(f.devices)

	if got := f.state(f.devices); got != holderLost || f.devices.Current() != nil {
		t.Fatalf("state = %s current = %v, want lost with no device bound", got, f.devices.Current())
	}
	meta := f.onlyRun()
	if meta.Outcome != runfolder.OutcomeDeviceLost || meta.Result != runfolder.ResultCancelled {
		t.Fatalf("run = %s/%s, want result cancelled with outcome deviceLost", meta.Result, meta.Outcome)
	}
	if _, err := f.devices.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure() error = %v, want the next device call to take the remembered device again", err)
	}
}

func TestDeviceSession_ARunnerExitRecordsABareRunScriptAsDeviceLostNotCancelled(t *testing.T) {
	f := newRunnerExitFixture(t)
	started := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		_, err := f.runs.RunScript(context.Background(), "slow", nil, func(target scriptTarget) (runner.ScriptResult, error) {
			close(started)
			<-target.Ctx.Done()
			return runner.ScriptResult{}, target.Ctx.Err()
		})
		finished <- err
	}()
	<-started

	f.exitRunner()
	f.awaitRelease(f.devices)

	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("RunScript error = %v, want the script's own cancellation", err)
	}
	if meta := f.onlyRun(); meta.Outcome != runfolder.OutcomeDeviceLost || meta.Result != runfolder.ResultCancelled {
		t.Fatalf("run = %s/%s, want result cancelled with outcome deviceLost", meta.Result, meta.Outcome)
	}
}

func TestDeviceSession_ARunnerExitCancelsAScriptInTheOpenRunWithTheLossAsCause(t *testing.T) {
	f := newRunnerExitFixture(t)
	f.startRun()
	started := make(chan struct{})
	causes := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, _ = f.runs.RunScript(context.Background(), "slow", nil, func(target scriptTarget) (runner.ScriptResult, error) {
			close(started)
			<-target.Ctx.Done()
			causes <- context.Cause(target.Ctx)
			return runner.ScriptResult{}, target.Ctx.Err()
		})
	}()
	<-started

	f.exitRunner()
	f.awaitRelease(f.devices)
	<-finished

	if cause := <-causes; !errors.Is(cause, session.ErrDeviceLost) {
		t.Fatalf("cancel cause = %v, want session.ErrDeviceLost", cause)
	}
	if meta := f.onlyRun(); meta.Outcome != runfolder.OutcomeDeviceLost {
		t.Fatalf("run outcome = %s, want deviceLost", meta.Outcome)
	}
}
