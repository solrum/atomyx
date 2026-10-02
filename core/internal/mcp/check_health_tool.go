package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var checkHealthInputSchema = jsEmptyObject()

var checkHealthSchema = compileToolSchema("check_health", checkHealthInputSchema)

// registerCheckHealthTool adds the check_health tool: reports whether
// a device is currently bound and, when one is, whether it answers a
// quick liveness check, without running a flow or touching app
// state. An unreachable device is still a successfully answered
// check, so this tool reports isError:true only when the check
// itself could not run.
func registerCheckHealthTool(server *mcp.Server, tc *ToolContext) {
	addObserveOnlyTool(server, tc.Traits, &mcp.Tool{
		Name: "check_health",
		Description: "Report whether a device is currently bound to this session and, " +
			"when one is, whether it answers a quick liveness check. Returns " +
			"selected and healthy booleans, a state (unselected, idle, running, released or lost), plus a code and remedy when unhealthy. " +
			"Does not run a flow or change app state. Call this to diagnose a " +
			"stalled session before retrying an action, instead of guessing from " +
			"a timed-out call.",
		InputSchema: checkHealthInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(checkHealthSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("check_health", err), nil
		}
		report := tc.Session.CheckHealth(ctx)
		return jsonToolResult(report)
	})
}
