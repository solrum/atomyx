package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var getUiTreeInputSchema = jsObject(map[string]any{
	"limit":                 jsNumber(),
	"includeSystemOverlays": jsBoolean(),
})

var getUiTreeSchema = compileToolSchema("get_ui_tree", getUiTreeInputSchema)

type getUiTreeArgs struct {
	Limit                 *int  `json:"limit,omitempty"`
	IncludeSystemOverlays *bool `json:"includeSystemOverlays,omitempty"`
}

type flatNode struct {
	Role      string            `json:"role"`
	ID        *string           `json:"id,omitempty"`
	Text      *string           `json:"text,omitempty"`
	Label     *string           `json:"label,omitempty"`
	Hint      *string           `json:"hint,omitempty"`
	Value     *string           `json:"value,omitempty"`
	Bounds    *string           `json:"bounds,omitempty"`
	Enabled   *bool             `json:"enabled,omitempty"`
	Clickable *bool             `json:"clickable,omitempty"`
	Focused   *bool             `json:"focused,omitempty"`
	Selected  *bool             `json:"selected,omitempty"`
	Checked   *bool             `json:"checked,omitempty"`
	Visible   *bool             `json:"visible,omitempty"`
	Ext       map[string]string `json:"ext,omitempty"`
	Depth     int               `json:"depth"`
}

func attrPtr(attrs map[string]string, key string) *string {
	if v, ok := attrs[key]; ok {
		return &v
	}
	return nil
}

// flattenTree renders node and its descendants as a depth-annotated
// flat list, in pre-order. It performs no filtering: the caller
// passes an already-shaped tree (see orchestra.FilterSystemOverlays
// for the system-overlay case get_ui_tree uses).
func flattenTree(node wire.TreeNodeWire, depth int) []flatNode {
	var out []flatNode
	var ext map[string]string
	for key, value := range node.Attributes {
		if strings.HasPrefix(key, "ext:") {
			if ext == nil {
				ext = make(map[string]string)
			}
			ext[key] = value
		}
	}
	role := node.Attributes[orchestra.AttrRole]
	if role == "" {
		role = "other"
	}
	out = append(out, flatNode{
		Role:      role,
		ID:        attrPtr(node.Attributes, orchestra.AttrID),
		Text:      attrPtr(node.Attributes, orchestra.AttrText),
		Label:     attrPtr(node.Attributes, orchestra.AttrLabel),
		Hint:      attrPtr(node.Attributes, orchestra.AttrHint),
		Value:     attrPtr(node.Attributes, orchestra.AttrValue),
		Bounds:    attrPtr(node.Attributes, orchestra.AttrBounds),
		Enabled:   node.Enabled,
		Clickable: node.Clickable,
		Focused:   node.Focused,
		Selected:  node.Selected,
		Checked:   node.Checked,
		Visible:   node.Visible,
		Ext:       ext,
		Depth:     depth,
	})
	for _, child := range node.Children {
		out = append(out, flattenTree(child, depth+1)...)
	}
	return out
}

type getUiTreeResult struct {
	Total     int        `json:"total"`
	Returned  int        `json:"returned"`
	Truncated bool       `json:"truncated"`
	Nodes     []flatNode `json:"nodes"`
}

// registerGetUiTreeTool adds the get_ui_tree tool: a flat,
// depth-annotated snapshot of the current hierarchy.
func registerGetUiTreeTool(server *mcp.Server, tc *ToolContext) {
	server.AddTool(&mcp.Tool{
		Name: "get_ui_tree",
		Description: "Capture the current UI hierarchy as a flat depth-annotated list of " +
			"elements. Each entry includes role, id, text, label, bounds, and " +
			"interactive state. THIS IS YOUR PRIMARY OBSERVATION TOOL, so call it " +
			"FIRST after launch_app, after every navigation action (tap, back, " +
			"swipe), and whenever you need to understand the current screen. " +
			"Prefer this over screenshot because it is faster, cheaper, and gives you " +
			"actionable selectors for tap/input_text directly.",
		InputSchema: getUiTreeInputSchema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := validateToolArguments(getUiTreeSchema, req.Params.Arguments); err != nil {
			return invalidArgumentsResult("get_ui_tree", err), nil
		}
		var args getUiTreeArgs
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return invalidArgumentsResult("get_ui_tree", err), nil
		}

		orch, err := orchestraOrFail(ctx, tc)
		if err != nil {
			return errorResult(err), nil
		}
		tree, err := orch.Hierarchy(ctx)
		if err != nil {
			return errorResult(err), nil
		}
		includeOverlays := args.IncludeSystemOverlays != nil && *args.IncludeSystemOverlays
		var flat []flatNode
		if includeOverlays {
			flat = flattenTree(tree, 0)
		} else if filtered, ok := orchestra.FilterSystemOverlays(tree); ok {
			flat = flattenTree(filtered, 0)
		}
		limited := flat
		if args.Limit != nil && *args.Limit < len(flat) {
			limited = flat[:*args.Limit]
		}
		if limited == nil {
			limited = []flatNode{}
		}
		return jsonToolResult(getUiTreeResult{
			Total:     len(flat),
			Returned:  len(limited),
			Truncated: len(limited) < len(flat),
			Nodes:     limited,
		})
	})
}
