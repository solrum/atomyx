package mcp

import (
	"context"
	"encoding/json"
	"errors"

	"dev.atomyx/internal/orchestra"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultWaitForElementTimeoutMs = 5000

var waitForElementInputSchema = jsObject(map[string]any{
	"selector":       selectorInputSchema(),
	"timeoutMs":      jsNumber(),
	"pollIntervalMs": jsNumber(),
}, "selector")

var waitForElementSchema = compileToolSchema("wait_for_element", waitForElementInputSchema)

type waitForElementArgs struct {
	Selector       orchestra.Selector `json:"selector"`
	TimeoutMs      *int               `json:"timeoutMs,omitempty"`
	PollIntervalMs *int               `json:"pollIntervalMs,omitempty"`
}

// registerWaitForElementTool adds the wait_for_element tool: polls
// the UI hierarchy until selector matches or the deadline expires,
// returning the same detail shape as find_element on success.
func registerWaitForElementTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "wait_for_element",
		Description: "Poll the UI hierarchy until an element matching the selector appears, " +
			"or the timeout expires. Use after a tap that triggers navigation: " +
			"wait_for_element({selector: {text: 'Welcome'}, timeoutMs: 5000}). " +
			"Returns the same shape as find_element on success (id, text, label, " +
			"value, role, bounds, center, enabled, clickable) or " +
			"{found: false, reason, elapsedMs, polls} on timeout.",
		InputSchema: waitForElementInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(waitForElementSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("wait_for_element", err), nil
		}
		var args waitForElementArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("wait_for_element", err), nil
		}
		timeoutMs := defaultWaitForElementTimeoutMs
		if args.TimeoutMs != nil {
			timeoutMs = *args.TimeoutMs
		}
		pollIntervalMs := 0
		if args.PollIntervalMs != nil {
			pollIntervalMs = *args.PollIntervalMs
		}

		orch, err := orchestraOrFail(ctx, tc)
		if err != nil {
			return errorResult(err), nil
		}
		cursors, err := orch.WaitFor(ctx, args.Selector, orchestra.WaitOptions{
			TimeoutMs:      timeoutMs,
			PollIntervalMs: pollIntervalMs,
		})
		if err != nil {
			var timeoutErr *orchestra.FindTimeoutError
			if errors.As(err, &timeoutErr) {
				return jsonToolResult(map[string]any{
					"found":     false,
					"reason":    timeoutErr.Error(),
					"elapsedMs": timeoutErr.ElapsedMs,
					"polls":     timeoutErr.PollCount,
				})
			}
			return errorResult(err), nil
		}
		return jsonToolResult(resolveElementDetails(cursors[0]))
	})
}
