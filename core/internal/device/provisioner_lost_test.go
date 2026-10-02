package device

import (
	"context"
	"errors"
	"os"
	"testing"
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
