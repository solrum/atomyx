package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"dev.atomyx/internal/driver"
)

func TestApp_NoDeviceSelected_ReturnsNoDeviceSelectedMessage(t *testing.T) {
	ctx := newTestContext()
	RegisterAppFeature(ctx)

	for _, method := range []string{"listApps", "currentForeground"} {
		_, err := ctx.Dispatcher.Dispatch(context.Background(), method, nil)
		if err == nil || err.Error() != "No device selected" {
			t.Fatalf("%s: Dispatch() error = %v, want %q", method, err, "No device selected")
		}
	}

	params, _ := json.Marshal(map[string]string{"bundleId": "com.test"})
	_, err := ctx.Dispatcher.Dispatch(context.Background(), "launchApp", params)
	if err == nil || err.Error() != "No device selected" {
		t.Fatalf("launchApp: Dispatch() error = %v, want %q", err, "No device selected")
	}
}

func TestApp_LaunchApp_MissingBundleID_ReturnsInvalidParams(t *testing.T) {
	ctx := newTestContext()
	RegisterAppFeature(ctx)

	_, err := ctx.Dispatcher.Dispatch(context.Background(), "launchApp", json.RawMessage(`{}`))
	var dispatchErr *DispatcherError
	if !errors.As(err, &dispatchErr) || dispatchErr.Code != ErrInvalidParams {
		t.Fatalf("Dispatch() error = %v, want InvalidParams", err)
	}
}

func TestApp_LaunchApp_ThenCurrentForeground_ReflectsLaunchedBundle(t *testing.T) {
	ctx := newTestContext()
	drv := selectMockDevice(t, ctx)
	RegisterAppFeature(ctx)

	params, _ := json.Marshal(map[string]any{"bundleId": "com.test", "noReset": true})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "launchApp", params); err != nil {
		t.Fatalf("launchApp: Dispatch() error = %v", err)
	}
	if drv.RunningApp == nil || *drv.RunningApp != "com.test" {
		t.Fatalf("driver.RunningApp = %v, want com.test", drv.RunningApp)
	}

	result, err := ctx.Dispatcher.Dispatch(context.Background(), "currentForeground", nil)
	if err != nil {
		t.Fatalf("currentForeground: Dispatch() error = %v", err)
	}
	got, ok := result.(driver.ForegroundInfo)
	if !ok || got.BundleID == nil || *got.BundleID != "com.test" {
		t.Fatalf("currentForeground result = %#v, want bundleId com.test", result)
	}
}

func TestApp_LaunchApp_NonStringArgsElement_IsForwardedNotDropped(t *testing.T) {
	ctx := newTestContext()
	drv := selectMockDevice(t, ctx)
	RegisterAppFeature(ctx)

	params, _ := json.Marshal(map[string]any{
		"bundleId": "com.test",
		"args":     []any{"--flag", 5, true},
	})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "launchApp", params); err != nil {
		t.Fatalf("launchApp: Dispatch() error = %v", err)
	}

	if len(drv.Calls) != 1 || drv.Calls[0].Method != "LaunchApp" {
		t.Fatalf("Calls = %+v, want one LaunchApp entry", drv.Calls)
	}
	launchArgs, ok := drv.Calls[0].Args[1].(driver.LaunchArgs)
	if !ok {
		t.Fatalf("Calls[0].Args[1] = %#v, want driver.LaunchArgs", drv.Calls[0].Args[1])
	}
	want := []string{"--flag", "5", "true"}
	if !reflect.DeepEqual(launchArgs.Args, want) {
		t.Fatalf("LaunchArgs.Args = %v, want %v (non-string elements must be forwarded, not silently dropped)", launchArgs.Args, want)
	}
}

func TestApp_ListApps_ReturnsDriverApps(t *testing.T) {
	ctx := newTestContext()
	selectMockDevice(t, ctx)
	RegisterAppFeature(ctx)

	result, err := ctx.Dispatcher.Dispatch(context.Background(), "listApps", nil)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if _, ok := result.([]driver.InstalledApp); !ok {
		t.Fatalf("Dispatch() result = %#v, want []driver.InstalledApp", result)
	}
}
