package mcp

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	mockdriver "dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

func TestLaunchAppArgumentsReachDriverAndBypassRepeatSkip(t *testing.T) {
	drv := mockdriver.New()
	drv.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	args := map[string]any{
		"appId":     "com.example.app",
		"arguments": map[string]any{"route": "/focus", "retries": 3, "debug": true},
	}
	for range 2 {
		if result, decoded := callTool(t, cs, ctx, "launch_app", args); result.IsError {
			t.Fatalf("launch_app: %v", decoded)
		}
	}
	calls := launchAppDriverCalls(drv)
	if len(calls) != 2 {
		t.Fatalf("driver launches = %d, want 2 (arguments are never skipped as a repeat)", len(calls))
	}
	want := map[string]string{"route": "/focus", "retries": "3", "debug": "true"}
	if !reflect.DeepEqual(calls[0].Arguments, want) {
		t.Fatalf("driver launch arguments = %v, want %v", calls[0].Arguments, want)
	}
}

func TestLaunchAppRejectsNonScalarArguments(t *testing.T) {
	drv := mockdriver.New()
	drv.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)

	for _, arguments := range []any{map[string]any{"a": []any{"x"}}, map[string]any{"a": nil}, "route=/focus"} {
		result, decoded := callTool(t, cs, ctx, "launch_app", map[string]any{"appId": "com.example.app", "arguments": arguments})
		if !result.IsError || decoded["code"] != diag.CodeInvalidArgument {
			t.Fatalf("arguments %v: got %v, want INVALID_ARGUMENT", arguments, decoded)
		}
	}
	if len(launchAppDriverCalls(drv)) != 0 {
		t.Fatal("invalid arguments reached the driver")
	}
}

func linkLaunchServer(t *testing.T, drv *mockdriver.Driver) (*mcp.ClientSession, context.Context) {
	t.Helper()
	drv.Apps = []driver.InstalledApp{{BundleID: "com.example.app"}}
	cs, ctx := testServer(t, ServerDeps{
		Probe:   device.NewStaticProbe(),
		Factory: singleMockDriverFactory{driver: drv},
		Storage: storage.NewInMemoryStorage(),
	})
	selectMockDevice(t, cs, ctx)
	return cs, ctx
}

func TestLaunchAppLinkOpensTheLinkInTheApp(t *testing.T) {
	drv := mockdriver.New()
	cs, ctx := linkLaunchServer(t, drv)

	result, decoded := callTool(t, cs, ctx, "launch_app", map[string]any{"appId": "com.example.app", "link": "myapp://orders/42?a=1&b=2"})
	if result.IsError {
		t.Fatalf("launch_app with link: %v", decoded)
	}
	if len(drv.OpenedLinks) != 1 || drv.OpenedLinks[0] != "myapp://orders/42?a=1&b=2" {
		t.Fatalf("opened links = %v, want the link", drv.OpenedLinks)
	}
	if got := len(launchAppDriverCalls(drv)); got != 0 {
		t.Fatalf("driver launches = %d, want 0 when a link starts the app", got)
	}
}

func TestLaunchAppLinkRejectsArgumentsAndModes(t *testing.T) {
	tests := []map[string]any{
		{"appId": "com.example.app", "link": "myapp://home", "arguments": map[string]any{"a": "b"}},
		{"appId": "com.example.app", "link": "myapp://home", "mode": "fresh"},
		{"appId": "com.example.app", "link": "no scheme"},
	}
	for _, args := range tests {
		drv := mockdriver.New()
		cs, ctx := linkLaunchServer(t, drv)

		result, decoded := callTool(t, cs, ctx, "launch_app", args)
		if !result.IsError || decoded["code"] != diag.CodeInvalidArgument {
			t.Fatalf("launch_app %v: got %v, want INVALID_ARGUMENT", args, decoded)
		}
		if len(drv.OpenedLinks) != 0 {
			t.Fatalf("launch_app %v opened %v, want nothing", args, drv.OpenedLinks)
		}
	}
}

func TestLaunchAppLinkReportsAnUnsupportedDevice(t *testing.T) {
	drv := mockdriver.New()
	drv.SetCapabilities(driver.Capabilities{CanOpenLink: false})
	cs, ctx := linkLaunchServer(t, drv)

	result, decoded := callTool(t, cs, ctx, "launch_app", map[string]any{"appId": "com.example.app", "link": "myapp://home"})
	if !result.IsError || decoded["code"] != diag.CodeLinkUnsupported {
		t.Fatalf("launch_app: got %v, want %s", decoded, diag.CodeLinkUnsupported)
	}
}

func TestLaunchAppPermissionsAreAppliedBeforeTheLaunch(t *testing.T) {
	drv := mockdriver.New()
	cs, ctx := linkLaunchServer(t, drv)

	result, decoded := callTool(t, cs, ctx, "launch_app", map[string]any{
		"appId":       "com.example.app",
		"permissions": map[string]any{"camera": "allow", "location": "deny"},
	})
	if result.IsError {
		t.Fatalf("launch_app with permissions: %v", decoded)
	}
	if len(drv.PermissionChanges) != 1 {
		t.Fatalf("permission changes = %+v, want one", drv.PermissionChanges)
	}
	change := drv.PermissionChanges[0]
	if change.AppID != "com.example.app" || change.Permissions[driver.PermissionCamera] != driver.PermissionAllow || change.Permissions[driver.PermissionLocation] != driver.PermissionDeny {
		t.Fatalf("permission change = %+v, want camera allow and location deny for the app", change)
	}
	if got := len(launchAppDriverCalls(drv)); got != 1 {
		t.Fatalf("driver launches = %d, want 1", got)
	}
}

func TestLaunchAppRejectsUnknownPermissionNamesAndStates(t *testing.T) {
	tests := []map[string]any{
		{"appId": "com.example.app", "permissions": map[string]any{"bluetooth": "allow"}},
		{"appId": "com.example.app", "permissions": map[string]any{"camera": "maybe"}},
	}
	for _, args := range tests {
		drv := mockdriver.New()
		cs, ctx := linkLaunchServer(t, drv)

		result, decoded := callTool(t, cs, ctx, "launch_app", args)
		if !result.IsError || decoded["code"] != diag.CodeInvalidArgument {
			t.Fatalf("launch_app %v: got %v, want INVALID_ARGUMENT", args, decoded)
		}
		if len(drv.PermissionChanges) != 0 || len(launchAppDriverCalls(drv)) != 0 {
			t.Fatalf("launch_app %v reached the device", args)
		}
	}
}

func TestLaunchAppPermissionsReportAnUnsupportedDevice(t *testing.T) {
	drv := mockdriver.New()
	drv.SetCapabilities(driver.Capabilities{CanSetPermissions: false})
	cs, ctx := linkLaunchServer(t, drv)

	result, decoded := callTool(t, cs, ctx, "launch_app", map[string]any{
		"appId":       "com.example.app",
		"permissions": map[string]any{"camera": "allow"},
	})
	if !result.IsError || decoded["code"] != diag.CodePermissionsUnsupported {
		t.Fatalf("launch_app: got %v, want %s", decoded, diag.CodePermissionsUnsupported)
	}
	if len(launchAppDriverCalls(drv)) != 0 {
		t.Fatal("the app was launched although its permissions could not be set")
	}
}
