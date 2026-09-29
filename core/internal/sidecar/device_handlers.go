package sidecar

import (
	"context"
	"encoding/json"

	"dev.atomyx/internal/device"
)

// RegisterDeviceFeature wires listDevices, selectDevice, and
// deselectDevice against probe and factory. The composition root
// supplies the concrete Probe and Factory; a test supplies
// device.StaticProbe and device.MockDriverFactory instead.
func RegisterDeviceFeature(ctx *Context, probe device.Probe, factory device.Factory) Handle {
	service := newDeviceService(probe, factory, ctx.Session, ctx.Events)

	mustRegister(ctx.Dispatcher, "listDevices", func(c context.Context, _ json.RawMessage) (any, error) {
		return service.List(c), nil
	})
	mustRegister(ctx.Dispatcher, "selectDevice", func(c context.Context, params json.RawMessage) (any, error) {
		id, ok := decodeParams(params)["id"].(string)
		if !ok || id == "" {
			return nil, NewDispatcherError(ErrInvalidParams, "params.id must be a non-empty string", nil)
		}
		return service.Select(c, id)
	})
	mustRegister(ctx.Dispatcher, "deselectDevice", func(context.Context, json.RawMessage) (any, error) {
		service.Deselect()
		return nil, nil
	})
	return Handle{}
}
