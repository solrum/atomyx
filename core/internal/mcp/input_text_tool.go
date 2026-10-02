package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf16"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const eraseTextUpperBound = 999

var inputTextInputSchema = jsObject(map[string]any{
	"selector":   selectorInputSchema(),
	"x":          jsNumber(),
	"y":          jsNumber(),
	"text":       jsString(),
	"clearFirst": jsBoolean(),
}, "text")

var inputTextSchema = compileToolSchema("input_text", inputTextInputSchema)

type inputTextArgs struct {
	Selector   *orchestra.Selector `json:"selector,omitempty"`
	X          *float64            `json:"x,omitempty"`
	Y          *float64            `json:"y,omitempty"`
	Text       string              `json:"text"`
	ClearFirst *bool               `json:"clearFirst,omitempty"`
}

// registerInputTextTool adds the input_text tool: the selector path
// runs the full Orchestra pipeline (scroll-into-view, obscurement,
// tap-to-focus, optional clear, type); the coordinate path taps then
// types directly, skipping scroll and obscurement.
func registerInputTextTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "input_text",
		Description: "Type text into an input field. Pass either {selector, text} for full " +
			"pipeline (find + scroll-into-view + tap-to-focus + optional clear + " +
			"type) or {x, y, text} for coordinate-based input. Set " +
			"clearFirst=false to append instead of overwrite (default clears).",
		InputSchema: inputTextInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, errResult := decodeInputTextArgs(req.Params.Arguments)
		if errResult != nil {
			return errResult, nil
		}

		orch, err := orchestraOrFail(ctx, tc)
		if err != nil {
			return errorResult(err), nil
		}

		if args.Selector != nil {
			result, err := orch.InputText(ctx, *args.Selector, args.Text, orchestra.InputTextOptions{ClearFirst: args.ClearFirst})
			if err != nil {
				return errorResult(err), nil
			}
			return jsonToolResult(result)
		}

		point := wire.Point{X: *args.X, Y: *args.Y}
		if err := orch.TapAt(ctx, point); err != nil {
			return errorResult(err), nil
		}
		if args.ClearFirst == nil || *args.ClearFirst {
			// A driver without erase support is not an action-level
			// failure here: the caller already knows where the field
			// is and can clear it explicitly if this is a no-op.
			_ = orch.EraseText(ctx, eraseTextUpperBound)
		}
		if err := orch.TypeText(ctx, args.Text); err != nil {
			return errorResult(err), nil
		}
		return jsonToolResult(map[string]any{
			"ok":     true,
			"detail": fmt.Sprintf("typed %d char(s) at (%s,%s)", len(utf16.Encode([]rune(args.Text))), formatNumber(*args.X), formatNumber(*args.Y)),
		})
	})
}

func decodeInputTextArgs(raw json.RawMessage) (inputTextArgs, *mcp.CallToolResult) {
	if err := validateToolArguments(inputTextSchema, raw); err != nil {
		return inputTextArgs{}, invalidArgumentsResult("input_text", err)
	}
	var args inputTextArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return inputTextArgs{}, invalidArgumentsResult("input_text", err)
	}
	if args.Selector == nil && (args.X == nil || args.Y == nil) {
		return inputTextArgs{}, invalidArgumentsResult("input_text", fmt.Errorf("must provide either a selector or both x and y"))
	}
	return args, nil
}
