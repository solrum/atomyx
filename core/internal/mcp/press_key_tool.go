package mcp

import (
	"context"
	"encoding/json"

	"dev.atomyx/internal/driver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var pressKeyInputSchema = jsObject(map[string]any{
	"key": jsStringDesc("Key name: back, home, enter, tab, escape, delete, etc."),
}, "key")

var pressKeySchema = compileToolSchema("press_key", pressKeyInputSchema)

type pressKeyArgs struct {
	Key string `json:"key"`
}

// registerPressKeyTool adds the press_key tool: a system or
// navigation key press, reported as an ActionResult so the agent can
// react when the current platform has no equivalent primitive.
func registerPressKeyTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "press_key",
		Description: "Press a system or navigation key (back, home, enter, etc.). Some " +
			"keys may return ok=false when the platform has no equivalent " +
			"primitive; the response hint explains the recommended on-screen " +
			"fallback (e.g. tap a Cancel / Close button).",
		InputSchema: pressKeyInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(pressKeySchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("press_key", err), nil
		}
		var args pressKeyArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("press_key", err), nil
		}

		orch, err := orchestraOrFail(ctx, tc)
		if err != nil {
			return errorResult(err), nil
		}
		result, err := orch.PressKey(ctx, driver.KeyCode(args.Key))
		if err != nil {
			return errorResult(err), nil
		}
		return jsonToolResult(result)
	})
}
