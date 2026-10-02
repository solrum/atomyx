package mcp

import (
	"context"
	"encoding/json"

	"dev.atomyx/internal/driver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var launchAppInputSchema = jsObject(map[string]any{
	"appId": jsStringDesc("Cross-platform app identifier (bundle id / package name). " +
		"Use the string reported by `list_apps`."),
}, "appId")

var launchAppSchema = compileToolSchema("launch_app", launchAppInputSchema)

type launchAppArgs struct {
	AppID string `json:"appId"`
}

// registerLaunchAppTool adds the launch_app tool: brings appId to the
// foreground on the active device.
func registerLaunchAppTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "launch_app",
		Description: "Launch an app on the connected device by its app identifier. The " +
			"driver brings the app to the foreground; subsequent tool calls " +
			"operate on its UI. Requires an active device, so call `select_device` " +
			"first. Use `list_apps` to discover available identifiers.",
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
		if err := orch.LaunchApp(ctx, args.AppID, driver.LaunchArgs{}); err != nil {
			return errorResult(err), nil
		}
		return jsonToolResult(map[string]any{"ok": true, "appId": args.AppID})
	})
}
