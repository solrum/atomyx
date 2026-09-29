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

func TestListAppsReturnsInstalledApps(t *testing.T) {
	drv := mockdriver.New()
	drv.Apps = []driver.InstalledApp{
		{BundleID: "com.example.app", DisplayName: "Example"},
	}
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "list_apps", map[string]any{})
	if result.IsError {
		t.Fatalf("list_apps: unexpected isError: %v", decoded)
	}
	if decoded["count"] != float64(1) {
		t.Fatalf("list_apps count: got %v, want 1", decoded["count"])
	}
	apps, ok := decoded["apps"].([]any)
	if !ok || len(apps) != 1 {
		t.Fatalf("list_apps apps: got %v, want a single entry", decoded["apps"])
	}
	entry := apps[0].(map[string]any)
	if entry["bundleId"] != "com.example.app" || entry["displayName"] != "Example" {
		t.Fatalf("list_apps apps[0]: got %v", entry)
	}
	if _, present := decoded["note"]; present {
		t.Fatalf("list_apps: unexpected note with apps present: %v", decoded)
	}
}

func TestListAppsSurfacesDriverError(t *testing.T) {
	drv := mockdriver.New()
	drv.ListAppsErr = errors.New("enumeration unavailable")
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	result, decoded := callTool(t, cs, ctx, "list_apps", map[string]any{})
	if !result.IsError {
		t.Fatal("list_apps (driver error): expected isError true")
	}
	if decoded["code"] != diag.CodeUnknown {
		t.Fatalf("list_apps (driver error) code: got %v, want %s", decoded["code"], diag.CodeUnknown)
	}
	if decoded["message"] != "enumeration unavailable" {
		t.Fatalf("list_apps (driver error) message: got %v, want %q", decoded["message"], "enumeration unavailable")
	}
}
