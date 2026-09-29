package sidecar

import (
	"context"
	"encoding/json"
	"fmt"

	"dev.atomyx/internal/driver"
)

// RegisterAppFeature wires listApps, launchApp, and
// currentForeground against the session's currently selected device.
func RegisterAppFeature(ctx *Context) Handle {
	service := newAppService(ctx.Session)

	mustRegister(ctx.Dispatcher, "listApps", func(c context.Context, _ json.RawMessage) (any, error) {
		return service.List(c)
	})
	mustRegister(ctx.Dispatcher, "launchApp", func(c context.Context, params json.RawMessage) (any, error) {
		p := decodeParams(params)
		bundleID, ok := p["bundleId"].(string)
		if !ok || bundleID == "" {
			return nil, NewDispatcherError(ErrInvalidParams, "params.bundleId must be a non-empty string", nil)
		}
		if err := service.Launch(c, bundleID, launchArgsFromParams(p)); err != nil {
			return nil, err
		}
		return nil, nil
	})
	mustRegister(ctx.Dispatcher, "currentForeground", func(c context.Context, _ json.RawMessage) (any, error) {
		return service.CurrentForeground(c)
	})
	return Handle{}
}

func launchArgsFromParams(p map[string]any) driver.LaunchArgs {
	var args driver.LaunchArgs
	if raw, ok := p["args"].([]any); ok {
		// A caller is expected to send only strings here, but nothing
		// downstream validates that. An element that isn't a string is
		// forwarded rather than dropped, so a malformed request fails
		// where the value is actually used instead of silently losing
		// entries here.
		args.Args = make([]string, len(raw))
		for i, v := range raw {
			if s, ok := v.(string); ok {
				args.Args[i] = s
			} else {
				args.Args[i] = fmt.Sprint(v)
			}
		}
	}
	if raw, ok := p["env"].(map[string]any); ok {
		env := make(map[string]string, len(raw))
		for k, v := range raw {
			if s, ok := v.(string); ok {
				env[k] = s
			}
		}
		args.Environment = env
	}
	if noReset, ok := p["noReset"].(bool); ok {
		args.NoReset = &noReset
	}
	return args
}
