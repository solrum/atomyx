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

func launchAppDriverCalls(drv *mockdriver.Driver) []driver.LaunchArgs {
	var out []driver.LaunchArgs
	for _, call := range drv.Calls {
		if call.Method == "LaunchApp" {
			out = append(out, call.Args[1].(driver.LaunchArgs))
		}
	}
	return out
}

func TestLaunchAppModeReachesDriver(t *testing.T) {
	tests := []struct {
		name  string
		args  map[string]any
		check func(*bool) bool
	}{
		{"omitted", map[string]any{"appId": "com.example.app"}, func(b *bool) bool { return b == nil }},
		{"default", map[string]any{"appId": "com.example.app", "mode": "default"}, func(b *bool) bool { return b == nil }},
		{"fresh", map[string]any{"appId": "com.example.app", "mode": "fresh"}, func(b *bool) bool { return b != nil && !*b }},
		{"attach", map[string]any{"appId": "com.example.app", "mode": "attach"}, func(b *bool) bool { return b != nil && *b }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			drv := mockdriver.New()
			drv.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
			cs, ctx := testServer(t, ServerDeps{
				Probe:   device.NewStaticProbe(),
				Factory: singleMockDriverFactory{driver: drv},
				Storage: storage.NewInMemoryStorage(),
			})
			selectMockDevice(t, cs, ctx)

			result, decoded := callTool(t, cs, ctx, "launch_app", tc.args)
			if result.IsError {
				t.Fatalf("launch_app: %v", decoded)
			}
			calls := launchAppDriverCalls(drv)
			if len(calls) != 1 || !tc.check(calls[0].NoReset) {
				t.Fatalf("driver launch args = %+v", calls)
			}
		})
	}
}

func TestLaunchAppFreshForcesRestartAndDefaultIsSkipped(t *testing.T) {
	drv := mockdriver.New()
	drv.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	for _, mode := range []string{"default", "default", "fresh"} {
		if result, decoded := callTool(t, cs, ctx, "launch_app", map[string]any{"appId": "com.example.app", "mode": mode}); result.IsError {
			t.Fatalf("launch_app %s: %v", mode, decoded)
		}
	}
	if got := len(launchAppDriverCalls(drv)); got != 2 {
		t.Fatalf("driver launches = %d, want 2 (second default skipped, fresh relaunches)", got)
	}
}

func TestLaunchAppRejectsUnknownMode(t *testing.T) {
	drv := mockdriver.New()
	drv.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	for _, mode := range []any{"bogus", "", 3} {
		result, decoded := callTool(t, cs, ctx, "launch_app", map[string]any{"appId": "com.example.app", "mode": mode})
		if !result.IsError || decoded["code"] != diag.CodeInvalidArgument {
			t.Fatalf("mode %v: got %v, want INVALID_ARGUMENT", mode, decoded)
		}
	}
	if len(launchAppDriverCalls(drv)) != 0 {
		t.Fatal("an invalid mode reached the driver")
	}
}
