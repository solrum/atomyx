package iosdriver_test

import (
	"context"
	"testing"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

var faultInjectionLocation = driver.Location{Latitude: 1, Longitude: 2}

// newFaultInjectionDevice builds a physical-device driver over conn. The
// external tunnel option keeps Connect from launching iproxy, so the
// driver reaches the fake runner directly.
func newFaultInjectionDevice(conn iosdriver.Conn) *iosdriver.IosDriver {
	return iosdriver.New(conn, process.NewFake(), iosdriver.Options{
		Background:     process.NewFakeBackground(),
		UDID:           "fault-injection",
		Kind:           iosdriver.KindDevice,
		ExternalTunnel: true,
	})
}

// TestIosDriver_SetLocation_DeviceWireCallTimeout proves that a device
// SetLocation against a runner that never answers returns within
// faultInjectionBudget with diag.CodeToolTimeout.
func TestIosDriver_SetLocation_DeviceWireCallTimeout(t *testing.T) {
	agent := newFaultInjectionIosAgent(t, "ping")
	host, port := agent.hostPort(t)
	d := newFaultInjectionDevice(transport.NewTCPClient(host, port, transport.WithTCPResponseTimeout(responseTimeout)))
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() against a healthy runner: %v", err)
	}

	start := time.Now()
	err := d.SetLocation(context.Background(), faultInjectionLocation)
	if elapsed := time.Since(start); elapsed > faultInjectionBudget {
		t.Fatalf("SetLocation took %v, want it bounded by %v", elapsed, faultInjectionBudget)
	}
	if code, _, _ := diag.Describe(err); code != diag.CodeToolTimeout {
		t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeToolTimeout)
	}
}

// TestIosDriver_SetLocation_DeviceBoundaryTranslation proves that a
// device SetLocation names no transport detail when the runner is
// unreachable or rejects the command.
func TestIosDriver_SetLocation_DeviceBoundaryTranslation(t *testing.T) {
	t.Run("dead endpoint", func(t *testing.T) {
		d := newFaultInjectionDevice(transport.NewTCPClient("127.0.0.1", 1, transport.WithTCPResponseTimeout(responseTimeout)))
		err := d.SetLocation(context.Background(), faultInjectionLocation)
		if err == nil {
			t.Fatal("SetLocation against a dead endpoint returned no error")
		}
		assertNoForbiddenTransportSubstring(t, "SetLocation", err.Error())
		if code, _, _ := diag.Describe(err); code != diag.CodeAgentUnreachable {
			t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeAgentUnreachable)
		}
	})

	t.Run("agent rejection", func(t *testing.T) {
		agent := newRejectingIosAgent(t)
		host, port := agent.hostPort(t)
		conn := transport.NewTCPClient(host, port, transport.WithTCPResponseTimeout(responseTimeout))
		if err := conn.Connect(context.Background()); err != nil {
			t.Fatalf("conn.Connect() against a rejecting runner: %v", err)
		}
		d := newFaultInjectionDevice(conn)
		err := d.SetLocation(context.Background(), faultInjectionLocation)
		if err == nil {
			t.Fatal("SetLocation against a rejecting runner returned no error")
		}
		assertNoForbiddenTransportSubstring(t, "SetLocation", err.Error())
		if code, _, _ := diag.Describe(err); code != diag.CodeAgentRejected {
			t.Fatalf("code = %q (err %v), want %q", code, err, diag.CodeAgentRejected)
		}
	})
}
