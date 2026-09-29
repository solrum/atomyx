package sidecar

import (
	"context"
	"encoding/json"
	"testing"

	"dev.atomyx/internal/driver"
)

func TestInput_NoDeviceSelected_ReturnsNoDeviceSelectedMessage(t *testing.T) {
	ctx := newTestContext()
	RegisterInputFeature(ctx)

	params, _ := json.Marshal(map[string]float64{"xRatio": 0.5, "yRatio": 0.5})
	_, err := ctx.Dispatcher.Dispatch(context.Background(), "tapRatio", params)
	if err == nil || err.Error() != "No device selected" {
		t.Fatalf("Dispatch() error = %v, want %q", err, "No device selected")
	}
}

func TestInput_TapRatio_NonNumericRatios_ReturnsPlainError(t *testing.T) {
	ctx := newTestContext()
	RegisterInputFeature(ctx)

	_, err := ctx.Dispatcher.Dispatch(context.Background(), "tapRatio", json.RawMessage(`{}`))
	if err == nil || err.Error() != "tapRatio requires numeric xRatio and yRatio" {
		t.Fatalf("Dispatch() error = %v, want %q", err, "tapRatio requires numeric xRatio and yRatio")
	}
}

func TestInput_TapRatio_DispatchesTapAtScaledPoint(t *testing.T) {
	ctx := newTestContext()
	drv := selectMockDevice(t, ctx)
	RegisterInputFeature(ctx)

	params, _ := json.Marshal(map[string]float64{"xRatio": 0.5, "yRatio": 0.5})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "tapRatio", params); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	var tapped bool
	for _, c := range drv.Calls {
		if c.Method == "Tap" {
			tapped = true
		}
	}
	if !tapped {
		t.Fatalf("driver.Calls = %+v, want a Tap call", drv.Calls)
	}
}

func TestInput_TapRatio_WithBundleID_LaunchesBeforeTapping(t *testing.T) {
	ctx := newTestContext()
	drv := selectMockDevice(t, ctx)
	drv.Apps = []driver.InstalledApp{{BundleID: "com.test"}}
	RegisterInputFeature(ctx)

	params, _ := json.Marshal(map[string]any{"xRatio": 0.1, "yRatio": 0.1, "bundleId": "com.test"})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "tapRatio", params); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if drv.RunningApp == nil || *drv.RunningApp != "com.test" {
		t.Fatalf("driver.RunningApp = %v, want com.test", drv.RunningApp)
	}
}
