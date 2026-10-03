package mcp

import (
	"context"
	"encoding/json"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/diag"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var selectDeviceInputSchema = jsObject(map[string]any{
	"platform": jsAnyOf(jsConst("ios"), jsConst("android")),
	"id":       jsStringDesc("Device identifier as reported by list_devices."),
	"kind":     jsAnyOf(jsConst("simulator"), jsConst("device")),
	"port":     jsNumber(),
}, "platform", "id")

var selectDeviceSchema = compileToolSchema("select_device", selectDeviceInputSchema)

type selectDeviceArgs struct {
	Platform device.Platform `json:"platform"`
	ID       string          `json:"id"`
	Kind     *SelectKind     `json:"kind,omitempty"`
	Port     *int            `json:"port,omitempty"`
}

type activeDeviceWire struct {
	Platform    device.Platform `json:"platform"`
	ID          string          `json:"id"`
	Kind        SelectKind      `json:"kind,omitempty"`
	ConnectedAt int64           `json:"connectedAt"`
}

// registerSelectDeviceTool adds the select_device tool: binds the
// session to a device so subsequent tools drive it. Calling it again
// mid-session disconnects the previous driver and wires a new one.
func registerSelectDeviceTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "select_device",
		Description: "Bind the MCP session to a device so subsequent tools (launch_app, " +
			"tap, get_ui_tree, etc.) operate on it. Enumerate candidates with " +
			"list_devices first. Calling this again mid-session switches to a " +
			"different device. The previous driver is disconnected cleanly. A device " +
			"that another session holds idle is taken over from it, and a device " +
			"that session is using fails with DEVICE_HELD.",
		InputSchema: selectDeviceInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(selectDeviceSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("select_device", err), nil
		}
		var args selectDeviceArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("select_device", err), nil
		}
		kind := SelectKind("")
		if args.Kind != nil {
			kind = *args.Kind
		}
		port := 0
		if args.Port != nil {
			port = *args.Port
		}

		active, err := tc.Session.Select(ctx, SelectDeviceInput{
			Platform: args.Platform,
			ID:       args.ID,
			Kind:     kind,
			Port:     port,
		})
		if err != nil {
			code, reason, hint := diag.Describe(err)
			payload := map[string]any{"ok": false, "reason": reason, "code": code}
			if hint != "" {
				payload["hint"] = hint
			}
			return jsonToolResult(payload)
		}
		return jsonToolResult(map[string]any{
			"ok": true,
			"active": activeDeviceWire{
				Platform:    active.Platform,
				ID:          active.ID,
				Kind:        active.Kind,
				ConnectedAt: active.ConnectedAt,
			},
		})
	})
}

var disconnectDeviceInputSchema = jsEmptyObject()

var disconnectDeviceSchema = compileToolSchema("disconnect_device", disconnectDeviceInputSchema)

type wasActiveWire struct {
	Platform device.Platform `json:"platform"`
	ID       string          `json:"id"`
}

// registerDisconnectDeviceTool adds the disconnect_device tool: tears
// down the active driver and leaves the session idle. Idempotent, since
// calling it with no active device still reports ok:true.
func registerDisconnectDeviceTool(server *mcp.Server, tc *ToolContext) {
	addObserveOnlyTool(server, tc.Traits, &mcp.Tool{
		Name: "disconnect_device",
		Description: "Disconnect the currently-active device and leave the session idle. " +
			"Subsequent tool calls that need a device will fail until " +
			"select_device is called again. Returns ok:true even when no device " +
			"was bound (idempotent).",
		InputSchema: disconnectDeviceInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(disconnectDeviceSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("disconnect_device", err), nil
		}
		wasActive := tc.Session.Current()
		if err := tc.Session.Disconnect(ctx); err != nil {
			code, reason, hint := diag.Describe(err)
			payload := map[string]any{"ok": false, "reason": reason, "code": code}
			if hint != "" {
				payload["hint"] = hint
			}
			return jsonToolResult(payload)
		}
		var wasActiveOut *wasActiveWire
		if wasActive != nil {
			wasActiveOut = &wasActiveWire{Platform: wasActive.Platform, ID: wasActive.ID}
		}
		return jsonToolResult(map[string]any{"ok": true, "wasActive": wasActiveOut})
	})
}
