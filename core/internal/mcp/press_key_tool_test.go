package mcp

import (
	"errors"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/storage"
)

func TestPressKeyDispatchesToDriver(t *testing.T) {
	drv := mockdriver.New()
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "press_key", map[string]any{"key": "back"})
	if result.IsError {
		t.Fatalf("press_key: unexpected isError: %v", decoded)
	}
	if decoded["ok"] != true {
		t.Fatalf("press_key: got %v, want ok:true", decoded)
	}
	found := false
	for _, c := range drv.Calls {
		if c.Method == "PressKey" {
			found = true
		}
	}
	if !found {
		t.Fatal("press_key: driver did not record a PressKey call")
	}
}

func TestPressKeySurfacesDriverError(t *testing.T) {
	drv := mockdriver.New()
	drv.PressKeyErr = errors.New("key not supported")
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "press_key", map[string]any{"key": "back"})
	if !result.IsError {
		t.Fatal("press_key (driver error): expected isError true")
	}
	if decoded["code"] != diag.CodeUnknown {
		t.Fatalf("press_key (driver error) code: got %v, want %s", decoded["code"], diag.CodeUnknown)
	}
	if decoded["message"] != "key not supported" {
		t.Fatalf("press_key (driver error) message: got %v, want %q", decoded["message"], "key not supported")
	}
}
