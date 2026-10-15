package iosdriver_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
)

const tunnelStopWait = 2 * time.Second

func requireTunnelStopped(t *testing.T, stopped <-chan struct{}) {
	t.Helper()
	select {
	case <-stopped:
	case <-time.After(tunnelStopWait):
		t.Fatal("the iproxy process this driver started is still running after a failed Connect")
	}
}

func TestIosDriver_Connect_Device_TransportFailureStopsTunnelItStarted(t *testing.T) {
	stopped := make(chan struct{})
	conn := newDefaultFakeConn()
	conn.connectErr = errors.New("connection reset")
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{
		UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: freeTCPPort(t), Background: &tunnelBackground{stopped: stopped},
	})

	if err := d.Connect(context.Background()); err == nil {
		t.Fatal("Connect() error = nil, want the transport failure")
	}

	requireTunnelStopped(t, stopped)
}

func TestIosDriver_Connect_Device_PingFailureStopsTunnelItStarted(t *testing.T) {
	stopped := make(chan struct{})
	conn := newDefaultFakeConn()
	conn.Respond("ping", func(map[string]any) (map[string]any, error) {
		return nil, errors.New("runner closed the connection")
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{
		UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: freeTCPPort(t), Background: &tunnelBackground{stopped: stopped},
	})

	if err := d.Connect(context.Background()); err == nil {
		t.Fatal("Connect() error = nil, want the ping failure")
	}

	requireTunnelStopped(t, stopped)
	if conn.IsConnected() {
		t.Fatal("the transport is still open after a failed Connect")
	}
}

func TestIosDriver_Connect_Device_FailureLeavesReusedTunnelRunning(t *testing.T) {
	port, count := startHeldListener(t, 0, "")
	bg := process.NewFakeBackground()
	conn := newDefaultFakeConn()
	conn.connectErr = errors.New("connection reset")
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{
		UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: port, Background: bg,
	})
	t.Cleanup(iosdriver.SetTunnelPingTimeout(busyTestProbeTimeout))

	if err := d.Connect(context.Background()); err == nil {
		t.Fatal("Connect() error = nil, want the transport failure")
	}
	_ = d.Disconnect()

	if len(bg.Invocations()) != 0 {
		t.Fatalf("background starts = %+v, want the existing tunnel reused", bg.Invocations())
	}
	if count.Load() == 0 {
		t.Fatal("the existing listener was never probed")
	}
}
