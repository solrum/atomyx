package iosdriver_test

import (
	"context"
	"net"
	"strings"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
)

const tunnelTestPid = 7001

// listeningBackground starts a fake iproxy that already accepts
// connections on the host port of its port mapping and reports a pid.
type listeningBackground struct {
	t *testing.T
}

func (b listeningBackground) Start(ctx context.Context, _ string, args ...string) (process.BackgroundProcess, error) {
	hostPort, _, _ := strings.Cut(args[0], ":")
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:"+hostPort)
	if err != nil {
		return nil, err
	}
	b.t.Cleanup(func() { _ = listener.Close() })
	proc := process.NewFakeBackgroundProcess()
	proc.SetPid(tunnelTestPid)
	return proc, nil
}

func TestStartTunnel_ReportsThePidOfTheIproxyItStarted(t *testing.T) {
	port := freeTCPPort(t)

	stop, pid, err := iosdriver.StartTunnel(t.Context(), listeningBackground{t: t}, clock.SystemClock{}, iosdriver.TunnelConfig{
		UDID: "DEV-1", HostPort: port,
	})
	if err != nil {
		t.Fatalf("StartTunnel() error = %v", err)
	}
	defer stop()

	if pid != tunnelTestPid {
		t.Fatalf("pid = %d, want %d", pid, tunnelTestPid)
	}
}
