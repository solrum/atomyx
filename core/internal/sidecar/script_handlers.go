package sidecar

import (
	"context"
	"encoding/json"

	"dev.atomyx/internal/script"
)

// RegisterScriptFeature wires runScript, stopScript, and isRunning
// against the session's currently selected device. runScript routes
// scenario-shaped YAML to the scenario path, which additionally
// requires params.cwd: the directory each child script's relative
// path is resolved against.
func RegisterScriptFeature(ctx *Context) Handle {
	service := newScriptService(ctx.Session, ctx.Events)

	mustRegister(ctx.Dispatcher, "runScript", func(c context.Context, params json.RawMessage) (any, error) {
		decoded := decodeParams(params)
		yaml, ok := decoded["yaml"].(string)
		if !ok || yaml == "" {
			return nil, NewDispatcherError(ErrInvalidParams, "params.yaml must be a non-empty string", nil)
		}
		if script.IsScenarioYaml(yaml) {
			cwd, ok := decoded["cwd"].(string)
			if !ok || cwd == "" {
				return nil, NewDispatcherError(ErrInvalidParams, "scenario runs require params.cwd (directory of the scenario file)", nil)
			}
			return service.RunScenario(c, yaml, cwd)
		}
		return service.Run(c, yaml)
	})
	mustRegister(ctx.Dispatcher, "stopScript", func(context.Context, json.RawMessage) (any, error) {
		service.Stop()
		return nil, nil
	})
	mustRegister(ctx.Dispatcher, "isRunning", func(context.Context, json.RawMessage) (any, error) {
		return isRunningResult{Running: service.IsRunning()}, nil
	})
	return Handle{}
}
