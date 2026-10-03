package device

import (
	"context"
	"errors"
	"os"
	"testing"

	"dev.atomyx/internal/logging"
)

func awaitLoss(t *testing.T, lost <-chan error) *ProcessExitError {
	t.Helper()
	cause := <-lost
	var exit *ProcessExitError
	if !errors.As(cause, &exit) {
		t.Fatalf("loss cause = %v, want a *ProcessExitError", cause)
	}
	return exit
}

func TestDriverProvisioner_EnsureIOSAgentWatched_RunnerExitEndsTheAgentAndReportsTheLoss(t *testing.T) {
	fx := newLaunchFixture(t)
	p := fx.provisioner(nil)
	lost := make(chan error, 1)
	_, kill, err := p.EnsureIOSAgentWatched(context.Background(), "SIM-1", IosAgentKindSimulator, func(cause error) { lost <- cause })
	if err != nil {
		t.Fatalf("EnsureIOSAgentWatched() error = %v", err)
	}

	fx.farm.started("xcodebuild")[0].ExitWithStderr(1, "device disconnected")

	exit := awaitLoss(t, lost)
	if exit.Component != LostRunner {
		t.Errorf("component = %q, want %q", exit.Component, LostRunner)
	}
	if _, err := os.Stat(fx.statePath("SIM-1")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("runner record after the loss: %v, want it removed by the provisioner's own kill", err)
	}
	p.mu.Lock()
	held := len(p.reserved)
	p.mu.Unlock()
	if held != 0 {
		t.Errorf("reserved ports after the loss = %d, want 0", held)
	}
	if err := kill(); err != nil {
		t.Errorf("kill() after the loss = %v, want nil", err)
	}
}

func TestDriverProvisioner_EnsureIOSAgentWatched_TunnelExitEndsTheRunnerAndReportsTheLoss(t *testing.T) {
	fx := newLaunchFixture(t)
	p := fx.provisioner(nil)
	lost := make(chan error, 1)
	_, _, err := p.EnsureIOSAgentWatched(context.Background(), "DEV-1", IosAgentKindDevice, func(cause error) { lost <- cause })
	if err != nil {
		t.Fatalf("EnsureIOSAgentWatched() error = %v", err)
	}

	fx.farm.started("iproxy")[0].Exit(1)

	exit := awaitLoss(t, lost)
	if exit.Component != LostTunnel {
		t.Errorf("component = %q, want %q", exit.Component, LostTunnel)
	}
	if !fx.farm.started("xcodebuild")[0].Killed() {
		t.Error("the runner was left running after its tunnel exited")
	}
}

func TestDriverProvisioner_EnsureIOSAgentWatched_AFailedLaunchReportsNoLoss(t *testing.T) {
	fx := newLaunchFixture(t)
	fx.farm.exitFirstRunners = 1 << 10
	p := fx.provisioner(nil)
	_, _, err := p.EnsureIOSAgentWatched(context.Background(), "SIM-1", IosAgentKindSimulator, func(cause error) {
		t.Errorf("a failed launch reported a loss: %v", cause)
	})
	if err == nil {
		t.Fatal("EnsureIOSAgentWatched() error = nil, want the launch failure")
	}
}

// debugSignalLogger reports each Debug message on a channel and drops
// every other log call.
type debugSignalLogger struct{ debug chan string }

func (l debugSignalLogger) Debug(msg string, _ logging.LogContext)  { l.debug <- msg }
func (debugSignalLogger) Info(string, logging.LogContext)           {}
func (debugSignalLogger) Warn(string, logging.LogContext)           {}
func (debugSignalLogger) Error(string, error, logging.LogContext)   {}
func (l debugSignalLogger) Child(logging.LogContext) logging.Logger { return l }

func TestDriverProvisioner_EnsureIOSAgentWatched_RunnerExitAfterAnotherProcessTookOwnershipTouchesNothing(t *testing.T) {
	fx := newLaunchFixture(t)
	logger := debugSignalLogger{debug: make(chan string, 1)}
	p := fx.provisioner(logger)
	lost := make(chan error, 1)
	_, _, err := p.EnsureIOSAgentWatched(context.Background(), "DEV-1", IosAgentKindDevice, func(cause error) { lost <- cause })
	if err != nil {
		t.Fatalf("EnsureIOSAgentWatched() error = %v", err)
	}
	record, err := ReadRunnerState(fx.statePath("DEV-1"))
	if err != nil {
		t.Fatal(err)
	}
	record.OwnerPid = testOwnerPid + 1
	if err := WriteRunnerState(fx.statePath("DEV-1"), record); err != nil {
		t.Fatal(err)
	}

	fx.farm.started("xcodebuild")[0].Exit(1)

	select {
	case <-logger.debug:
	case cause := <-lost:
		t.Fatalf("a runner owned by another process was reported lost: %v", cause)
	}
	if fx.farm.started("iproxy")[0].Killed() {
		t.Error("the tunnel of a runner owned by another process was stopped")
	}
	kept, err := ReadRunnerState(fx.statePath("DEV-1"))
	if err != nil || kept.OwnerPid != testOwnerPid+1 {
		t.Errorf("record = %+v (%v), want it kept with its new owner", kept, err)
	}
}
