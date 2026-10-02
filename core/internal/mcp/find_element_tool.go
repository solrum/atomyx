package mcp

import (
	"context"
	"encoding/json"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var findElementInputSchema = selectorInputSchema()

var findElementSchema = compileToolSchema("find_element", findElementInputSchema)

type elementDetails struct {
	Found     bool         `json:"found"`
	ID        *string      `json:"id,omitempty"`
	Text      *string      `json:"text,omitempty"`
	Label     *string      `json:"label,omitempty"`
	Value     *string      `json:"value,omitempty"`
	Role      *string      `json:"role,omitempty"`
	Bounds    *wire.Bounds `json:"bounds"`
	Center    *wire.Point  `json:"center"`
	Enabled   *bool        `json:"enabled,omitempty"`
	Clickable *bool        `json:"clickable,omitempty"`
	Secure    *bool        `json:"secure,omitempty"`
}

// resolveElementDetails builds the find_element / wait_for_element
// success shape from a matched cursor.
func resolveElementDetails(cursor *orchestra.TreeCursor) elementDetails {
	attrs := cursor.Node.Attributes
	details := elementDetails{
		Found:     true,
		ID:        attrPtr(attrs, orchestra.AttrID),
		Text:      attrPtr(attrs, orchestra.AttrText),
		Label:     attrPtr(attrs, orchestra.AttrLabel),
		Value:     attrPtr(attrs, orchestra.AttrValue),
		Role:      attrPtr(attrs, orchestra.AttrRole),
		Enabled:   cursor.Node.Enabled,
		Clickable: cursor.Node.Clickable,
		Secure:    secureFlag(cursor.Node),
	}
	if bounds, ok := orchestra.ParseBounds(attrs[orchestra.AttrBounds]); ok {
		details.Bounds = &bounds
		center := orchestra.BoundsCenter(bounds)
		details.Center = &center
	}
	return details
}

// registerFindElementTool adds the find_element tool: a single-shot
// selector lookup that returns enough detail to tap the element
// directly or fall back to its center coordinates.
func registerFindElementTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "find_element",
		Description: "Find a single element matching a selector. Returns the element's id, " +
			"text, label, role, bounds, and tap center point. Use this to verify " +
			"an element exists or to get coordinates when a selector-based tap " +
			"is failing for some reason. Selector priority: id > label > text > " +
			"value > hint, with role/enabled/clickable/focused as constraints.",
		InputSchema: findElementInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(findElementSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("find_element", err), nil
		}
		var selector orchestra.Selector
		if err := json.Unmarshal(req.Params.Arguments, &selector); err != nil {
			return invalidArgumentsResult("find_element", err), nil
		}

		orch, err := orchestraOrFail(ctx, tc)
		if err != nil {
			return errorResult(err), nil
		}
		cursor, err := orch.FindOne(ctx, selector)
		if err != nil {
			return errorResult(err), nil
		}
		if cursor == nil {
			return jsonToolResult(map[string]any{"found": false})
		}
		return jsonToolResult(resolveElementDetails(cursor))
	})
}
