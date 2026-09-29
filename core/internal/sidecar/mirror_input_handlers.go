package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"math"
)

// MirrorInputFeatureDeps are the platform-agent readiness providers
// RegisterMirrorInputFeature needs to gate every mirror action. The
// composition root constructs the concrete IosAgentService and
// AndroidAgentService (also passed to RegisterIosAgentFeature and
// RegisterAndroidAgentFeature) and injects them here, so this feature
// depends on narrow read-only interfaces rather than the concrete
// agent services or their provisioning machinery.
type MirrorInputFeatureDeps struct {
	IosAgent     IosAgentStatusProvider
	AndroidAgent AndroidAgentStatusProvider
}

// RegisterMirrorInputFeature wires the ten mirror-canvas input
// methods (ratio-based tap, long-press, swipe, pinch, streaming
// touch, and keyboard input) against the session's currently selected
// device.
func RegisterMirrorInputFeature(ctx *Context, deps MirrorInputFeatureDeps) Handle {
	service := newMirrorInputService(ctx.Session, deps.IosAgent, deps.AndroidAgent)

	mustRegister(ctx.Dispatcher, "mirrorTapRatio", func(c context.Context, params json.RawMessage) (any, error) {
		p := decodeParams(params)
		deviceID, xRatio, yRatio, err := requireDeviceAndRatio(p, "mirrorTapRatio")
		if err != nil {
			return nil, err
		}
		bundleID, _ := p["bundleId"].(string)
		if err := service.TapRatio(c, deviceID, xRatio, yRatio, bundleID); err != nil {
			return nil, err
		}
		return nil, nil
	})

	mustRegister(ctx.Dispatcher, "mirrorLongPressRatio", func(c context.Context, params json.RawMessage) (any, error) {
		p := decodeParams(params)
		deviceID, xRatio, yRatio, err := requireDeviceAndRatio(p, "mirrorLongPressRatio")
		if err != nil {
			return nil, err
		}
		durationMs := optionalDurationMs(p)
		bundleID, _ := p["bundleId"].(string)
		if err := service.LongPressRatio(c, deviceID, xRatio, yRatio, durationMs, bundleID); err != nil {
			return nil, err
		}
		return nil, nil
	})

	mustRegister(ctx.Dispatcher, "mirrorSwipeRatio", func(c context.Context, params json.RawMessage) (any, error) {
		p := decodeParams(params)
		deviceID, ok := p["deviceId"].(string)
		if !ok || deviceID == "" {
			return nil, errors.New("mirrorSwipeRatio requires a non-empty deviceId")
		}
		fromXRatio, fxOk := p["fromXRatio"].(float64)
		fromYRatio, fyOk := p["fromYRatio"].(float64)
		toXRatio, txOk := p["toXRatio"].(float64)
		toYRatio, tyOk := p["toYRatio"].(float64)
		if !fxOk || !fyOk || !txOk || !tyOk ||
			!isFinite(fromXRatio) || !isFinite(fromYRatio) || !isFinite(toXRatio) || !isFinite(toYRatio) {
			return nil, errors.New("mirrorSwipeRatio requires numeric fromXRatio/fromYRatio/toXRatio/toYRatio")
		}
		durationMs := optionalDurationMs(p)
		bundleID, _ := p["bundleId"].(string)
		if err := service.SwipeRatio(c, deviceID, fromXRatio, fromYRatio, toXRatio, toYRatio, durationMs, bundleID); err != nil {
			return nil, err
		}
		return nil, nil
	})

	mustRegister(ctx.Dispatcher, "mirrorStreamingTouchDown", func(c context.Context, params json.RawMessage) (any, error) {
		deviceID, xRatio, yRatio, touchID, err := parseStreamingParams(params)
		if err != nil {
			return nil, err
		}
		if err := service.StreamingDown(c, deviceID, xRatio, yRatio, touchID); err != nil {
			return nil, err
		}
		return nil, nil
	})

	mustRegister(ctx.Dispatcher, "mirrorStreamingTouchMove", func(c context.Context, params json.RawMessage) (any, error) {
		deviceID, xRatio, yRatio, touchID, err := parseStreamingParams(params)
		if err != nil {
			return nil, err
		}
		if err := service.StreamingMove(c, deviceID, xRatio, yRatio, touchID); err != nil {
			return nil, err
		}
		return nil, nil
	})

	mustRegister(ctx.Dispatcher, "mirrorStreamingTouchUp", func(c context.Context, params json.RawMessage) (any, error) {
		deviceID, xRatio, yRatio, touchID, err := parseStreamingParams(params)
		if err != nil {
			return nil, err
		}
		if err := service.StreamingUp(c, deviceID, xRatio, yRatio, touchID); err != nil {
			return nil, err
		}
		return nil, nil
	})

	mustRegister(ctx.Dispatcher, "mirrorInputText", func(c context.Context, params json.RawMessage) (any, error) {
		p := decodeParams(params)
		deviceID, ok := p["deviceId"].(string)
		if !ok || deviceID == "" {
			return nil, errors.New("mirrorInputText requires a non-empty deviceId")
		}
		text, ok := p["text"].(string)
		if !ok {
			return nil, errors.New("mirrorInputText requires a string text")
		}
		if err := service.InputText(c, deviceID, text); err != nil {
			return nil, err
		}
		return nil, nil
	})

	mustRegister(ctx.Dispatcher, "mirrorEraseText", func(c context.Context, params json.RawMessage) (any, error) {
		p := decodeParams(params)
		deviceID, ok := p["deviceId"].(string)
		if !ok || deviceID == "" {
			return nil, errors.New("mirrorEraseText requires a non-empty deviceId")
		}
		count, ok := p["count"].(float64)
		if !ok || !isFinite(count) || count <= 0 {
			return nil, errors.New("mirrorEraseText requires a positive numeric count")
		}
		if err := service.EraseText(c, deviceID, int(count)); err != nil {
			return nil, err
		}
		return nil, nil
	})

	mustRegister(ctx.Dispatcher, "mirrorPressKey", func(c context.Context, params json.RawMessage) (any, error) {
		p := decodeParams(params)
		deviceID, ok := p["deviceId"].(string)
		if !ok || deviceID == "" {
			return nil, errors.New("mirrorPressKey requires a non-empty deviceId")
		}
		key, ok := p["key"].(string)
		if !ok || key == "" {
			return nil, errors.New("mirrorPressKey requires a non-empty key")
		}
		if err := service.PressKey(c, deviceID, key); err != nil {
			return nil, err
		}
		return nil, nil
	})

	mustRegister(ctx.Dispatcher, "mirrorPinch", func(c context.Context, params json.RawMessage) (any, error) {
		p := decodeParams(params)
		deviceID, ok := p["deviceId"].(string)
		if !ok || deviceID == "" {
			return nil, errors.New("mirrorPinch requires a non-empty deviceId")
		}
		centerXRatio, cxOk := p["centerXRatio"].(float64)
		centerYRatio, cyOk := p["centerYRatio"].(float64)
		fromScale, fsOk := p["fromScale"].(float64)
		toScale, tsOk := p["toScale"].(float64)
		if !cxOk || !cyOk || !fsOk || !tsOk ||
			!isFinite(centerXRatio) || !isFinite(centerYRatio) || !isFinite(fromScale) || !isFinite(toScale) {
			return nil, errors.New("mirrorPinch requires numeric centerXRatio, centerYRatio, fromScale, toScale")
		}
		durationMs := optionalDurationMs(p)
		bundleID, _ := p["bundleId"].(string)
		if err := service.PinchRatio(c, deviceID, centerXRatio, centerYRatio, fromScale, toScale, durationMs, bundleID); err != nil {
			return nil, err
		}
		return nil, nil
	})

	return Handle{}
}

