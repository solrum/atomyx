package mcp

import (
	"context"
	"encoding/json"

	"dev.atomyx/internal/device"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var listDevicesInputSchema = jsObject(map[string]any{
	"platform": jsAnyOf(jsConst("ios"), jsConst("android"), jsConst("all")),
})

var listDevicesSchema = compileToolSchema("list_devices", listDevicesInputSchema)

type listDevicesArgs struct {
	Platform *string `json:"platform,omitempty"`
}

type discoveredDeviceWire struct {
	Platform device.Platform `json:"platform"`
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	State    device.State    `json:"state"`
	Kind     device.Kind     `json:"kind"`
}

type listDevicesResult struct {
	Count   int                    `json:"count"`
	Devices []discoveredDeviceWire `json:"devices"`
	Hint    *string                `json:"hint,omitempty"`
}

// registerListDevicesTool adds the list_devices tool: enumerates
// connected devices across every supported platform via probe.
func registerListDevicesTool(server *mcp.Server, traits *toolTraits, probe device.Probe) {
	addObserveOnlyTool(server, traits, &mcp.Tool{
		Name: "list_devices",
		Description: "Enumerate connected devices across all supported platforms. Returns " +
			"a flat list with { platform, id, name, state, kind } entries. Use " +
			"this BEFORE starting the MCP server to pick the target device. " +
			"Platforms whose toolchain isn't installed on the host are silently " +
			"skipped.",
		InputSchema: listDevicesInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(listDevicesSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("list_devices", err), nil
		}
		var args listDevicesArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("list_devices", err), nil
		}

		descriptors := probe.Scan(ctx)
		devices := make([]discoveredDeviceWire, 0, len(descriptors))
		for _, d := range descriptors {
			if args.Platform != nil && *args.Platform != "all" && string(d.Platform) != *args.Platform {
				continue
			}
			devices = append(devices, discoveredDeviceWire{
				Platform: d.Platform,
				ID:       d.ID,
				Name:     d.Name,
				State:    d.State,
				Kind:     d.Kind,
			})
		}

		result := listDevicesResult{Count: len(devices), Devices: devices}
		if len(devices) == 0 {
			h := "No devices found. Check that the host platform toolchain is " +
				"installed and a device/simulator is actually running."
			result.Hint = &h
		}
		return jsonToolResult(result)
	})
}
