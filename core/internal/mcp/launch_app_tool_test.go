package mcp

import (
	"errors"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/storage"
)

func TestLaunchAppDispatchesToDriver(t *testing.T) {
	drv := mockdriver.New()
	drv.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "launch_app", map[string]any{"appId": "com.example.app"})
	if result.IsError {
		t.Fatalf("launch_app: unexpected isError: %v", decoded)
	}
	if decoded["ok"] != true || decoded["appId"] != "com.example.app" {
		t.Fatalf("launch_app: got %v, want ok:true appId:com.example.app", decoded)
	}
	if drv.RunningApp == nil || *drv.RunningApp != "com.example.app" {
		t.Fatalf("launch_app: driver RunningApp = %v, want com.example.app", drv.RunningApp)
	}
}

func TestLaunchAppSurfacesDriverError(t *testing.T) {
	drv := mockdriver.New()
	drv.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	drv.LaunchAppErr = errors.New("driver launch failed")
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "launch_app", map[string]any{"appId": "com.example.app"})
	if !result.IsError {
		t.Fatal("launch_app (driver error): expected isError true")
	}
	if decoded["code"] != diag.CodeUnknown {
		t.Fatalf("launch_app (driver error) code: got %v, want %s", decoded["code"], diag.CodeUnknown)
	}
	if decoded["message"] != "driver launch failed" {
		t.Fatalf("launch_app (driver error) message: got %v, want %q", decoded["message"], "driver launch failed")
	}
}
