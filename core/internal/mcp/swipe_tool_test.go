package mcp

import (
	"errors"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/storage"
)

func TestSwipeDirectionDispatchesToDriver(t *testing.T) {
	drv := mockdriver.New()
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "swipe", map[string]any{"direction": "up"})
	if result.IsError {
		t.Fatalf("swipe(direction): unexpected isError: %v", decoded)
	}
	if decoded["ok"] != true || decoded["direction"] != "up" {
		t.Fatalf("swipe(direction): got %v, want ok:true direction:up", decoded)
	}
	found := false
	for _, c := range drv.Calls {
		if c.Method == "Swipe" {
			found = true
		}
	}
	if !found {
		t.Fatal("swipe(direction): driver did not record a Swipe call")
	}
}

func TestSwipeSurfacesDriverError(t *testing.T) {
	drv := mockdriver.New()
	drv.SwipeErr = errors.New("swipe rejected")
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "swipe", map[string]any{"direction": "down"})
	if !result.IsError {
		t.Fatal("swipe (driver error): expected isError true")
	}
	if decoded["code"] != diag.CodeUnknown {
		t.Fatalf("swipe (driver error) code: got %v, want %s", decoded["code"], diag.CodeUnknown)
	}
	if decoded["message"] != "swipe rejected" {
		t.Fatalf("swipe (driver error) message: got %v, want %q", decoded["message"], "swipe rejected")
	}
}
