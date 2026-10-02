package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var swipeInputSchema = jsObject(map[string]any{
	"direction":  jsAnyOf(jsConst("up"), jsConst("down"), jsConst("left"), jsConst("right")),
	"fraction":   jsNumber(),
	"fromX":      jsNumber(),
	"fromY":      jsNumber(),
	"toX":        jsNumber(),
	"toY":        jsNumber(),
	"durationMs": jsNumber(),
})

var swipeSchema = compileToolSchema("swipe", swipeInputSchema)

type swipeArgs struct {
	Direction  *orchestra.Direction `json:"direction,omitempty"`
	Fraction   *float64             `json:"fraction,omitempty"`
	FromX      *float64             `json:"fromX,omitempty"`
	FromY      *float64             `json:"fromY,omitempty"`
	ToX        *float64             `json:"toX,omitempty"`
	ToY        *float64             `json:"toY,omitempty"`
	DurationMs *int                 `json:"durationMs,omitempty"`
}

// registerSwipeTool adds the swipe tool: a centered directional
// scroll, or a precise two-point swipe when the caller passes raw
// from/to coordinates instead.
func registerSwipeTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "swipe",
		Description: "Swipe the screen. Pass {direction: 'up'|'down'|'left'|'right'} for a " +
			"centered directional scroll (default 60% of screen), or {fromX, fromY, " +
			"toX, toY} for a precise two-point swipe. durationMs default 200.",
		InputSchema: swipeInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, errResult := decodeSwipeArgs(req.Params.Arguments)
		if errResult != nil {
			return errResult, nil
		}

		orch, err := orchestraOrFail(ctx, tc)
		if err != nil {
			return errorResult(err), nil
		}

		if args.Direction != nil {
			opts := orchestra.SwipeDirectionOptions{DurationMs: args.DurationMs, Fraction: args.Fraction}
			if err := orch.SwipeDirection(ctx, *args.Direction, opts); err != nil {
				return errorResult(err), nil
			}
			return jsonToolResult(map[string]any{"ok": true, "direction": *args.Direction})
		}

		from := wire.Point{X: *args.FromX, Y: *args.FromY}
		to := wire.Point{X: *args.ToX, Y: *args.ToY}
		durationMs := orchestra.DefaultSwipeDurationMs
		if args.DurationMs != nil {
			durationMs = *args.DurationMs
		}
		if err := orch.SwipeAt(ctx, from, to, durationMs); err != nil {
			return errorResult(err), nil
		}
		return jsonToolResult(map[string]any{
			"ok": true,
			"detail": fmt.Sprintf("swiped from (%s,%s) to (%s,%s)",
				formatNumber(*args.FromX), formatNumber(*args.FromY),
				formatNumber(*args.ToX), formatNumber(*args.ToY)),
		})
	})
}

func decodeSwipeArgs(raw json.RawMessage) (swipeArgs, *mcp.CallToolResult) {
	if err := validateToolArguments(swipeSchema, raw); err != nil {
		return swipeArgs{}, invalidArgumentsResult("swipe", err)
	}
	var args swipeArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return swipeArgs{}, invalidArgumentsResult("swipe", err)
	}
	hasCoords := args.FromX != nil && args.FromY != nil && args.ToX != nil && args.ToY != nil
	if args.Direction == nil && !hasCoords {
		return swipeArgs{}, invalidArgumentsResult("swipe", fmt.Errorf("must provide either a direction or all of fromX, fromY, toX, and toY"))
	}
	return args, nil
}
