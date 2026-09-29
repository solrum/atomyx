package sidecar

import (
	"context"
	"encoding/base64"
	"testing"

	"dev.atomyx/internal/wire"
)

func TestInspection_NoDeviceSelected_ReturnsNoDeviceSelectedMessage(t *testing.T) {
	ctx := newTestContext()
	RegisterInspectionFeature(ctx)

	for _, method := range []string{"getUiTree", "screenshot"} {
		_, err := ctx.Dispatcher.Dispatch(context.Background(), method, nil)
		if err == nil || err.Error() != "No device selected" {
			t.Fatalf("%s: Dispatch() error = %v, want %q", method, err, "No device selected")
		}
	}
}

func TestInspection_GetUiTree_ReturnsStagedHierarchy(t *testing.T) {
	ctx := newTestContext()
	drv := selectMockDevice(t, ctx)
	RegisterInspectionFeature(ctx)
	drv.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "root"}})

	result, err := ctx.Dispatcher.Dispatch(context.Background(), "getUiTree", nil)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	tree, ok := result.(wire.TreeNodeWire)
	if !ok || tree.Attributes["id"] != "root" {
		t.Fatalf("Dispatch() result = %#v, want tree with id=root", result)
	}
}

func TestInspection_Screenshot_ReturnsBase64OfRawBytes(t *testing.T) {
	ctx := newTestContext()
	selectMockDevice(t, ctx)
	RegisterInspectionFeature(ctx)

	result, err := ctx.Dispatcher.Dispatch(context.Background(), "screenshot", nil)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	got, ok := result.(screenshotResult)
	if !ok {
		t.Fatalf("Dispatch() result = %#v, want screenshotResult", result)
	}
	// The mock driver's Screenshot returns an empty byte slice; the
	// wire contract still base64-encodes whatever bytes come back.
	if got.BytesBase64 != base64.StdEncoding.EncodeToString([]byte{}) {
		t.Fatalf("BytesBase64 = %q, want the base64 of an empty screenshot", got.BytesBase64)
	}
}

func TestInspection_InvalidateUiCache_ForcesFreshCapture(t *testing.T) {
	ctx := newTestContext()
	drv := selectMockDevice(t, ctx)
	RegisterInspectionFeature(ctx)
	drv.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "first"}})

	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "getUiTree", nil); err != nil {
		t.Fatalf("first getUiTree: Dispatch() error = %v", err)
	}
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "invalidateUiCache", nil); err != nil {
		t.Fatalf("invalidateUiCache: Dispatch() error = %v", err)
	}

	drv.StageHierarchy(wire.TreeNodeWire{Attributes: map[string]string{"id": "second"}})
	result, err := ctx.Dispatcher.Dispatch(context.Background(), "getUiTree", nil)
	if err != nil {
		t.Fatalf("second getUiTree: Dispatch() error = %v", err)
	}
	tree, ok := result.(wire.TreeNodeWire)
	if !ok || tree.Attributes["id"] != "second" {
		t.Fatalf("Dispatch() result = %#v, want tree with id=second", result)
	}
}
