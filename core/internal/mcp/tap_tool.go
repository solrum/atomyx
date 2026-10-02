package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var tapInputSchema = jsObject(map[string]any{
	"selector": selectorInputSchema(),
	"x":        jsNumber(),
	"y":        jsNumber(),
})

var tapSchema = compileToolSchema("tap", tapInputSchema)

type tapArgs struct {
	Selector *orchestra.Selector `json:"selector,omitempty"`
	X        *float64            `json:"x,omitempty"`
	Y        *float64            `json:"y,omitempty"`
}

// registerTapTool adds the tap tool: selector-based tap through the
// full Orchestra pipeline (scroll-into-view, obscurement check), or a
// raw coordinate tap when the caller passes {x, y} instead.
func registerTapTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "tap",
		Description: "Tap an element. Pass {selector: {...}} to find and tap the matching " +
			"element with automatic scroll-into-view and obscurement detection, OR " +
			"pass {x, y} to tap raw coordinates (use when you have a center point " +
			"from get_ui_tree or find_element). Returns ok=true on success, or " +
			"ok=false with a reason and optional obscurer info on failure.",
		InputSchema: tapInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, errResult := decodeTapArgs(req.Params.Arguments)
		if errResult != nil {
			return errResult, nil
		}

		orch, err := orchestraOrFail(ctx, tc)
		if err != nil {
			return errorResult(err), nil
		}

		if args.Selector != nil {
			result, err := orch.Tap(ctx, *args.Selector, orchestra.TapOptions{})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonToolResult(result)
		}

		point := wire.Point{X: *args.X, Y: *args.Y}
		if err := orch.TapAt(ctx, point); err != nil {
			return errorResult(err), nil
		}
		return jsonToolResult(map[string]any{
			"ok":     true,
			"detail": fmt.Sprintf("tapped at (%s,%s)", formatNumber(*args.X), formatNumber(*args.Y)),
		})
	})
}

func decodeTapArgs(raw json.RawMessage) (tapArgs, *mcp.CallToolResult) {
	if err := validateToolArguments(tapSchema, raw); err != nil {
		return tapArgs{}, invalidArgumentsResult("tap", err)
	}
	var args tapArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return tapArgs{}, invalidArgumentsResult("tap", err)
	}
	if args.Selector == nil && (args.X == nil || args.Y == nil) {
		return tapArgs{}, invalidArgumentsResult("tap", fmt.Errorf("must provide either a selector or both x and y"))
	}
	return args, nil
}
