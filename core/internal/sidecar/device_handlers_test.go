package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"dev.atomyx/internal/device"
)

func TestDevice_ListDevices_ReturnsProbeSnapshot(t *testing.T) {
	ctx := newTestContext()
	probe := device.NewStaticProbe([]device.Descriptor{mockDescriptor})
	RegisterDeviceFeature(ctx, probe, device.NewMockDriverFactory())

	result, err := ctx.Dispatcher.Dispatch(context.Background(), "listDevices", nil)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	got, ok := result.([]device.Descriptor)
	if !ok || len(got) != 1 || got[0].ID != mockDescriptor.ID {
		t.Fatalf("Dispatch() result = %#v, want [%+v]", result, mockDescriptor)
	}
}

func TestDevice_SelectDevice_MissingID_ReturnsInvalidParams(t *testing.T) {
	ctx := newTestContext()
	RegisterDeviceFeature(ctx, device.NewStaticProbe(), device.NewMockDriverFactory())

	_, err := ctx.Dispatcher.Dispatch(context.Background(), "selectDevice", json.RawMessage(`{}`))
	var dispatchErr *DispatcherError
	if !errors.As(err, &dispatchErr) || dispatchErr.Code != ErrInvalidParams {
		t.Fatalf("Dispatch() error = %v, want InvalidParams", err)
	}
}

func TestDevice_SelectDevice_UnknownID_ReturnsNotFoundMessage(t *testing.T) {
	ctx := newTestContext()
	RegisterDeviceFeature(ctx, device.NewStaticProbe([]device.Descriptor{}, []device.Descriptor{}), device.NewMockDriverFactory())

	params, _ := json.Marshal(map[string]string{"id": "ghost"})
	_, err := ctx.Dispatcher.Dispatch(context.Background(), "selectDevice", params)
	if err == nil || err.Error() != `Device "ghost" not found` {
		t.Fatalf("Dispatch() error = %v, want %q", err, `Device "ghost" not found`)
	}
	var dispatchErr *DispatcherError
	if errors.As(err, &dispatchErr) {
		t.Fatalf("Dispatch() error = %v, want a plain error (wire InternalError), not a DispatcherError", err)
	}
}

func TestDevice_SelectDevice_KnownID_InstallsSessionDevice(t *testing.T) {
	ctx := newTestContext()
	selectMockDevice(t, ctx)

	selection := ctx.Session.Device()
	if selection == nil || selection.ID != mockDescriptor.ID {
		t.Fatalf("Session.Device() = %+v, want ID %q", selection, mockDescriptor.ID)
	}
	if !selection.Driver.IsConnected() {
		t.Fatal("selected driver is not connected")
	}
}

func TestDevice_DeselectDevice_ClearsSessionDevice(t *testing.T) {
	ctx := newTestContext()
	selectMockDevice(t, ctx)

	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "deselectDevice", nil); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if ctx.Session.Device() != nil {
		t.Fatalf("Session.Device() = %+v, want nil after deselectDevice", ctx.Session.Device())
	}
}