// requireDeviceAndRatio validates the deviceId/xRatio/yRatio triple
// shared by mirrorTapRatio and mirrorLongPressRatio.
func requireDeviceAndRatio(p map[string]any, method string) (deviceID string, xRatio, yRatio float64, err error) {
	deviceID, ok := p["deviceId"].(string)
	if !ok || deviceID == "" {
		return "", 0, 0, errors.New(method + " requires a non-empty deviceId")
	}
	xRatio, xOk := p["xRatio"].(float64)
	yRatio, yOk := p["yRatio"].(float64)
	if !xOk || !yOk || !isFinite(xRatio) || !isFinite(yRatio) {
		return "", 0, 0, errors.New(method + " requires numeric xRatio and yRatio")
	}
	return deviceID, xRatio, yRatio, nil
}

// optionalDurationMs reads params.durationMs when present and finite,
// rounds it to the nearest integer, and clamps it to zero so a
// negative value from an external caller can never reach the input
// service as a negative duration. Returns nil when absent so the
// service applies its own default.
func optionalDurationMs(p map[string]any) *int {
	raw, ok := p["durationMs"].(float64)
	if !ok || !isFinite(raw) {
		return nil
	}
	rounded := int(math.Round(raw))
	if rounded < 0 {
		rounded = 0
	}
	return &rounded
}

// parseStreamingParams validates the deviceId/xRatio/yRatio/touchId
// shared by the three streaming touch methods, defaulting touchId to
// 1 when absent.
func parseStreamingParams(params json.RawMessage) (deviceID string, xRatio, yRatio float64, touchID int, err error) {
	p := decodeParams(params)
	deviceID, ok := p["deviceId"].(string)
	if !ok || deviceID == "" {
		return "", 0, 0, 0, errors.New("streaming touch requires a non-empty deviceId")
	}
	xRatio, xOk := p["xRatio"].(float64)
	yRatio, yOk := p["yRatio"].(float64)
	if !xOk || !yOk || !isFinite(xRatio) || !isFinite(yRatio) {
		return "", 0, 0, 0, errors.New("streaming touch requires numeric xRatio and yRatio")
	}
	touchID = 1
	if raw, ok := p["touchId"].(float64); ok && isFinite(raw) {
		touchID = int(math.Round(raw))
	}
	return deviceID, xRatio, yRatio, touchID, nil
}
