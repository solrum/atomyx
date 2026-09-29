package sidecar

import (
	"context"
	"encoding/json"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver/mock"
)

// mockDescriptor is the single device every device-free test selects
// against.
var mockDescriptor = device.Descriptor{
	ID:       "mock-1",
	Platform: device.PlatformAndroid,
	Name:     "Mock Device",
	Kind:     device.KindEmulator,
	State:    device.StateOnline,
}

// selectMockDevice registers the device feature over a StaticProbe
// staged with mockDescriptor and a MockDriverFactory, selects it, and
// returns the underlying mock.Driver so a test can stage hierarchies,
// screenshots, and assert on recorded calls.
func selectMockDevice(t *testing.T, ctx *Context) *mock.Driver {
	t.Helper()
	probe := device.NewStaticProbe([]device.Descriptor{mockDescriptor}, []device.Descriptor{mockDescriptor})
	factory := device.NewMockDriverFactory()
	RegisterDeviceFeature(ctx, probe, factory)

	params, _ := json.Marshal(map[string]string{"id": mockDescriptor.ID})
	if _, err := ctx.Dispatcher.Dispatch(context.Background(), "selectDevice", params); err != nil {
		t.Fatalf("selectDevice failed: %v", err)
	}

	selection := ctx.Session.Device()
	if selection == nil {
		t.Fatalf("selectDevice did not install a session device")
	}
	drv, ok := selection.Driver.(*mock.Driver)
	if !ok {
		t.Fatalf("session device driver = %T, want *mock.Driver", selection.Driver)
	}
	return drv
}
