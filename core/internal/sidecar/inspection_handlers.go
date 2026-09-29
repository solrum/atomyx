package sidecar

import (
	"context"
	"encoding/base64"
	"encoding/json"
)

type screenshotResult struct {
	BytesBase64 string `json:"bytesBase64"`
}

// RegisterInspectionFeature wires getUiTree, screenshot, and
// invalidateUiCache against the session's currently selected device.
func RegisterInspectionFeature(ctx *Context) Handle {
	service := newInspectionService(ctx.Session)

	mustRegister(ctx.Dispatcher, "getUiTree", func(c context.Context, params json.RawMessage) (any, error) {
		fresh, _ := decodeParams(params)["fresh"].(bool)
		return service.GetUiTree(c, fresh)
	})
	mustRegister(ctx.Dispatcher, "screenshot", func(c context.Context, _ json.RawMessage) (any, error) {
		bytes, err := service.Screenshot(c)
		if err != nil {
			return nil, err
		}
		return screenshotResult{BytesBase64: base64.StdEncoding.EncodeToString(bytes)}, nil
	})
	mustRegister(ctx.Dispatcher, "invalidateUiCache", func(context.Context, json.RawMessage) (any, error) {
		service.Invalidate()
		return nil, nil
	})
	return Handle{}
}
