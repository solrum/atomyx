package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/session/sessiontest"
	"dev.atomyx/internal/storage"
)

// erroringDeviceFactory is a device.Factory whose Build always fails,
// so a test can drive DeviceSession.Select's error branch without a
// real device or transport.
type erroringDeviceFactory struct {
	err error
}

func (f erroringDeviceFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
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

func pinnedServer(t *testing.T) (*Server, *mockdriver.Driver) {
	t.Helper()
	drv := mockdriver.New()
	server := mustNewServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
		Pinned:  &SelectDeviceInput{Platform: device.PlatformAndroid, ID: "emulator-5554"},
	})
	return server, drv
}

func TestSelectDeviceNamingAnotherIDOnAPinnedServerReturnsDevicePinned(t *testing.T) {
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: mockdriver.New()},
		Storage: storage.NewInMemoryStorage(),
		Pinned:  &SelectDeviceInput{Platform: device.PlatformAndroid, ID: "emulator-5554"},
	})

	result, decoded := callTool(t, cs, ctx, "select_device", map[string]any{
		"platform": "android",
		"id":       "emulator-5556",
	})
	if result.IsError {
		t.Fatalf("select_device (pinned): unexpected isError: %v", decoded)
	}
	if decoded["ok"] != false {
		t.Fatalf("select_device (pinned): got %v, want ok:false", decoded)
	}
	if decoded["code"] != diag.CodeDevicePinned {
		t.Fatalf("select_device (pinned) code: got %v, want %q", decoded["code"], diag.CodeDevicePinned)
	}
	hint, _ := decoded["hint"].(string)
	if !strings.Contains(hint, "bound to one device") {
		t.Fatalf("select_device (pinned) hint: got %q, want it to say the server is bound to one device", hint)
	}
}

func TestSelectDeviceNamingAnotherPlatformOnAPinnedServerReturnsDevicePinned(t *testing.T) {
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: mockdriver.New()},
		Storage: storage.NewInMemoryStorage(),
		Pinned:  &SelectDeviceInput{Platform: device.PlatformAndroid, ID: "emulator-5554"},
	})

	_, decoded := callTool(t, cs, ctx, "select_device", map[string]any{
		"platform": "ios",
		"id":       "emulator-5554",
	})
	if decoded["code"] != diag.CodeDevicePinned {
		t.Fatalf("select_device (pinned, other platform) code: got %v, want %q", decoded["code"], diag.CodeDevicePinned)
	}
}

func TestSelectDeviceNamingThePinnedIDAgainIsAllowed(t *testing.T) {
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: mockdriver.New()},
		Storage: storage.NewInMemoryStorage(),
		Pinned:  &SelectDeviceInput{Platform: device.PlatformAndroid, ID: "emulator-5554"},
	})

	for i := 0; i < 2; i++ {
		_, decoded := callTool(t, cs, ctx, "select_device", map[string]any{
			"platform": "android",
			"id":       "emulator-5554",
		})
		if decoded["ok"] != true {
			t.Fatalf("select_device (pinned id, call %d): got %v, want ok:true", i+1, decoded)
		}
	}
}

func TestPinnedServerSelectsItsDeviceOnFirstUse(t *testing.T) {
	server, drv := pinnedServer(t)
	if server.session.Current() != nil {
		t.Fatal("a pinned server held a device before any call needed it")
	}

	active, err := server.session.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if active.ID != "emulator-5554" || active.Platform != device.PlatformAndroid {
		t.Fatalf("Ensure selected %s/%s, want android/emulator-5554", active.Platform, active.ID)
	}
	if !drv.IsConnected() {
		t.Fatal("the pinned device's driver is not connected after first use")
	}

	again, err := server.session.Ensure(context.Background())
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if again != active {
		t.Fatal("a second Ensure re-selected the pinned device instead of reusing it")
	}
}

func TestUnpinnedServerStillReportsNoActiveDevice(t *testing.T) {
	server := newCloseTestServer(t, singleMockDriverFactory{driver: mockdriver.New()})
	if _, err := server.session.Ensure(context.Background()); !errors.Is(err, errNoActiveDevice) {
		t.Fatalf("Ensure on an unpinned server: got %v, want errNoActiveDevice", err)
	}
}
