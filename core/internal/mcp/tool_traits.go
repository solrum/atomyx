package mcp

import "github.com/modelcontextprotocol/go-sdk/mcp"

// toolTraits records what each tool declared about itself when it was
// registered, so the middleware reads declarations instead of keeping a
// list of tool names.
type toolTraits struct {
	observeOnly map[string]bool
}

func newToolTraits() *toolTraits {
	return &toolTraits{observeOnly: map[string]bool{}}
}

// addObserveOnlyTool registers a tool that only observes or manages the
// session. A call to it is not session activity: it neither marks the
// held device running nor restarts the idle timer, so polling such a tool
// cannot keep a device from its idle release.
func addObserveOnlyTool(server *mcp.Server, traits *toolTraits, tool *mcp.Tool, handler mcp.ToolHandler) {
	traits.observeOnly[tool.Name] = true
	server.AddTool(tool, handler)
}

func (t *toolTraits) isObserveOnly(name string) bool {
	return t.observeOnly[name]
}
