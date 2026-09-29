package mcp

import (
	"context"

	"dev.atomyx/internal/driver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var listAppsInputSchema = jsEmptyObject()

var listAppsSchema = compileToolSchema("list_apps", listAppsInputSchema)

type listAppsResult struct {
	Count int                   `json:"count"`
	Apps  []driver.InstalledApp `json:"apps"`
	Note  *string               `json:"note,omitempty"`
}

// registerListAppsTool adds the list_apps tool: enumerates installed
// apps on the active device via the driver.
func registerListAppsTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "list_apps",
		Description: "List apps installed on the current device. Returns app " +
			"identifiers plus display labels. Call before launch_app when you " +
			"need to confirm the exact identifier.",
		InputSchema: listAppsInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(listAppsSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("list_apps", err), nil
		}

		orch, err := orchestraOrFail(tc)
		if err != nil {
			return errorResult(err), nil
		}
		apps, err := orch.ListApps(ctx)
		if err != nil {
			return errorResult(err), nil
		}
		if apps == nil {
			apps = []driver.InstalledApp{}
		}
		result := listAppsResult{Count: len(apps), Apps: apps}
		if len(apps) == 0 {
			note := "No apps returned. Device may have no installed apps, or " +
				"the host enumeration toolchain is not available."
			result.Note = &note
		}
		return jsonToolResult(result)
	})
}
