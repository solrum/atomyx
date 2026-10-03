package iosdriver_test

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
)

func connectDeviceWithExistingListener(t *testing.T, identityFields, runnerID string) (*process.FakeBackground, error) {
	t.Helper()
	port, cleanup := startFakeRunnerListener(t, true, identityFields)
	defer cleanup()
	bg := process.NewFakeBackground()
	d := iosdriver.New(newDefaultFakeConn(), process.NewFake(), iosdriver.Options{
		UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: port, RunnerID: runnerID, Background: bg,
	})
	err := d.Connect(context.Background())
	if err == nil {
		_ = d.Disconnect()
	}
	return bg, err
}

func requireBindConflict(t *testing.T, err error, bg *process.FakeBackground) {
	t.Helper()
	var iproxyErr *iosdriver.IproxyError
	if !errors.As(err, &iproxyErr) || iproxyErr.Code != iosdriver.IproxyErrBindConflict {
		t.Fatalf("Connect() error = %v, want an IproxyErrBindConflict IproxyError", err)
	}
	if len(bg.Invocations()) != 0 {
		t.Fatalf("background starts = %+v, want no iproxy spawn next to a foreign runner", bg.Invocations())
	}
}

func TestIosDriver_Connect_Device_RunnerWithDifferentRunnerIDIsRefused(t *testing.T) {
	bg, err := connectDeviceWithExistingListener(t, `"runnerId":"other","targetUdid":"DEV-1"`, "mine")
	requireBindConflict(t, err, bg)
}

func TestIosDriver_Connect_Device_RunnerWithMatchingRunnerIDIsReused(t *testing.T) {
	bg, err := connectDeviceWithExistingListener(t, `"runnerId":"mine","targetUdid":"DEV-1"`, "mine")
	if err != nil {
		t.Fatalf("Connect() error = %v, want the matching runner reused", err)
	}
	if len(bg.Invocations()) != 0 {
		t.Fatalf("background starts = %+v, want none when the matching runner already answers", bg.Invocations())
	}
}

func TestIosDriver_Connect_Device_NoRunnerIDRefusesOtherTargetUDID(t *testing.T) {
	bg, err := connectDeviceWithExistingListener(t, `"targetUdid":"DEV-2"`, "")
	requireBindConflict(t, err, bg)
}

func TestIosDriver_Connect_Device_NoRunnerIDRefusesRunnerReportingNothing(t *testing.T) {
	bg, err := connectDeviceWithExistingListener(t, ``, "")
	requireBindConflict(t, err, bg)
}

func startTunnelNextToListener(t *testing.T, reuse func() (int, bool)) (*process.FakeBackground, func(), int, error) {
	t.Helper()
	port, cleanup := startFakeTunnelListener(t, true)
	t.Cleanup(cleanup)
	bg := process.NewFakeBackground()
	stop, pid, err := iosdriver.StartTunnel(t.Context(), bg, clock.SystemClock{}, iosdriver.TunnelConfig{
		UDID: "DEV-1", HostPort: port, Reuse: reuse,
	})
	return bg, stop, pid, err
}

func TestStartTunnel_ReusesAListenerTheCallerAccountsForAndReportsItsPid(t *testing.T) {
	bg, stop, pid, err := startTunnelNextToListener(t, func() (int, bool) { return tunnelTestPid, true })

	if err != nil {
		t.Fatalf("StartTunnel() error = %v", err)
	}
	if stop != nil || pid != tunnelTestPid {
		t.Fatalf("stop nil = %v pid = %d, want no stop function and pid %d", stop == nil, pid, tunnelTestPid)
	}
	if len(bg.Invocations()) != 0 {
		t.Fatalf("background starts = %+v, want no second iproxy", bg.Invocations())
	}
}

func TestStartTunnel_RefusesAListenerTheCallerCannotAccountFor(t *testing.T) {
	tests := []struct {
		name  string
		reuse func() (int, bool)
	}{
		{"no accounting given", nil},
		{"accounting declines", func() (int, bool) { return tunnelTestPid, false }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bg, _, _, err := startTunnelNextToListener(t, tc.reuse)

			var iproxyErr *iosdriver.IproxyError
			if !errors.As(err, &iproxyErr) || iproxyErr.Code != iosdriver.IproxyErrBindConflict {
				t.Fatalf("StartTunnel() error = %v, want an IproxyErrBindConflict IproxyError", err)
			}
			if len(bg.Invocations()) != 0 {
				t.Fatalf("background starts = %+v, want no iproxy spawn next to a tunnel nobody can stop", bg.Invocations())
			}
		})
	}
}
