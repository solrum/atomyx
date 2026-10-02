package iosdriver_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
)

const tunnelExitVerdictTimeout = 5 * time.Second

func TestStartTunnel_ExitAfterReadyIsReportedAsATunnelExit(t *testing.T) {
	port := freeTCPPort(t)
	proc := process.NewFakeBackgroundProcess()
	verdicts := make(chan error, 1)

	stop, _, err := iosdriver.StartTunnel(t.Context(), exitingBackground{t: t, proc: proc}, clock.SystemClock{}, iosdriver.TunnelConfig{
		UDID: "DEV-1", HostPort: port, OnExit: func(verdict error) { verdicts <- verdict },
	})
	if err != nil {
		t.Fatalf("StartTunnel() error = %v", err)
	}
	defer stop()

	proc.ExitWithStderr(1, "iproxy: device unplugged")
	select {
	case verdict := <-verdicts:
		var iproxyErr *iosdriver.IproxyError
		if !errors.As(verdict, &iproxyErr) || iproxyErr.Code != iosdriver.IproxyErrTunnelExit {
			t.Fatalf("verdict = %v, want an IproxyErrTunnelExit IproxyError", verdict)
		}
		if !strings.Contains(iproxyErr.Message, "after the tunnel was reachable") || strings.Contains(iproxyErr.Message, "before") {
			t.Fatalf("message = %q, want it to say the exit came after the tunnel was reachable", iproxyErr.Message)
		}
	case <-time.After(tunnelExitVerdictTimeout):
		t.Fatal("no verdict after the iproxy process exited")
	}
}

func TestStartTunnel_ExitBeforeReadyIsReportedAsASpawnExit(t *testing.T) {
	port := freeTCPPort(t)
	bg := process.NewFakeBackground()
	proc := process.NewFakeBackgroundProcess()
	proc.ExitWithStderr(1, "iproxy: no device found")
	bg.Stub(proc)

	_, _, err := iosdriver.StartTunnel(t.Context(), bg, clock.SystemClock{}, iosdriver.TunnelConfig{
		UDID: "DEV-1", HostPort: port,
	})
	var iproxyErr *iosdriver.IproxyError
	if !errors.As(err, &iproxyErr) || iproxyErr.Code != iosdriver.IproxyErrSpawnExit {
		t.Fatalf("StartTunnel() error = %v, want an IproxyErrSpawnExit IproxyError", err)
	}
	if !strings.Contains(iproxyErr.Message, "before the tunnel became reachable") {
		t.Fatalf("message = %q, want it to say the exit came before the tunnel was reachable", iproxyErr.Message)
	}
}

// exitingBackground starts a fake iproxy that accepts connections on
// the host port of its port mapping and exits when the test says so.
type exitingBackground struct {
	t    *testing.T
	proc *process.FakeBackgroundProcess
}

func (b exitingBackground) Start(ctx context.Context, _ string, args ...string) (process.BackgroundProcess, error) {
	return listeningBackground{t: b.t}.startWith(ctx, b.proc, args)
}
