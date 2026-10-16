package mcp

import (
	"context"
	"encoding/json"
	"strconv"

	"dev.atomyx/internal/orchestra"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var launchAppInputSchema = jsObject(map[string]any{
	"appId": jsStringDesc("Cross-platform app identifier (bundle id / package name). " +
		"Use the string reported by `list_apps`."),
	"mode": launchModeSchema(),
	"arguments": map[string]any{
		"type":                 "object",
		"additionalProperties": jsAnyOf(jsString(), jsNumber(), jsBoolean()),
		"description": "Named values handed to the app at launch, each a string, number or boolean " +
			"delivered as text. The app reads them at startup. They apply only when the launch " +
			"starts the app, so a launch with `attach` that finds the app running ignores them. " +
			"A launch that carries arguments is never skipped as a repeat.",
	},
	"permissions": permissionsSchema(),
	"link": jsStringDesc("A URL, deep link or universal link to open in the app instead of " +
		"starting it on its home screen, for example `myapp://orders/42`. The app opens on the " +
		"link's destination. It cannot be combined with `arguments` or a `mode` other than " +
		"`default`. A device that cannot open links fails with `LINK_UNSUPPORTED`, and a link " +
		"that no app opens fails with `LINK_NOT_OPENED`. Success means the device accepted " +
		"the link, so wait for an element on the destination before acting."),
}, "appId")

var launchAppSchema = compileToolSchema("launch_app", launchAppInputSchema)

type launchAppArgs struct {
	AppID       string            `json:"appId"`
	Mode        string            `json:"mode"`
	Arguments   map[string]any    `json:"arguments"`
	Link        string            `json:"link"`
	Permissions map[string]string `json:"permissions"`
}

// launchArgumentValues renders each validated argument value as the text
// the app receives. Numbers print without an exponent or a trailing
// fraction when they are whole.
func launchArgumentValues(arguments map[string]any) map[string]string {
	if len(arguments) == 0 {
		return nil
	}
	out := make(map[string]string, len(arguments))
	for name, value := range arguments {
		switch v := value.(type) {
		case string:
			out[name] = v
		case bool:
			out[name] = strconv.FormatBool(v)
		case float64:
			out[name] = strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	return out
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
			"pass mode `fresh` to force-stop and relaunch it with its data kept. " +
			"Pass `arguments` to hand named values to the app at launch, for " +
			"example to start it on a given screen. Pass `link` to open a deep link " +
			"or URL in the app, which lands it on the link's destination. Pass `permissions` " +
			"to grant or revoke app permissions before it starts.",
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
		opts := orchestra.LaunchOptions{Mode: launchModeOf(args.Mode), Arguments: launchArgumentValues(args.Arguments), Link: args.Link, Permissions: launchPermissions(args.Permissions)}
		if err := orch.LaunchAppWithOptions(ctx, args.AppID, opts); err != nil {
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
