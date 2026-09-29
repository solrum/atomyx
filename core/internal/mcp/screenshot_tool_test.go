package mcp

import (
	"errors"
	"os"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/storage"
)

func TestScreenshotSavesFileAndReturnsPath(t *testing.T) {
	drv := mockdriver.New()
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "screenshot", map[string]any{})
	if result.IsError {
		t.Fatalf("screenshot: unexpected isError: %v", decoded)
	}
	path, _ := decoded["path"].(string)
	if path == "" {
		t.Fatalf("screenshot: missing path in %v", decoded)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if decoded["format"] != "png" {
		t.Fatalf("screenshot format: got %v, want png", decoded["format"])
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("screenshot: file not written at %q: %v", path, err)
	}
}

func TestScreenshotSurfacesDriverError(t *testing.T) {
	drv := mockdriver.New()
	drv.ScreenshotErr = errors.New("capture failed")
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "screenshot", map[string]any{})
	if !result.IsError {
		t.Fatal("screenshot (driver error): expected isError true")
	}
	if decoded["code"] != diag.CodeUnknown {
		t.Fatalf("screenshot (driver error) code: got %v, want %s", decoded["code"], diag.CodeUnknown)
	}
	if decoded["message"] != "capture failed" {
		t.Fatalf("screenshot (driver error) message: got %v, want %q", decoded["message"], "capture failed")
	}
}
