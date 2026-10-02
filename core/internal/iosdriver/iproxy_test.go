package iosdriver_test

import (
	"context"
	"errors"
	"testing"

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
