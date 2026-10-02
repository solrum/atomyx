package iosdriver_test

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
)

func TestIosDriver_Connect_Device_IproxyEarlyExitSurfacesSpawnExitThroughBackgroundRunner(t *testing.T) {
	port := freeTCPPort(t)
	bg := process.NewFakeBackground()
	proc := process.NewFakeBackgroundProcess()
	proc.ExitWithStderr(1, "iproxy: no device found")
	bg.Stub(proc)
	runner := process.NewFake()
	d := iosdriver.New(newDefaultFakeConn(), runner, iosdriver.Options{
		UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: port, Background: bg,
	})

	err := d.Connect(context.Background())
	var iproxyErr *iosdriver.IproxyError
	if !errors.As(err, &iproxyErr) || iproxyErr.Code != iosdriver.IproxyErrSpawnExit {
		t.Fatalf("Connect() error = %v, want an IproxyErrSpawnExit IproxyError", err)
	}
	if len(bg.Invocations()) != 1 || bg.Invocations()[0].Name != "iproxy" {
		t.Fatalf("background starts = %+v, want exactly one iproxy start", bg.Invocations())
	}
	if len(runner.Invocations()) != 0 {
		t.Fatalf("Runner.Run invocations = %+v, want none", runner.Invocations())
	}
}

func TestIosDriver_Connect_Device_TunnelTimeoutKillsIproxyProcess(t *testing.T) {
	port := freeTCPPort(t)
	bg := process.NewFakeBackground()
	proc := process.NewFakeBackgroundProcess()
	bg.Stub(proc)
	d := iosdriver.New(newDefaultFakeConn(), process.NewFake(), iosdriver.Options{
		UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: port, Clock: &fakeClock{}, Background: bg,
	})

	err := d.Connect(context.Background())
	var iproxyErr *iosdriver.IproxyError
	if !errors.As(err, &iproxyErr) || iproxyErr.Code != iosdriver.IproxyErrTunnelTimeout {
		t.Fatalf("Connect() error = %v, want an IproxyErrTunnelTimeout IproxyError", err)
	}
	if !proc.Killed() {
		t.Fatal("iproxy process was not killed after the tunnel wait timed out")
	}
}

func TestIosDriver_Disconnect_KillsIproxyThroughBackgroundHandle(t *testing.T) {
	port := freeTCPPort(t)
	stopped := make(chan struct{})
	tunnel := &tunnelBackground{stopped: stopped}
	d := iosdriver.New(newDefaultFakeConn(), process.NewFake(), iosdriver.Options{
		UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: port, Background: tunnel,
	})
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := d.Disconnect(); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("Disconnect returned before the iproxy process exited")
	}
	if err := d.Disconnect(); err != nil {
		t.Fatalf("second Disconnect() error = %v, want idempotent", err)
	}
}

func TestIosDriver_Connect_Device_CanceledDuringTunnelWait_KillsIproxyProcess(t *testing.T) {
	port := freeTCPPort(t)
	bg := process.NewFakeBackground()
	proc := process.NewFakeBackgroundProcess()
	bg.Stub(proc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := iosdriver.New(newDefaultFakeConn(), process.NewFake(), iosdriver.Options{
		UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: port, Clock: &cancelOnSleepClock{cancel: cancel}, Background: bg,
	})

	err := d.Connect(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Connect() error = %v, want it to wrap context.Canceled", err)
	}
	if !proc.Killed() {
		t.Fatal("iproxy process was still running after the connect was canceled")
	}
}
