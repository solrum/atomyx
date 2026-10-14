package mcp

import (
	"context"
	"encoding/json"

	"dev.atomyx/internal/orchestra"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var launchAppInputSchema = jsObject(map[string]any{
	"appId": jsStringDesc("Cross-platform app identifier (bundle id / package name). " +
		"Use the string reported by `list_apps`."),
	"mode": launchModeSchema(),
}, "appId")

var launchAppSchema = compileToolSchema("launch_app", launchAppInputSchema)

type launchAppArgs struct {
	AppID string `json:"appId"`
	Mode  string `json:"mode"`
}

// launchModeSchema accepts the three launch modes. A caller that omits
// the property gets the default mode.
func launchModeSchema() map[string]any {
	schema := jsAnyOf(jsConst(string(orchestra.LaunchModeDefault)), jsConst(string(orchestra.LaunchModeFresh)), jsConst(string(orchestra.LaunchModeAttach)))
	schema["description"] = "How to start the app. `default` (when omitted) starts it without promising anything about process state, and a second `default` launch of the same app in the same session is skipped. `fresh` force-stops the app and launches it again, keeping its data, and forces that restart even when the app was already launched. `attach` reuses a running instance, and a repeat attach launch is skipped."
	return schema
}

// registerLaunchAppTool adds the launch_app tool: brings appId to the
// foreground on the active device.
func registerLaunchAppTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "launch_app",
		Description: "Launch an app on the connected device by its app identifier. The " +
			"driver brings the app to the foreground; subsequent tool calls " +
			"operate on its UI. Requires an active device, so call `select_device` " +
			"first. Use `list_apps` to discover available identifiers. A second " +
			"default launch of the same app in the same session is skipped, so " +
			"pass mode `fresh` to force-stop and relaunch it with its data kept.",
		InputSchema: launchAppInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(launchAppSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("launch_app", err), nil
		}
		var args launchAppArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("launch_app", err), nil
		}

		orch, err := orchestraOrFail(ctx, tc)
		if err != nil {
			return errorResult(err), nil
		}
		if err := orch.LaunchAppWithMode(ctx, args.AppID, launchModeOf(args.Mode)); err != nil {
			return errorResult(err), nil
		}
		return jsonToolResult(map[string]any{"ok": true, "appId": args.AppID})
	})
}

// launchModeOf converts the validated mode argument to an Orchestra
// launch mode, treating an omitted value as the default mode.
func launchModeOf(mode string) orchestra.LaunchMode {
	if mode == "" {
		return orchestra.LaunchModeDefault
	}
	return orchestra.LaunchMode(mode)
}
