package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/storage"
)

// erroringDeviceFactory is a device.Factory whose Build always fails,
// so a test can drive DeviceSession.Select's error branch without a
// real device or transport.
type erroringDeviceFactory struct {
	err error
}

func (f erroringDeviceFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	return device.Built{}, f.err
}

func TestSelectDeviceSurfacesFactoryError(t *testing.T) {
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: erroringDeviceFactory{err: errors.New("connect refused")},
		Storage: storage.NewInMemoryStorage(),
	})

	result, decoded := callTool(t, cs, ctx, "select_device", map[string]any{
		"platform": "android",
		"id":       "emulator-5554",
	})
	if result.IsError {
		t.Fatalf("select_device (factory error): unexpected isError: %v", decoded)
	}
	if decoded["ok"] != false {
		t.Fatalf("select_device (factory error): got %v, want ok:false", decoded)
	}
	reason, _ := decoded["reason"].(string)
	if !strings.Contains(reason, "connect refused") {
		t.Fatalf("select_device (factory error) reason: got %q, want it to include the underlying error", reason)
	}
	if _, present := decoded["active"]; present {
		t.Fatalf("select_device (factory error): unexpected active field, got %v", decoded)
	}
}

func TestSelectDeviceSurfacesCodedFailureCleanly(t *testing.T) {
	codedErr := diag.New(
		diag.CodeAgentNotReady,
		"The accessibility service is not running. Re-enable it on the device.",
		"Re-enable the accessibility service on the device, then try again.",
		nil,
	)
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: erroringDeviceFactory{err: codedErr},
		Storage: storage.NewInMemoryStorage(),
	})

	result, decoded := callTool(t, cs, ctx, "select_device", map[string]any{
		"platform": "android",
		"id":       "emulator-5554",
	})
	if result.IsError {
		t.Fatalf("select_device (coded error): unexpected isError: %v", decoded)
	}
	if decoded["ok"] != false {
		t.Fatalf("select_device (coded error): got %v, want ok:false", decoded)
	}
	reason, _ := decoded["reason"].(string)
	wantReason := "The accessibility service is not running. Re-enable it on the device."
	if reason != wantReason {
		t.Fatalf("select_device (coded error) reason: got %q, want %q", reason, wantReason)
	}
	if strings.Contains(reason, "session.select") || strings.Contains(reason, "driver.connect") {
		t.Fatalf("select_device (coded error) reason: got %q, want no internal wrap text", reason)
	}
	if decoded["code"] != string(diag.CodeAgentNotReady) {
		t.Fatalf("select_device (coded error) code: got %v, want %q", decoded["code"], diag.CodeAgentNotReady)
	}
	wantHint := "Re-enable the accessibility service on the device, then try again."
	if decoded["hint"] != wantHint {
		t.Fatalf("select_device (coded error) hint: got %v, want %q", decoded["hint"], wantHint)
	}
}
