package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"math"
)

// RegisterInputFeature wires tapRatio against the session's currently
// selected device.
func RegisterInputFeature(ctx *Context) Handle {
	service := newInputService(ctx.Session)

	mustRegister(ctx.Dispatcher, "tapRatio", func(c context.Context, params json.RawMessage) (any, error) {
		p := decodeParams(params)
		xRatio, xOk := p["xRatio"].(float64)
		yRatio, yOk := p["yRatio"].(float64)
		if !xOk || !yOk || !isFinite(xRatio) || !isFinite(yRatio) {
			return nil, errors.New("tapRatio requires numeric xRatio and yRatio")
		}
		bundleID, _ := p["bundleId"].(string)
		if err := service.TapRatio(c, xRatio, yRatio, bundleID); err != nil {
			return nil, err
		}
		return nil, nil
	})
	return Handle{}
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
